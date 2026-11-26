package live

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

// HandleTranslation reports that the Codex OAuth upstream has no translation session capability.
func (h *Handler) HandleTranslation(c *gin.Context) {
	writeCapabilityNotSupported(c, "Realtime translation sessions")
}

// HandleTranscriptionSession reports that the Codex OAuth upstream has no transcription-only capability.
func (h *Handler) HandleTranscriptionSession(c *gin.Context) {
	writeCapabilityNotSupported(c, "Realtime transcription-only sessions")
}

// HandleSIPControl reports that the Codex OAuth upstream has no SIP dialog capability.
func (h *Handler) HandleSIPControl(c *gin.Context) {
	action := "control"
	if c != nil && c.Request != nil && c.Request.URL != nil {
		parts := strings.Split(strings.Trim(c.Request.URL.Path, "/"), "/")
		if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
			action = parts[len(parts)-1]
		}
	}
	writeCapabilityNotSupported(c, "Realtime SIP "+action)
}

// HandleHangup forwards hangup for a locally created WebRTC call using its pinned OAuth credential.
func (h *Handler) HandleHangup(c *gin.Context) {
	if h == nil || h.authManager == nil || h.sessions == nil {
		writeRealtimeError(c, http.StatusServiceUnavailable, "Codex live session service unavailable", "server_error", "realtime_session_unavailable")
		return
	}
	callID := strings.TrimSpace(c.Param("call_id"))
	if !callIDPattern.MatchString(callID) {
		writeRealtimeError(c, http.StatusBadRequest, "Invalid Realtime call ID", "invalid_request_error", "invalid_call_id")
		return
	}
	session, ok := h.sessions.peek(callID)
	if !ok {
		writeRealtimeError(c, http.StatusNotFound, "Realtime call not found", "invalid_request_error", "realtime_call_not_found")
		return
	}

	if ownerPrincipal, ownerProvider := requestOwner(c); session.ownerPrincipal != "" && (ownerPrincipal != session.ownerPrincipal || ownerProvider != session.ownerProvider) {
		writeRealtimeError(c, http.StatusForbidden, "Realtime call belongs to another API principal", "invalid_request_error", "realtime_call_scope_mismatch")
		return
	}

	ctx := context.WithValue(c.Request.Context(), "gin", c)
	ctx = handlers.EnrichContextWithSessionHierarchy(ctx, liveSelectionHeaders(c), nil, map[string]any{
		coreexecutor.ExecutionSessionMetadataKey: callID,
	})
	if session.sessionID != "" {
		meta := logging.GetClientRequestMetadata(ctx)
		meta.SessionID = session.sessionID
		meta.ParentSessionID = session.parentSessionID
		ctx = logging.WithClientRequestMetadata(ctx, meta)
	}
	var selected *auth.Auth

	selectionOpts := coreexecutor.Options{
		Headers: liveSelectionHeaders(c),
		Metadata: map[string]any{
			coreexecutor.PinnedAuthMetadataKey:       session.authID,
			coreexecutor.ExecutionSessionMetadataKey: callID,
		},
	}
	selectedAuth, errSelect := h.selectOAuth(ctx, session.model, selectionOpts)
	if errSelect != nil {
		writeSelectionError(c, errSelect)
		return
	}
	selected = selectedAuth
	if selected == nil {
		writeRealtimeError(c, http.StatusServiceUnavailable, "Codex auth unavailable", "server_error", "codex_auth_unavailable")
		return
	}
	logging.SetGinCPATraceID(c, selected.EnsureIndex())

	body, errRead := readBody(c.Request.Body)
	if errRead != nil {
		writeRealtimeError(c, clienterror.HTTPStatusFromErrorOr(errRead, http.StatusBadRequest), errRead.Error(), "invalid_request_error", "invalid_request")
		return
	}
	upstreamURL := h.realtimeHTTPBaseURL() + "/realtime/calls/" + url.PathEscape(callID) + "/hangup"
	baseHeaders := protocolHeaders(c.Request.Header)
	if contentType := strings.TrimSpace(c.GetHeader("Content-Type")); contentType != "" {
		baseHeaders.Set("Content-Type", contentType)
	}
	runtimeConfig := h.currentConfig()
	performRequest := func(current *auth.Auth) (*http.Response, error) {
		headers := baseHeaders.Clone()
		setAccountHeader(headers, current)
		request, errRequest := h.authManager.NewHttpRequest(ctx, current, http.MethodPost, upstreamURL, body, headers)
		if errRequest != nil {
			return nil, errRequest
		}
		authType, authValue := current.AccountInfo()
		helps.RecordAPIRequest(ctx, runtimeConfig, helps.UpstreamRequestLog{
			URL:       upstreamURL,
			Method:    http.MethodPost,
			Headers:   headersForLogging(request.Header),
			Body:      body,
			Provider:  "codex",
			AuthID:    current.ID,
			AuthLabel: current.Label,
			AuthType:  authType,
			AuthValue: authValue,
		})
		return h.authManager.HttpRequest(ctx, current, request)
	}
	response, errRequest := performRequest(selected)
	if errRequest != nil {
		helps.RecordAPIResponseError(ctx, runtimeConfig, errRequest)
		writeRealtimeError(c, clienterror.HTTPStatusFromErrorOr(errRequest, http.StatusBadGateway), errRequest.Error(), "api_error", "realtime_upstream_unavailable")
		return
	}
	defer func() {
		if errClose := response.Body.Close(); errClose != nil {
			log.Errorf("codex realtime hangup: close response body error: %v", errClose)
		}
	}()
	helps.RecordAPIResponseMetadata(ctx, runtimeConfig, response.StatusCode, callResponseHeaders(response.Header))
	responseBody, errResponse := readLimitedBody(response.Body)
	if errResponse != nil {
		helps.AppendAPIResponseChunk(ctx, runtimeConfig, responseBody)
		helps.RecordAPIResponseError(ctx, runtimeConfig, errResponse)
		writeRealtimeError(c, http.StatusBadGateway, "Failed to read Realtime hangup response", "api_error", "realtime_upstream_unavailable")
		return
	}
	helps.AppendAPIResponseChunk(ctx, runtimeConfig, responseBody)
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		h.sessions.complete(session, "client_hangup")
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	copyRealtimeHandshakeHeaders(c.Writer.Header(), response.Header)
	c.Status(response.StatusCode)
	if _, errWrite := c.Writer.Write(responseBody); errWrite != nil {
		log.WithError(errWrite).Warn("codex realtime hangup: write response body failed")
	}
}

func (h *Handler) realtimeHTTPBaseURL() string {
	return strings.TrimRight(websocketHTTPURL(h.sidebandAPIBaseURL), "/")
}

func writeCapabilityNotSupported(c *gin.Context, capability string) {
	writeRealtimeError(c, http.StatusNotImplemented, capability+" are not supported by the ChatGPT/Codex OAuth upstream", "not_supported_error", "realtime_capability_not_supported")
}
