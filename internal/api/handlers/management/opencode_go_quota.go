package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// OpenCode Go is a first-class quota provider in Relay: an auth file with
// "type": "opencode-go" and an "api_key" becomes an account whose rolling
// (5-hour), weekly, and monthly limits come from OpenCode's usage API.
const (
	openCodeGoProvider = "opencode-go"
	openCodeGoUsageURL = "https://opencode.ai/inference/go/v1/usage"
	// Management quota calls are bounded like the management APICall.
	openCodeGoQuotaTimeout = 20 * time.Second
)

func isOpenCodeGoAuth(auth *coreauth.Auth) bool {
	return auth != nil && strings.EqualFold(strings.TrimSpace(auth.Provider), openCodeGoProvider)
}

func openCodeGoAPIKey(auth *coreauth.Auth) string {
	if auth.Metadata != nil {
		if key, ok := auth.Metadata["api_key"].(string); ok && strings.TrimSpace(key) != "" {
			return strings.TrimSpace(key)
		}
	}
	return tokenValueForAuth(auth)
}

func (h *Handler) fetchOpenCodeGoQuota(ctx context.Context, auth *coreauth.Auth) (pluginapi.QuotaFetchResponse, error) {
	key := openCodeGoAPIKey(auth)
	if key == "" {
		return pluginapi.QuotaFetchResponse{}, fmt.Errorf("opencode-go credential has no api_key")
	}
	ctx, cancel := context.WithTimeout(ctx, openCodeGoQuotaTimeout)
	defer cancel()
	req, errReq := http.NewRequestWithContext(ctx, http.MethodGet, openCodeGoUsageURL, nil)
	if errReq != nil {
		return pluginapi.QuotaFetchResponse{}, fmt.Errorf("build usage request: %w", errReq)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	// OpenCode Go asks clients to identify themselves.
	req.Header.Set("User-Agent", "relay")

	resp, errDo := (&http.Client{Transport: h.apiCallTransport(auth, "")}).Do(req)
	if errDo != nil {
		return pluginapi.QuotaFetchResponse{}, fmt.Errorf("usage request failed: %w", errDo)
	}
	defer func() { _ = resp.Body.Close() }()
	body, errRead := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if errRead != nil {
		return pluginapi.QuotaFetchResponse{}, fmt.Errorf("read usage response: %w", errRead)
	}
	if resp.StatusCode != http.StatusOK {
		return pluginapi.QuotaFetchResponse{}, fmt.Errorf("usage request returned status %d", resp.StatusCode)
	}
	return parseOpenCodeGoUsage(body)
}

// parseOpenCodeGoUsage maps {"usage":{"rolling":{"status","percent","resetsAt"},...}}
// to Relay's quota shape. "percent" is the share used; a rate-limited window
// has nothing left whatever its percent says.
func parseOpenCodeGoUsage(body []byte) (pluginapi.QuotaFetchResponse, error) {
	var decoded struct {
		Usage map[string]struct {
			Status   string   `json:"status"`
			Percent  *float64 `json:"percent"`
			ResetsAt string   `json:"resetsAt"`
		} `json:"usage"`
	}
	if errJSON := json.Unmarshal(body, &decoded); errJSON != nil {
		return pluginapi.QuotaFetchResponse{}, fmt.Errorf("usage response is not valid JSON")
	}
	group := pluginapi.QuotaGroup{DisplayName: "OpenCode Go"}
	for _, name := range []string{"rolling", "weekly", "monthly"} {
		window, ok := decoded.Usage[name]
		if !ok {
			continue
		}
		remaining := 0.0
		if window.Status != "rate-limited" {
			if window.Percent == nil {
				return pluginapi.QuotaFetchResponse{}, fmt.Errorf("usage window %s has no percent", name)
			}
			remaining = min(1, max(0, 1-*window.Percent/100))
		}
		group.Buckets = append(group.Buckets, pluginapi.QuotaBucket{
			Window:            name,
			RemainingFraction: remaining,
			ResetTime:         window.ResetsAt,
		})
	}
	if len(group.Buckets) == 0 {
		return pluginapi.QuotaFetchResponse{}, fmt.Errorf("usage response has no windows")
	}
	return pluginapi.QuotaFetchResponse{Groups: []pluginapi.QuotaGroup{group}}, nil
}
