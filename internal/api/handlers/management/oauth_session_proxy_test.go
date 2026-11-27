package management

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/udit19199/relay/internal/auth/codex"
	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	"github.com/udit19199/relay/internal/util"
)

type proxyTestCodexOAuthService struct {
	client   *http.Client
	tokenURL string
}

func (s *proxyTestCodexOAuthService) GenerateAuthURL(state string, _ *codex.PKCECodes) (string, error) {
	return "https://auth.example.test/oauth?state=" + state, nil
}

func (s *proxyTestCodexOAuthService) ExchangeCodeForTokens(ctx context.Context, code string, _ *codex.PKCECodes) (*codex.CodexAuthBundle, error) {
	req, errReq := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader("code="+url.QueryEscape(code)))
	if errReq != nil {
		return nil, errReq
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, errDo := s.client.Do(req)
	if errDo != nil {
		return nil, errDo
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return nil, errRead
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(body, &payload)
	if payload.Email == "" {
		payload.Email = "codex-" + code + "@example.test"
	}
	now := time.Now()
	return &codex.CodexAuthBundle{
		TokenData: codex.CodexTokenData{
			IDToken:      makeOAuthTestJWT("plus"),
			AccessToken:  "access-" + code,
			RefreshToken: "refresh-" + code,
			AccountID:    "acc-" + code,
			Email:        payload.Email,
			PlanType:     "plus",
			Expire:       now.Add(time.Hour).Format(time.RFC3339),
		},
		LastRefresh: now.Format(time.RFC3339),
	}, nil
}

func (s *proxyTestCodexOAuthService) CreateTokenStorage(bundle *codex.CodexAuthBundle) *codex.CodexTokenStorage {
	return &codex.CodexTokenStorage{
		IDToken:      bundle.TokenData.IDToken,
		AccessToken:  bundle.TokenData.AccessToken,
		RefreshToken: bundle.TokenData.RefreshToken,
		AccountID:    bundle.TokenData.AccountID,
		LastRefresh:  bundle.LastRefresh,
		Email:        bundle.TokenData.Email,
		Expire:       bundle.TokenData.Expire,
		PlanType:     bundle.TokenData.PlanType,
	}
}

func startForwardHTTPProxy(t *testing.T, hits *atomic.Int32, expectedProxyAuth string, dropBody bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if expectedProxyAuth != "" {
			if r.Header.Get("Proxy-Authorization") != expectedProxyAuth {
				w.Header().Set("Proxy-Authenticate", `Basic realm="proxy"`)
				http.Error(w, "proxy auth required", http.StatusProxyAuthRequired)
				return
			}
		}
		if dropBody {
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, errHijack := hj.Hijack()
				if errHijack == nil {
					_ = conn.Close()
					return
				}
			}
			http.Error(w, "proxy dropped connection", http.StatusBadGateway)
			return
		}
		outReq, errReq := http.NewRequestWithContext(r.Context(), r.Method, r.RequestURI, r.Body)
		if errReq != nil {
			http.Error(w, errReq.Error(), http.StatusBadGateway)
			return
		}
		outReq.Header = r.Header.Clone()
		outReq.Header.Del("Proxy-Authorization")
		outReq.Header.Set("X-Via-Session-Proxy", "1")
		resp, errDo := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(outReq)
		if errDo != nil {
			http.Error(w, errDo.Error(), http.StatusBadGateway)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		for k, vals := range resp.Header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
}

func waitForOAuthStatus(t *testing.T, router http.Handler, state string) oauthStatusResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp := performOAuthStatusRequest(t, router, state)
		if resp.Status != "wait" {
			return resp
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for OAuth session %s status", state)
	return oauthStatusResponse{}
}

func TestStartOAuthV8SessionScopedProxyAndPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var proxyHits atomic.Int32
	var directHits atomic.Int32
	proxyServer := startForwardHTTPProxy(t, &proxyHits, "", false)
	defer proxyServer.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Session-Proxy") != "1" {
			directHits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"proxied-codex@example.test"}`))
	}))
	defer upstream.Close()

	originalFactory := newCodexOAuthService
	newCodexOAuthService = func(cfg *config.Config) codexOAuthService {
		var sdkCfg config.SDKConfig
		if cfg != nil {
			sdkCfg = cfg.SDKConfig
		}
		return &proxyTestCodexOAuthService{
			client:   util.SetProxy(&sdkCfg, &http.Client{}),
			tokenURL: upstream.URL + "/oauth/token",
		}
	}
	defer func() {
		newCodexOAuthService = originalFactory
	}()

	authDir := t.TempDir()
	unrelatedPath := filepath.Join(authDir, "unrelated.json")
	if errWrite := os.WriteFile(unrelatedPath, []byte(`{"type":"codex","email":"other@example.test","access_token":"a","refresh_token":"r"}`), 0o600); errWrite != nil {
		t.Fatalf("write unrelated auth file: %v", errWrite)
	}

	cfg := &config.Config{AuthDir: authDir}
	h := NewHandlerWithoutConfigFilePath(cfg, nil)

	var hookAuth *coreauth.Auth
	var diskProxyAtHookTime string
	h.SetPostAuthPersistHook(func(_ context.Context, auth *coreauth.Auth) error {
		hookAuth = auth.Clone()
		if path := strings.TrimSpace(auth.Attributes[coreauth.AttributePath]); path != "" {
			if raw, errRead := os.ReadFile(path); errRead == nil {
				var m map[string]any
				if json.Unmarshal(raw, &m) == nil {
					diskProxyAtHookTime, _ = m["proxy_url"].(string)
				}
			}
		}
		return nil
	})

	router := gin.New()
	router.GET("/v8/management/oauth/auth-url", h.StartOAuthV8)
	router.POST("/v8/management/oauth/auth-url", h.StartOAuthV8)
	router.GET("/status", h.GetAuthStatus)

	reqBody := `{"provider":"codex","proxy_url":"` + proxyServer.URL + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v8/management/oauth/auth-url", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v8/management/oauth/auth-url status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var startResp struct {
		State string `json:"state"`
		URL   string `json:"url"`
	}
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &startResp); errDecode != nil {
		t.Fatalf("decode start response: %v", errDecode)
	}
	if startResp.State == "" || startResp.URL == "" {
		t.Fatalf("unexpected start response: %s", rec.Body.String())
	}
	t.Cleanup(func() { CancelOAuthSession(startResp.State) })

	if _, errCallback := WriteOAuthCallbackFileForPendingSession(authDir, "codex", startResp.State, "code-1", ""); errCallback != nil {
		t.Fatalf("write callback file: %v", errCallback)
	}

	status := waitForOAuthStatus(t, router, startResp.State)
	if status.Status != "ok" {
		t.Fatalf("oauth status = %#v, want ok", status)
	}

	if proxyHits.Load() == 0 {
		t.Fatal("expected token exchange to use session proxy, got 0 proxy hits")
	}
	if directHits.Load() != 0 {
		t.Fatalf("expected 0 direct hits, got %d", directHits.Load())
	}
	if h.cfg.ProxyURL != "" {
		t.Fatalf("global cfg.ProxyURL mutated to %q, want empty", h.cfg.ProxyURL)
	}
	if hookAuth == nil {
		t.Fatal("expected postAuthPersistHook to be called")
	}
	if hookAuth.ProxyURL != proxyServer.URL {
		t.Fatalf("hookAuth.ProxyURL = %q, want %q", hookAuth.ProxyURL, proxyServer.URL)
	}
	if gotMeta, _ := hookAuth.Metadata["proxy_url"].(string); gotMeta != proxyServer.URL {
		t.Fatalf("hookAuth.Metadata[proxy_url] = %q, want %q", gotMeta, proxyServer.URL)
	}
	if diskProxyAtHookTime != proxyServer.URL {
		t.Fatalf("disk proxy_url at activation hook time = %q, want %q", diskProxyAtHookTime, proxyServer.URL)
	}

	unrelatedBytes, errReadUnrelated := os.ReadFile(unrelatedPath)
	if errReadUnrelated != nil {
		t.Fatalf("read unrelated auth file: %v", errReadUnrelated)
	}
	if strings.Contains(string(unrelatedBytes), "proxy_url") {
		t.Fatalf("unrelated auth file was modified: %s", string(unrelatedBytes))
	}
}

func TestStartOAuthV8RejectsInvalidOrQueryProxyURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, nil)
	router := gin.New()
	router.GET("/v8/management/oauth/auth-url", h.StartOAuthV8)
	router.POST("/v8/management/oauth/auth-url", h.StartOAuthV8)

	invalidProxies := []string{
		"://bad-url",
		"not-a-url",
		"ftp://127.0.0.1:21",
		"http://:8080",
		"http://127.0.0.1:99999",
		"http://127.0.0.1:0",
		"http://127.0.0.1:",
		"socks5://127.0.0.1",
		"direct",
		"none",
	}
	for _, badProxy := range invalidProxies {
		body, _ := json.Marshal(map[string]any{
			"provider":  "codex",
			"proxy_url": badProxy,
		})
		req := httptest.NewRequest(http.MethodPost, "/v8/management/oauth/auth-url", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("proxy_url=%q: status = %d, want 400; body = %s", badProxy, rec.Code, rec.Body.String())
		}
	}

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/v8/management/oauth/auth-url?provider=codex&proxy_url=http://127.0.0.1:8080", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s with query proxy_url: status = %d, want 400; body = %s", method, rec.Code, rec.Body.String())
		}
	}
}

func TestStartOAuthV8ProxyFailuresDoNotFallbackDirectAndRedactCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var directHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Session-Proxy") != "1" {
			directHits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"should-not-succeed@example.test"}`))
	}))
	defer upstream.Close()

	originalFactory := newCodexOAuthService
	newCodexOAuthService = func(cfg *config.Config) codexOAuthService {
		var sdkCfg config.SDKConfig
		if cfg != nil {
			sdkCfg = cfg.SDKConfig
		}
		return &proxyTestCodexOAuthService{
			client:   util.SetProxy(&sdkCfg, &http.Client{}),
			tokenURL: upstream.URL + "/oauth/token",
		}
	}
	defer func() {
		newCodexOAuthService = originalFactory
	}()

	var authRejectHits atomic.Int32
	authRejectProxy := startForwardHTTPProxy(t, &authRejectHits, "Basic dXNlcjpexpected-secret", false)
	defer authRejectProxy.Close()

	var dropHits atomic.Int32
	dropProxy := startForwardHTTPProxy(t, &dropHits, "", true)
	defer dropProxy.Close()

	ln, errListen := net.Listen("tcp", "127.0.0.1:0")
	if errListen != nil {
		t.Fatalf("listen temp port: %v", errListen)
	}
	closedAddr := ln.Addr().String()
	_ = ln.Close()

	authRejectURL, _ := url.Parse(authRejectProxy.URL)
	authRejectURL.User = url.UserPassword("user", "wrong-secret-pass")

	cases := []struct {
		name     string
		proxyURL string
	}{
		{name: "unreachable proxy", proxyURL: "http://user:unreachable-secret-pass@" + closedAddr},
		{name: "proxy auth rejected", proxyURL: authRejectURL.String()},
		{name: "proxy connection dropped", proxyURL: dropProxy.URL},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authDir := t.TempDir()
			h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, nil)
			router := gin.New()
			router.POST("/v8/management/oauth/auth-url", h.StartOAuthV8)
			router.GET("/status", h.GetAuthStatus)

			body, _ := json.Marshal(map[string]string{
				"provider":  "codex",
				"proxy_url": tc.proxyURL,
			})
			req := httptest.NewRequest(http.MethodPost, "/v8/management/oauth/auth-url", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("start status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			var startResp struct {
				State string `json:"state"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &startResp)
			t.Cleanup(func() { CancelOAuthSession(startResp.State) })

			if _, errWrite := WriteOAuthCallbackFileForPendingSession(authDir, "codex", startResp.State, "code-fail", ""); errWrite != nil {
				t.Fatalf("write callback: %v", errWrite)
			}

			status := waitForOAuthStatus(t, router, startResp.State)
			if status.Status != "error" {
				t.Fatalf("status = %#v, want error", status)
			}
			if strings.Contains(status.Error, "secret-pass") {
				t.Fatalf("status error leaked proxy password: %q", status.Error)
			}
			if directHits.Load() != 0 {
				t.Fatalf("direct fallback occurred: directHits = %d", directHits.Load())
			}
			entries, _ := os.ReadDir(authDir)
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".json") {
					t.Fatalf("unexpected credential file saved on failed proxy exchange: %s", e.Name())
				}
			}
		})
	}
}

func TestStartOAuthV8ParallelSessionsWithDifferentProxiesOutOfOrderCallbacks(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var hitsA, hitsB, directHits atomic.Int32
	proxyA := startForwardHTTPProxy(t, &hitsA, "", false)
	defer proxyA.Close()
	proxyB := startForwardHTTPProxy(t, &hitsB, "", false)
	defer proxyB.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Session-Proxy") != "1" {
			directHits.Add(1)
		}
		_ = r.ParseForm()
		code := r.FormValue("code")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"user-` + code + `@example.test"}`))
	}))
	defer upstream.Close()

	originalFactory := newCodexOAuthService
	newCodexOAuthService = func(cfg *config.Config) codexOAuthService {
		var sdkCfg config.SDKConfig
		if cfg != nil {
			sdkCfg = cfg.SDKConfig
		}
		return &proxyTestCodexOAuthService{
			client:   util.SetProxy(&sdkCfg, &http.Client{}),
			tokenURL: upstream.URL + "/oauth/token",
		}
	}
	defer func() {
		newCodexOAuthService = originalFactory
	}()

	authDir := t.TempDir()
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, nil)
	router := gin.New()
	router.POST("/v8/management/oauth/auth-url", h.StartOAuthV8)
	router.GET("/status", h.GetAuthStatus)

	startWithProxy := func(proxyURL string) string {
		body, _ := json.Marshal(map[string]string{"provider": "codex", "proxy_url": proxyURL})
		req := httptest.NewRequest(http.MethodPost, "/v8/management/oauth/auth-url", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("start status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		t.Cleanup(func() { CancelOAuthSession(resp.State) })
		return resp.State
	}

	stateA := startWithProxy(proxyA.URL)
	stateB := startWithProxy(proxyB.URL)

	// Deliver callback B first, then callback A (out of order)
	if _, errWrite := WriteOAuthCallbackFileForPendingSession(authDir, "codex", stateB, "b", ""); errWrite != nil {
		t.Fatalf("write callback B: %v", errWrite)
	}
	if statusB := waitForOAuthStatus(t, router, stateB); statusB.Status != "ok" {
		t.Fatalf("session B status = %#v, want ok", statusB)
	}
	if hitsB.Load() != 1 || hitsA.Load() != 0 {
		t.Fatalf("after B callback: hitsA=%d hitsB=%d, want 0 and 1", hitsA.Load(), hitsB.Load())
	}

	if _, errWrite := WriteOAuthCallbackFileForPendingSession(authDir, "codex", stateA, "a", ""); errWrite != nil {
		t.Fatalf("write callback A: %v", errWrite)
	}
	if statusA := waitForOAuthStatus(t, router, stateA); statusA.Status != "ok" {
		t.Fatalf("session A status = %#v, want ok", statusA)
	}
	if hitsA.Load() != 1 || hitsB.Load() != 1 || directHits.Load() != 0 {
		t.Fatalf("after A callback: hitsA=%d hitsB=%d direct=%d, want 1, 1, 0", hitsA.Load(), hitsB.Load(), directHits.Load())
	}

	entries, errDir := os.ReadDir(authDir)
	if errDir != nil {
		t.Fatalf("read authDir: %v", errDir)
	}
	foundByEmail := make(map[string]string)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, errRead := os.ReadFile(filepath.Join(authDir, entry.Name()))
		if errRead != nil {
			t.Fatalf("read %s: %v", entry.Name(), errRead)
		}
		var data map[string]any
		if errJSON := json.Unmarshal(raw, &data); errJSON != nil {
			t.Fatalf("unmarshal %s: %v", entry.Name(), errJSON)
		}
		email, _ := data["email"].(string)
		proxyURL, _ := data["proxy_url"].(string)
		foundByEmail[email] = proxyURL
	}
	if foundByEmail["user-a@example.test"] != proxyA.URL {
		t.Fatalf("user-a proxy_url = %q, want %q", foundByEmail["user-a@example.test"], proxyA.URL)
	}
	if foundByEmail["user-b@example.test"] != proxyB.URL {
		t.Fatalf("user-b proxy_url = %q, want %q", foundByEmail["user-b@example.test"], proxyB.URL)
	}
}

func TestStartOAuthV8WithoutProxyPreservesDefaultBehaviorAndPersistedProxyRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var proxyHits, directHits atomic.Int32
	proxyServer := startForwardHTTPProxy(t, &proxyHits, "", false)
	defer proxyServer.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Session-Proxy") != "1" {
			directHits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"default-codex@example.test"}`))
	}))
	defer upstream.Close()

	originalFactory := newCodexOAuthService
	newCodexOAuthService = func(cfg *config.Config) codexOAuthService {
		var sdkCfg config.SDKConfig
		if cfg != nil {
			sdkCfg = cfg.SDKConfig
		}
		return &proxyTestCodexOAuthService{
			client:   util.SetProxy(&sdkCfg, &http.Client{}),
			tokenURL: upstream.URL + "/oauth/token",
		}
	}
	defer func() {
		newCodexOAuthService = originalFactory
	}()

	authDir := t.TempDir()
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir, Port: 8317}, nil)
	router := gin.New()
	router.POST("/v8/management/oauth/auth-url", h.StartOAuthV8)
	router.GET("/status", h.GetAuthStatus)

	// 1. Default behavior without proxy_url override
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v8/management/oauth/auth-url", strings.NewReader(`{"provider":"codex"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var startResp struct {
		State string `json:"state"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &startResp)
	t.Cleanup(func() { CancelOAuthSession(startResp.State) })
	if _, errWrite := WriteOAuthCallbackFileForPendingSession(authDir, "codex", startResp.State, "no-proxy", ""); errWrite != nil {
		t.Fatalf("write callback: %v", errWrite)
	}
	if status := waitForOAuthStatus(t, router, startResp.State); status.Status != "ok" {
		t.Fatalf("status = %#v, want ok", status)
	}
	if directHits.Load() != 1 || proxyHits.Load() != 0 {
		t.Fatalf("without override: directHits=%d proxyHits=%d, want 1 and 0", directHits.Load(), proxyHits.Load())
	}
	entries, _ := os.ReadDir(authDir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, _ := os.ReadFile(filepath.Join(authDir, e.Name()))
		if strings.Contains(string(raw), "proxy_url") {
			t.Fatalf("credential without session proxy should not contain proxy_url: %s", string(raw))
		}
	}

}
