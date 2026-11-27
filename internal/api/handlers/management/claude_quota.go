package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// Claude Code exposes the account's current subscription usage at this
// endpoint with the same OAuth token used for inference. Reading it keeps
// Relay's figures current even while an account serves no proxied traffic;
// response headers only move when that account answers a request.
const (
	claudeUsageURL     = "https://api.anthropic.com/api/oauth/usage"
	claudeQuotaTimeout = 8 * time.Second
)

type claudeUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

// isClaudeAuth matches Claude OAuth credentials only. API-key credentials have
// no OAuth token, and the subscription usage endpoint does not accept one.
func isClaudeAuth(auth *coreauth.Auth) bool {
	return auth != nil &&
		strings.EqualFold(strings.TrimSpace(auth.Provider), "claude") &&
		claudeAccessToken(auth) != ""
}

func claudeAccessToken(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if v := misc.MetadataString(auth.Metadata, "accessToken"); v != "" {
		return v
	}
	return misc.MetadataString(auth.Metadata, "access_token")
}

// claudeTokenExpired reads the OAuth expiry the auth file records. A missing
// or unparseable value counts as expired: a 401 already proved the token bad.
func claudeTokenExpired(auth *coreauth.Auth) bool {
	raw := misc.MetadataString(auth.Metadata, "expired")
	if raw == "" {
		return true
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if expiredAt, errParse := time.Parse(layout, raw); errParse == nil {
			return expiredAt.Before(time.Now())
		}
	}
	return true
}

func (h *Handler) fetchClaudeQuota(ctx context.Context, auth *coreauth.Auth) (QuotaFetchResponse, error) {
	body, status, errFetch := h.claudeUsageBody(ctx, auth, claudeAccessToken(auth))
	if errFetch != nil {
		return QuotaFetchResponse{}, errFetch
	}
	// Subscription access tokens live for hours; an idle credential can be
	// past its expiry. Refresh once through the normal auth path and retry.
	// A 403 is a permissions answer, not expiry, so it must not rotate tokens.
	if status == http.StatusUnauthorized && claudeTokenExpired(auth) && claudeHasRefreshToken(auth) {
		refreshed := h.refreshClaudeAuth(ctx, auth)
		if refreshed == nil {
			return QuotaFetchResponse{}, fmt.Errorf("usage request returned status %d", status)
		}
		body, status, errFetch = h.claudeUsageBody(ctx, refreshed, claudeAccessToken(refreshed))
		if errFetch != nil {
			return QuotaFetchResponse{}, errFetch
		}
	}
	if status != http.StatusOK {
		return QuotaFetchResponse{}, fmt.Errorf("usage request returned status %d", status)
	}
	return parseClaudeUsage(body)
}

func (h *Handler) claudeUsageBody(ctx context.Context, auth *coreauth.Auth, token string) ([]byte, int, error) {
	if token == "" {
		return nil, 0, fmt.Errorf("claude credential has no access token")
	}
	return h.usageBody(ctx, auth, claudeUsageURL, token, claudeQuotaTimeout, map[string]string{"anthropic-beta": "oauth-2025-04-20"})
}

// usageBody sends an authenticated GET to a provider usage endpoint and
// returns at most 1 MiB of the body with the status code, leaving status
// handling to the caller.
func (h *Handler) usageBody(ctx context.Context, auth *coreauth.Auth, url, token string, timeout time.Duration, headers map[string]string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, errReq := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if errReq != nil {
		return nil, 0, fmt.Errorf("build usage request: %w", errReq)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "relay")
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, errDo := (&http.Client{Transport: h.apiCallTransport(auth, "")}).Do(req)
	if errDo != nil {
		return nil, 0, fmt.Errorf("usage request failed: %w", errDo)
	}
	defer func() { _ = resp.Body.Close() }()
	body, errRead := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if errRead != nil {
		return nil, 0, fmt.Errorf("read usage response: %w", errRead)
	}
	return body, resp.StatusCode, nil
}

func (h *Handler) refreshClaudeAuth(ctx context.Context, auth *coreauth.Auth) *coreauth.Auth {
	if h == nil || h.authManager == nil {
		return nil
	}
	// The app gives up after 20 seconds; let a refresh already underway finish
	// and land, so the next poll uses the new token.
	refreshed, errRefresh := h.authManager.ForceRefreshAuth(context.WithoutCancel(ctx), auth.ID)
	if errRefresh != nil {
		return nil
	}
	return refreshed
}

func claudeHasRefreshToken(auth *coreauth.Auth) bool {
	return misc.MetadataString(auth.Metadata, "refresh_token") != ""
}

// parseClaudeUsage maps {"five_hour":{"utilization","resets_at"},
// "seven_day":{...}} to Relay's quota shape. Utilization is the share used,
// so the remaining fraction is 1 - utilization/100. The weekly window is
// required because the app and `relay status` show it for every account.
func parseClaudeUsage(body []byte) (QuotaFetchResponse, error) {
	var decoded struct {
		FiveHour *claudeUsageWindow `json:"five_hour"`
		SevenDay *claudeUsageWindow `json:"seven_day"`
	}
	if errJSON := json.Unmarshal(body, &decoded); errJSON != nil {
		return QuotaFetchResponse{}, fmt.Errorf("usage response is not valid JSON")
	}
	if decoded.SevenDay == nil || decoded.SevenDay.Utilization == nil {
		return QuotaFetchResponse{}, fmt.Errorf("usage response has no weekly window")
	}
	group := QuotaGroup{DisplayName: "Claude"}
	for _, item := range []struct {
		name   string
		window *claudeUsageWindow
	}{
		{"5h", decoded.FiveHour},
		{"weekly", decoded.SevenDay},
	} {
		if item.window == nil || item.window.Utilization == nil {
			continue
		}
		remaining := min(1, max(0, 1-*item.window.Utilization/100))
		group.Buckets = append(group.Buckets, QuotaBucket{
			Window:            item.name,
			RemainingFraction: remaining,
			ResetTime:         item.window.ResetsAt,
		})
	}
	if len(group.Buckets) == 0 {
		return QuotaFetchResponse{}, fmt.Errorf("usage response has no windows")
	}
	return QuotaFetchResponse{Groups: []QuotaGroup{group}}, nil
}
