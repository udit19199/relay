package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gin "github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/udit19199/relay/internal/access"
	configaccess "github.com/udit19199/relay/internal/access/config_access"
	managementHandlers "github.com/udit19199/relay/internal/api/handlers/management"
	"github.com/udit19199/relay/internal/config"
	proxyconfig "github.com/udit19199/relay/internal/config"
	internallogging "github.com/udit19199/relay/internal/logging"
	"github.com/udit19199/relay/internal/registry"
	"github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

type codexSearchCaptureExecutor struct {
	request      *http.Request
	body         []byte
	authIDs      []string
	prepareErr   error
	httpErr      error
	responseBody io.ReadCloser
	statuses     []int
	refreshCalls int
	httpCalls    int
	beforeReturn func()
}

func (e *codexSearchCaptureExecutor) Identifier() string { return "codex" }

func (e *codexSearchCaptureExecutor) Execute(context.Context, *auth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, nil
}

func (e *codexSearchCaptureExecutor) ExecuteStream(context.Context, *auth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	return nil, nil
}

func (e *codexSearchCaptureExecutor) Refresh(_ context.Context, a *auth.Auth) (*auth.Auth, error) {
	e.refreshCalls++
	updated := a.Clone()
	if updated.Metadata == nil {
		updated.Metadata = make(map[string]any)
	}
	updated.Metadata["access_token"] = "refreshed-home-search-token"
	return updated, nil
}

func (e *codexSearchCaptureExecutor) CountTokens(context.Context, *auth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, nil
}

func (e *codexSearchCaptureExecutor) PrepareRequest(req *http.Request, a *auth.Auth) error {
	if e.prepareErr != nil {
		return e.prepareErr
	}
	token, _ := a.Metadata["access_token"].(string)
	if strings.TrimSpace(token) == "" && a.Attributes != nil {
		token = a.Attributes[auth.AttributeAPIKey]
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

type codexSearchGinContextSelector struct {
	ginContext *gin.Context
}

func (s *codexSearchGinContextSelector) Pick(ctx context.Context, _ string, _ string, _ coreexecutor.Options, auths []*auth.Auth) (*auth.Auth, error) {
	s.ginContext, _ = ctx.Value("gin").(*gin.Context)
	if len(auths) == 0 {
		return nil, nil
	}
	return auths[0], nil
}

type codexSearchAPIKeyFirstSelector struct{}

func (s *codexSearchAPIKeyFirstSelector) Pick(_ context.Context, _ string, _ string, _ coreexecutor.Options, auths []*auth.Auth) (*auth.Auth, error) {
	for _, candidate := range auths {
		if candidate.AuthKind() == auth.AuthKindAPIKey {
			return candidate, nil
		}
	}
	if len(auths) == 0 {
		return nil, nil
	}
	return auths[0], nil
}

func (e *codexSearchCaptureExecutor) HttpRequest(_ context.Context, selected *auth.Auth, req *http.Request) (*http.Response, error) {
	if e.httpErr != nil {
		return nil, e.httpErr
	}
	e.request = req.Clone(req.Context())
	e.authIDs = append(e.authIDs, selected.ID)
	e.httpCalls++
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	e.body = body
	responseBody := e.responseBody
	if responseBody == nil {
		responseBody = io.NopCloser(strings.NewReader(`{"results":[{"url":"https://example.com"}]}`))
	}
	statusCode := http.StatusOK
	if e.httpCalls <= len(e.statuses) && e.statuses[e.httpCalls-1] > 0 {
		statusCode = e.statuses[e.httpCalls-1]
	}
	if e.beforeReturn != nil {
		e.beforeReturn()
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       responseBody,
	}, nil
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWithOptions(t)
}

func newTestServerWithOptions(t *testing.T, opts ...ServerOption) *Server {
	t.Helper()
	cfg := &proxyconfig.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"test-key"},
		},
	}
	return newTestServerWithConfig(t, cfg, opts...)
}

func newTestServerWithConfig(t *testing.T, cfg *proxyconfig.Config, opts ...ServerOption) *Server {
	t.Helper()

	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	cfg.Port = 0
	cfg.AuthDir = authDir
	cfg.Debug = true
	cfg.LoggingToFile = false

	authManager := auth.NewManager(nil, nil, nil)
	accessManager := access.NewManager()
	if len(cfg.APIKeys) > 0 {
		accessManager.SetProviders([]access.Provider{configaccess.NewAPIKeyProvider(cfg.APIKeys)})
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	return NewServer(cfg, authManager, accessManager, configPath, opts...)
}

func TestNewServerNeverTrustsForwardedClientIPHeaders(t *testing.T) {
	server := newTestServer(t)
	server.engine.GET("/test-client-ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})
	request := httptest.NewRequest(http.MethodGet, "/test-client-ip", nil)
	request.RemoteAddr = "198.51.100.20:43123"
	request.Header.Set("X-Forwarded-For", "203.0.113.5")
	recorder := httptest.NewRecorder()
	server.engine.ServeHTTP(recorder, request)
	if got := recorder.Body.String(); got != "198.51.100.20" {
		t.Fatalf("client IP = %q, want direct peer IP (forwarded headers must never be trusted)", got)
	}
}

func TestHealthz(t *testing.T) {
	server := newTestServer(t)

	t.Run("GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}

		var resp struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response JSON: %v; body=%s", err, rr.Body.String())
		}
		if resp.Status != "ok" {
			t.Fatalf("unexpected response status: got %q want %q", resp.Status, "ok")
		}
	})

	t.Run("HEAD", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/healthz", nil)
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
		if rr.Body.Len() != 0 {
			t.Fatalf("expected empty body for HEAD request, got %q", rr.Body.String())
		}
	})
}

func TestCodexAlphaSearchForwardsRequest(t *testing.T) {
	server := newTestServer(t)
	executor := &codexSearchCaptureExecutor{}
	server.handlers.AuthManager.RegisterExecutor(executor)
	credential := &auth.Auth{
		ID:       "codex-auth",
		Provider: "codex",
		Status:   auth.StatusActive,
		Metadata: map[string]any{"access_token": "codex-token", "account_id": "account-123"},
	}
	if _, err := server.handlers.AuthManager.Register(context.Background(), credential); err != nil {
		t.Fatalf("register Codex auth: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"query":"GPT-5.6"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Session_id", "session-123")
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if executor.request == nil {
		t.Fatal("Codex executor did not receive a request")
	}
	if got, want := executor.request.URL.String(), "https://chatgpt.com/backend-api/codex/alpha/search"; got != want {
		t.Fatalf("upstream URL = %q, want %q", got, want)
	}
	if got, want := string(executor.body), `{"query":"GPT-5.6"}`; got != want {
		t.Fatalf("upstream body = %q, want %q", got, want)
	}
	if got := executor.request.Header.Get("Authorization"); got != "Bearer codex-token" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := executor.request.Header.Get("Chatgpt-Account-Id"); got != "account-123" {
		t.Fatalf("Chatgpt-Account-Id = %q", got)
	}
	if got := executor.request.Header.Get("Session_id"); got != "session-123" {
		t.Fatalf("Session_id = %q", got)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("response Content-Type = %q", got)
	}
	traceID := rr.Header().Get(internallogging.CPATraceIDHeader)
	parts := strings.SplitN(traceID, "-", 3)
	if len(parts) != 3 || parts[1] != credential.Index || parts[2] == "" {
		t.Fatalf("trace ID = %q, want timestamp-%s-requestID", traceID, credential.Index)
	}
	if _, errParseUUID := uuid.Parse(parts[2]); errParseUUID != nil {
		t.Fatalf("trace requestID = %q: %v", parts[2], errParseUUID)
	}
	if _, errParse := time.Parse("20060102150405", parts[0]); errParse != nil {
		t.Fatalf("trace timestamp = %q: %v", parts[0], errParse)
	}
}

func TestCodexAlphaSearchSanitizesResponsesOnlyFields(t *testing.T) {
	server := newTestServer(t)
	executor := &codexSearchCaptureExecutor{}
	server.handlers.AuthManager.RegisterExecutor(executor)
	credential := &auth.Auth{
		ID:       "codex-auth",
		Provider: "codex",
		Status:   auth.StatusActive,
		Metadata: map[string]any{"access_token": "codex-token"},
	}
	if _, errRegister := server.handlers.AuthManager.Register(context.Background(), credential); errRegister != nil {
		t.Fatalf("register Codex auth: %v", errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(credential.ID, credential.Provider, []*registry.ModelInfo{{ID: "gpt-5.6-sol"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(credential.ID)
	})

	payload := `{"id":"session-123","model":"gpt-5.6-sol","commands":{"search_query":[{"q":"golang channels"}]},"prompt_cache_key":"cache-123","prompt_cache_retention":"24h"}`
	for _, path := range []string{"/v1/alpha/search", "/backend-api/codex/alpha/search"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
			req.Header.Set("Authorization", "Bearer test-key")
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			server.engine.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
			}
			var upstreamBody map[string]json.RawMessage
			if errUnmarshal := json.Unmarshal(executor.body, &upstreamBody); errUnmarshal != nil {
				t.Fatalf("unmarshal upstream body: %v; body=%s", errUnmarshal, executor.body)
			}
			if _, exists := upstreamBody["prompt_cache_key"]; exists {
				t.Fatalf("upstream body contains prompt_cache_key: %s", executor.body)
			}
			if _, exists := upstreamBody["prompt_cache_retention"]; exists {
				t.Fatalf("upstream body contains prompt_cache_retention: %s", executor.body)
			}
			for _, field := range []string{"id", "model", "commands"} {
				if _, exists := upstreamBody[field]; !exists {
					t.Fatalf("upstream body missing %s: %s", field, executor.body)
				}
			}
		})
	}
}

func TestCodexAlphaSearchCredentialPolicy(t *testing.T) {
	newServer := func(t *testing.T, credentials ...*auth.Auth) (*Server, *codexSearchCaptureExecutor) {
		t.Helper()
		server := newTestServer(t)
		server.handlers.AuthManager.SetSelector(&codexSearchAPIKeyFirstSelector{})
		executor := &codexSearchCaptureExecutor{}
		server.handlers.AuthManager.RegisterExecutor(executor)
		for _, credential := range credentials {
			if _, errRegister := server.handlers.AuthManager.Register(context.Background(), credential); errRegister != nil {
				t.Fatalf("register Codex auth %s: %v", credential.ID, errRegister)
			}
		}
		return server, executor
	}
	apiKeyCredential := func() *auth.Auth {
		return &auth.Auth{
			ID:         "codex-api-key",
			Provider:   "codex",
			Status:     auth.StatusActive,
			Attributes: map[string]string{auth.AttributeAPIKey: "codex-key"},
		}
	}
	oauthCredential := func() *auth.Auth {
		return &auth.Auth{
			ID:       "codex-oauth",
			Provider: "codex",
			Status:   auth.StatusActive,
			Metadata: map[string]any{"access_token": "codex-token"},
		}
	}

	t.Run("mixed credentials", func(t *testing.T) {
		server, executor := newServer(t, apiKeyCredential(), oauthCredential())
		req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"query":"GPT-5.6"}`))
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
		if got := executor.authIDs; len(got) != 1 || got[0] != "codex-oauth" {
			t.Fatalf("selected auth IDs = %v, want [codex-oauth]", got)
		}
	})

	t.Run("ordinary API key only", func(t *testing.T) {
		server, executor := newServer(t, apiKeyCredential())
		req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"query":"GPT-5.6"}`))
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusServiceUnavailable, rr.Body.String())
		}
		if len(executor.authIDs) != 0 {
			t.Fatalf("selected auth IDs = %v, want none", executor.authIDs)
		}
	})
}

func TestCodexAlphaSearchOptInAPIKeyUsesConfiguredEndpoint(t *testing.T) {
	server := newTestServer(t)
	executor := &codexSearchCaptureExecutor{}
	server.handlers.AuthManager.RegisterExecutor(executor)
	credential := &auth.Auth{
		ID:       "codex-alpha-api-key",
		Provider: "codex",
		Status:   auth.StatusActive,
		Attributes: map[string]string{
			auth.AttributeAPIKey:           "codex-alpha-key",
			auth.AttributeCodexAlphaSearch: "true",
			"base_url":                     "https://codex.example.com/v1/",
		},
	}
	if _, errRegister := server.handlers.AuthManager.Register(context.Background(), credential); errRegister != nil {
		t.Fatalf("register Codex API key: %v", errRegister)
	}

	payload := `{"query":"golang","prompt_cache_key":"cache","prompt_cache_retention":"24h"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if executor.request == nil {
		t.Fatal("Codex executor did not receive a request")
	}
	if got, want := executor.request.URL.String(), "https://codex.example.com/v1/alpha/search"; got != want {
		t.Fatalf("upstream URL = %q, want %q", got, want)
	}
	if got := executor.request.Header.Get("Authorization"); got != "Bearer codex-alpha-key" {
		t.Fatalf("Authorization = %q, want API key bearer", got)
	}
	var upstreamBody map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(executor.body, &upstreamBody); errUnmarshal != nil {
		t.Fatalf("unmarshal upstream body: %v", errUnmarshal)
	}
	for _, field := range []string{"prompt_cache_key", "prompt_cache_retention"} {
		if _, exists := upstreamBody[field]; exists {
			t.Fatalf("upstream body contains %s: %s", field, executor.body)
		}
	}
}

func TestCodexAlphaSearchOptInAPIKeyStripsCredentialPrefix(t *testing.T) {
	server := newTestServer(t)
	executor := &codexSearchCaptureExecutor{}
	server.handlers.AuthManager.RegisterExecutor(executor)
	credential := &auth.Auth{
		ID:       "codex-alpha-api-key-prefix",
		Provider: "codex",
		Prefix:   "vendor",
		Status:   auth.StatusActive,
		Attributes: map[string]string{
			auth.AttributeAPIKey:           "codex-alpha-key",
			auth.AttributeCodexAlphaSearch: "true",
			"base_url":                     "https://codex.example.com/v1",
		},
	}
	if _, errRegister := server.handlers.AuthManager.Register(context.Background(), credential); errRegister != nil {
		t.Fatalf("register Codex API key: %v", errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(credential.ID, credential.Provider, []*registry.ModelInfo{{ID: "vendor/gpt-5.6-sol"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(credential.ID)
	})

	payload := `{"id":"00000000-0000-4000-8000-000000000003","model":"vendor/gpt-5.6-sol","commands":{"search_query":[{"q":"Go programming language official website"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if executor.request == nil {
		t.Fatal("Codex executor did not receive a request")
	}
	var upstreamBody map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(executor.body, &upstreamBody); errUnmarshal != nil {
		t.Fatalf("unmarshal upstream body: %v", errUnmarshal)
	}
	var upstreamModel string
	if errUnmarshal := json.Unmarshal(upstreamBody["model"], &upstreamModel); errUnmarshal != nil {
		t.Fatalf("unmarshal upstream model: %v", errUnmarshal)
	}
	if upstreamModel != "gpt-5.6-sol" {
		t.Fatalf("upstream model = %q, want gpt-5.6-sol", upstreamModel)
	}
}

func TestCodexAlphaSearchOptInAPIKeyResolvesModelAlias(t *testing.T) {
	server := newTestServer(t)
	executor := &codexSearchCaptureExecutor{}
	server.handlers.AuthManager.RegisterExecutor(executor)
	server.handlers.AuthManager.SetConfig(&proxyconfig.Config{
		CodexKey: []proxyconfig.CodexKey{{
			APIKey:      "codex-alpha-key",
			Prefix:      "vendor",
			BaseURL:     "https://codex.example.com/v1",
			AlphaSearch: true,
			Models: []proxyconfig.CodexModel{{
				Name:  "gpt-5.6-sol",
				Alias: "sol-alias",
			}},
		}},
	})
	credential := &auth.Auth{
		ID:       "codex-alpha-api-key-alias",
		Provider: "codex",
		Prefix:   "vendor",
		Status:   auth.StatusActive,
		Attributes: map[string]string{
			auth.AttributeAPIKey:           "codex-alpha-key",
			auth.AttributeCodexAlphaSearch: "true",
			"base_url":                     "https://codex.example.com/v1",
		},
	}
	if _, errRegister := server.handlers.AuthManager.Register(context.Background(), credential); errRegister != nil {
		t.Fatalf("register Codex API key: %v", errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(credential.ID, credential.Provider, []*registry.ModelInfo{{ID: "vendor/sol-alias"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(credential.ID)
	})

	payload := `{"model":"vendor/sol-alias","commands":{"search_query":[{"q":"golang"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if executor.request == nil {
		t.Fatal("Codex executor did not receive a request")
	}
	var upstreamBody map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(executor.body, &upstreamBody); errUnmarshal != nil {
		t.Fatalf("unmarshal upstream body: %v", errUnmarshal)
	}
	var upstreamModel string
	if errUnmarshal := json.Unmarshal(upstreamBody["model"], &upstreamModel); errUnmarshal != nil {
		t.Fatalf("unmarshal upstream model: %v", errUnmarshal)
	}
	if upstreamModel != "gpt-5.6-sol" {
		t.Fatalf("upstream model = %q, want gpt-5.6-sol", upstreamModel)
	}
}

func TestRewriteCodexAlphaSearchModel(t *testing.T) {
	original := []byte(`{"id":"search-1","model":"vendor/gpt-5.6-sol","commands":{"search_query":[{"q":"golang"}]}}`)
	rewritten := rewriteCodexAlphaSearchModel(original, "gpt-5.6-sol")
	var payload map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(rewritten, &payload); errUnmarshal != nil {
		t.Fatalf("unmarshal rewritten body: %v", errUnmarshal)
	}
	var model string
	if errUnmarshal := json.Unmarshal(payload["model"], &model); errUnmarshal != nil {
		t.Fatalf("unmarshal rewritten model: %v", errUnmarshal)
	}
	if model != "gpt-5.6-sol" {
		t.Fatalf("model = %q, want gpt-5.6-sol", model)
	}
	if _, exists := payload["commands"]; !exists {
		t.Fatal("commands field was dropped")
	}
	if string(rewriteCodexAlphaSearchModel([]byte(`{"query":"x"}`), "gpt-5.6-sol")) != `{"query":"x"}` {
		t.Fatal("body without model should remain unchanged")
	}
}

func TestCodexAlphaSearchOptInAPIKeyWithoutBaseURLFailsClosed(t *testing.T) {
	server := newTestServer(t)
	executor := &codexSearchCaptureExecutor{}
	server.handlers.AuthManager.RegisterExecutor(executor)
	if _, errRegister := server.handlers.AuthManager.Register(context.Background(), &auth.Auth{
		ID:       "codex-alpha-api-key",
		Provider: "codex",
		Status:   auth.StatusActive,
		Attributes: map[string]string{
			auth.AttributeAPIKey:           "codex-alpha-key",
			auth.AttributeCodexAlphaSearch: "true",
		},
	}); errRegister != nil {
		t.Fatalf("register Codex API key: %v", errRegister)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"query":"GPT-5.6"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusServiceUnavailable, rr.Body.String())
	}
	if executor.request != nil {
		t.Fatal("request was sent without an API key base URL")
	}
}

func TestCodexAlphaSearchPassesGinContextToAuthSelection(t *testing.T) {
	server := newTestServer(t)
	selector := &codexSearchGinContextSelector{}
	server.handlers.AuthManager.SetSelector(selector)
	executor := &codexSearchCaptureExecutor{}
	server.handlers.AuthManager.RegisterExecutor(executor)
	credential := &auth.Auth{
		ID:       "codex-auth",
		Provider: "codex",
		Status:   auth.StatusActive,
		Metadata: map[string]any{"access_token": "codex-token"},
	}
	if _, errRegister := server.handlers.AuthManager.Register(context.Background(), credential); errRegister != nil {
		t.Fatalf("register Codex auth: %v", errRegister)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search?key=home-query-key", strings.NewReader(`{"query":"GPT-5.6"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if selector.ginContext == nil {
		t.Fatal("auth selection did not receive the Gin context required by Home scheduling")
	}
	if got := selector.ginContext.Query("key"); got != "home-query-key" {
		t.Fatalf("Gin query key = %q, want %q", got, "home-query-key")
	}
}

func TestCodexAlphaSearchUsesRequestIDForSessionAffinity(t *testing.T) {
	server := newTestServer(t)
	server.handlers.AuthManager.SetSelector(auth.NewSessionAffinitySelector(&auth.RoundRobinSelector{}))
	executor := &codexSearchCaptureExecutor{}
	server.handlers.AuthManager.RegisterExecutor(executor)
	for _, id := range []string{"codex-auth-a", "codex-auth-b"} {
		registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: "gpt-5.6-luna"}})
		t.Cleanup(func() {
			registry.GetGlobalRegistry().UnregisterClient(id)
		})
		credential := &auth.Auth{
			ID:       id,
			Provider: "codex",
			Status:   auth.StatusActive,
			Metadata: map[string]any{"access_token": id},
		}
		if _, errRegister := server.handlers.AuthManager.Register(context.Background(), credential); errRegister != nil {
			t.Fatalf("register Codex auth: %v", errRegister)
		}
	}

	for _, payload := range []string{
		`{"id":"session-a","model":"gpt-5.6-luna"}`,
		`{"id":"session-b","model":"gpt-5.6-luna"}`,
		`{"id":"session-a","model":"gpt-5.6-luna"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
	}

	if got, want := len(executor.authIDs), 3; got != want {
		t.Fatalf("selected auth count = %d, want %d", got, want)
	}
	if executor.authIDs[0] == executor.authIDs[1] {
		t.Fatalf("different sessions selected the same auth %q", executor.authIDs[0])
	}
	if got, want := executor.authIDs[2], executor.authIDs[0]; got != want {
		t.Fatalf("session-affinity auth = %q, want %q", got, want)
	}
}

func TestCodexAlphaSearchRecordsRequestLog(t *testing.T) {
	server := newTestServer(t)
	server.cfg.RequestLog = true

	executor := &codexSearchCaptureExecutor{}
	server.handlers.AuthManager.RegisterExecutor(executor)
	credential := &auth.Auth{
		ID:       "codex-auth",
		Provider: "codex",
		Status:   auth.StatusActive,
		Metadata: map[string]any{"access_token": "codex-token", "account_id": "account-123"},
	}
	if _, err := server.handlers.AuthManager.Register(context.Background(), credential); err != nil {
		t.Fatalf("register Codex auth: %v", err)
	}

	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"query":"GPT-5.6"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	server.codexAlphaSearch(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	rawAPIRequest, okRequest := c.Get("API_REQUEST")
	if !okRequest {
		t.Fatal("API_REQUEST was not captured")
	}
	apiRequest, _ := rawAPIRequest.([]byte)
	if !strings.Contains(string(apiRequest), "=== API REQUEST 1 ===") {
		t.Fatalf("API_REQUEST missing request header section: %q", apiRequest)
	}
	if !strings.Contains(string(apiRequest), "https://chatgpt.com/backend-api/codex/alpha/search") {
		t.Fatalf("API_REQUEST missing upstream URL: %q", apiRequest)
	}
	if !strings.Contains(string(apiRequest), `{"query":"GPT-5.6"}`) {
		t.Fatalf("API_REQUEST missing body: %q", apiRequest)
	}
	rawAPIResponse, okResponse := c.Get("API_RESPONSE")
	if !okResponse {
		t.Fatal("API_RESPONSE was not captured")
	}
	apiResponse, _ := rawAPIResponse.([]byte)
	if !strings.Contains(string(apiResponse), "=== API RESPONSE 1 ===") {
		t.Fatalf("API_RESPONSE missing response header section: %q", apiResponse)
	}
	if !strings.Contains(string(apiResponse), `{"results":[{"url":"https://example.com"}]}`) {
		t.Fatalf("API_RESPONSE missing body: %q", apiResponse)
	}
}

func TestManagementResponseExposesHeadersForCORS(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "test-management-key")

	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusUnauthorized, rr.Body.String())
	}
	exposedHeaders := make(map[string]struct{})
	for _, headerName := range strings.Split(rr.Header().Get("Access-Control-Expose-Headers"), ",") {
		headerName = strings.ToLower(strings.TrimSpace(headerName))
		if headerName != "" {
			exposedHeaders[headerName] = struct{}{}
		}
	}
	for _, headerName := range corsExposedResponseHeaders {
		if _, ok := exposedHeaders[strings.ToLower(headerName)]; !ok {
			t.Fatalf("Access-Control-Expose-Headers missing %s: %q", headerName, rr.Header().Get("Access-Control-Expose-Headers"))
		}
	}
}

func TestOAuthCallbackRouteSkipsManagementKeyMiddleware(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "test-management-key")

	server := newTestServer(t)
	state := "server-codex-oauth-state"
	managementHandlers.RegisterOAuthSession(state, "codex")
	defer managementHandlers.CompleteOAuthSession(state)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/oauth-callback?state="+state+"&code=test-code", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	callbackPath := filepath.Join(server.cfg.AuthDir, ".oauth-codex-"+state+".oauth")
	if _, errRead := os.ReadFile(callbackPath); errRead != nil {
		t.Fatalf("expected callback file to be written without management key: %v", errRead)
	}
}
