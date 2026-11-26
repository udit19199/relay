package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestCustomMagicHeaders_OpenAICompat(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name: "compat",
		}},
	})
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url":                        server.URL,
			"api_key":                         "test-key",
			"header:X-Claude-Code-Session-Id": "$ABC",
			"header:X-Forwarded-Session":      "$X-Client-Session",
			"header:X-Missing":                "$NONEXISTENT",
			"header:X-Static":                 "static-value",
		},
	}

	req := cliproxyexecutor.Request{
		Model:   "gpt-4o",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAI,
		Headers: http.Header{
			"Abc":              []string{"session-abc-value"},
			"X-Client-Session": []string{"client-session-uuid-123"},
		},
	}

	_, err := executor.Execute(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := gotHeaders.Get("X-Claude-Code-Session-Id"); got != "session-abc-value" {
		t.Errorf("X-Claude-Code-Session-Id = %q, want %q", got, "session-abc-value")
	}
	if got := gotHeaders.Get("X-Forwarded-Session"); got != "client-session-uuid-123" {
		t.Errorf("X-Forwarded-Session = %q, want %q", got, "client-session-uuid-123")
	}
	if got := gotHeaders.Get("X-Static"); got != "static-value" {
		t.Errorf("X-Static = %q, want %q", got, "static-value")
	}
	if _, exists := gotHeaders["X-Missing"]; exists {
		t.Errorf("expected X-Missing to be omitted, got %q", gotHeaders.Get("X-Missing"))
	}
}

func TestOpenAICompatExecutor_UserAgentPassthroughAndDefault(t *testing.T) {
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{Name: "compat"}},
	})
	baseAuth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url": server.URL,
			"api_key":  "test-key",
		},
	}
	req := cliproxyexecutor.Request{
		Model:   "gpt-4o",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}

	t.Run("Execute defaults to cli-proxy-openai-compat when downstream User-Agent is absent", func(t *testing.T) {
		gotUserAgent = ""
		_, err := executor.Execute(context.Background(), baseAuth, req, cliproxyexecutor.Options{
			SourceFormat: sdktranslator.FormatOpenAI,
		})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if gotUserAgent != "cli-proxy-openai-compat" {
			t.Fatalf("User-Agent = %q, want cli-proxy-openai-compat", gotUserAgent)
		}
	})

	t.Run("Execute forwards downstream User-Agent when present", func(t *testing.T) {
		gotUserAgent = ""
		_, err := executor.Execute(context.Background(), baseAuth, req, cliproxyexecutor.Options{
			SourceFormat: sdktranslator.FormatOpenAI,
			Headers:      http.Header{"User-Agent": []string{"downstream-compat-client/1.0"}},
		})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if gotUserAgent != "downstream-compat-client/1.0" {
			t.Fatalf("User-Agent = %q, want downstream-compat-client/1.0", gotUserAgent)
		}
	})

	t.Run("ExecuteStream forwards downstream User-Agent when present", func(t *testing.T) {
		gotUserAgent = ""
		streamRes, err := executor.ExecuteStream(context.Background(), baseAuth, req, cliproxyexecutor.Options{
			SourceFormat: sdktranslator.FormatOpenAI,
			Stream:       true,
			Headers:      http.Header{"User-Agent": []string{"downstream-stream-client/1.0"}},
		})
		if err != nil {
			t.Fatalf("ExecuteStream() error = %v", err)
		}
		for range streamRes.Chunks {
		}
		if gotUserAgent != "downstream-stream-client/1.0" {
			t.Fatalf("User-Agent = %q, want downstream-stream-client/1.0", gotUserAgent)
		}
	})

	t.Run("Execute images and ExecuteStream images forward downstream User-Agent", func(t *testing.T) {
		imageReq := cliproxyexecutor.Request{
			Model:   "gpt-image-1",
			Payload: []byte(`{"model":"gpt-image-1","prompt":"draw a cat"}`),
		}
		gotUserAgent = ""
		_, err := executor.Execute(context.Background(), baseAuth, imageReq, cliproxyexecutor.Options{
			SourceFormat: sdktranslator.FromString(openAICompatImageHandlerType),
			Headers:      http.Header{"User-Agent": []string{"downstream-image-client/1.0"}},
		})
		if err != nil {
			t.Fatalf("Execute(images) error = %v", err)
		}
		if gotUserAgent != "downstream-image-client/1.0" {
			t.Fatalf("Execute(images) User-Agent = %q, want downstream-image-client/1.0", gotUserAgent)
		}

		gotUserAgent = ""
		streamRes, err := executor.ExecuteStream(context.Background(), baseAuth, imageReq, cliproxyexecutor.Options{
			SourceFormat: sdktranslator.FromString(openAICompatImageHandlerType),
			Stream:       true,
			Headers:      http.Header{"User-Agent": []string{"downstream-image-stream-client/1.0"}},
		})
		if err != nil {
			t.Fatalf("ExecuteStream(images) error = %v", err)
		}
		for range streamRes.Chunks {
		}
		if gotUserAgent != "downstream-image-stream-client/1.0" {
			t.Fatalf("ExecuteStream(images) User-Agent = %q, want downstream-image-stream-client/1.0", gotUserAgent)
		}
	})

	t.Run("configured header:User-Agent overrides downstream User-Agent", func(t *testing.T) {
		gotUserAgent = ""
		overrideAuth := &cliproxyauth.Auth{
			Provider: "openai-compatibility",
			Attributes: map[string]string{
				"base_url":          server.URL,
				"api_key":           "test-key",
				"header:User-Agent": "configured-compat-ua/3.0",
			},
		}
		_, err := executor.Execute(context.Background(), overrideAuth, req, cliproxyexecutor.Options{
			SourceFormat: sdktranslator.FormatOpenAI,
			Headers:      http.Header{"User-Agent": []string{"downstream-compat-client/1.0"}},
		})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if gotUserAgent != "configured-compat-ua/3.0" {
			t.Fatalf("User-Agent = %q, want configured-compat-ua/3.0", gotUserAgent)
		}
	})
}

func TestCustomMagicHeaders_XAI(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"created_at\":0,\"status\":\"completed\",\"background\":false,\"error\":null,\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	executor := NewXAIExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "xai",
		Attributes: map[string]string{
			"base_url":                        server.URL,
			"api_key":                         "xai-key",
			"header:X-Claude-Code-Session-Id": "$ABC",
			"header:X-Missing":                "$NONEXISTENT",
		},
	}

	req := cliproxyexecutor.Request{
		Model:   "grok-2",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAI,
		Headers: http.Header{
			"ABC": []string{"xai-session-value"},
		},
	}

	_, err := executor.Execute(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := gotHeaders.Get("X-Claude-Code-Session-Id"); got != "xai-session-value" {
		t.Errorf("X-Claude-Code-Session-Id = %q, want %q", got, "xai-session-value")
	}
	if _, exists := gotHeaders["X-Missing"]; exists {
		t.Errorf("expected X-Missing to be omitted, got %q", gotHeaders.Get("X-Missing"))
	}
}

func TestCustomMagicHeaders_Claude(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}]}`))
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "claude",
		Attributes: map[string]string{
			"base_url":                        server.URL,
			"api_key":                         "sk-ant-test",
			"header:X-Claude-Code-Session-Id": "$ABC",
			"header:X-Missing":                "$NONEXISTENT",
		},
	}

	req := cliproxyexecutor.Request{
		Model:   "claude-3-7-sonnet-20250219",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatClaude,
		Headers: http.Header{
			"Abc": []string{"claude-session-value"},
		},
	}

	_, err := executor.Execute(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := gotHeaders.Get("X-Claude-Code-Session-Id"); got != "claude-session-value" {
		t.Errorf("X-Claude-Code-Session-Id = %q, want %q", got, "claude-session-value")
	}
	if _, exists := gotHeaders["X-Missing"]; exists {
		t.Errorf("expected X-Missing to be omitted, got %q", gotHeaders.Get("X-Missing"))
	}
}

func TestCustomMagicHeaders_OpenAICompat_Stream(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name: "compat",
		}},
	})
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url":                        server.URL,
			"api_key":                         "test-key",
			"header:X-Claude-Code-Session-Id": "$ABC",
			"header:X-Empty-Var":              "$   ",
			"header:X-Only-Dollar":            "$",
			"header:X-Missing":                "$NONEXISTENT",
		},
	}

	req := cliproxyexecutor.Request{
		Model:   "gpt-4o",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAI,
		Stream:       true,
		Headers: http.Header{
			"Abc": []string{"stream-session-abc"},
		},
	}

	result, err := executor.ExecuteStream(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	for range result.Chunks {
	}

	if got := gotHeaders.Get("X-Claude-Code-Session-Id"); got != "stream-session-abc" {
		t.Errorf("X-Claude-Code-Session-Id = %q, want %q", got, "stream-session-abc")
	}
	if _, exists := gotHeaders["X-Missing"]; exists {
		t.Errorf("expected X-Missing to be omitted, got %q", gotHeaders.Get("X-Missing"))
	}
	if _, exists := gotHeaders["X-Empty-Var"]; exists {
		t.Errorf("expected X-Empty-Var to be omitted, got %q", gotHeaders.Get("X-Empty-Var"))
	}
	if _, exists := gotHeaders["X-Only-Dollar"]; exists {
		t.Errorf("expected X-Only-Dollar to be omitted, got %q", gotHeaders.Get("X-Only-Dollar"))
	}
}

func TestCustomMagicHeaders_Codex(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		_ = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"created_at\":0,\"status\":\"completed\",\"background\":false,\"error\":null,\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	executor := NewCodexExecutor(&config.Config{
		Codex: config.CodexConfig{
			DisableCodexCloaking: true,
		},
	})
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		Attributes: map[string]string{
			"base_url":                        server.URL,
			"api_key":                         "codex-key",
			"header:X-Claude-Code-Session-Id": "$ABC",
			"header:X-Missing":                "$NONEXISTENT",
		},
	}

	req := cliproxyexecutor.Request{
		Model:   "gpt-5-codex",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatCodex,
		Headers: http.Header{
			"Abc": []string{"codex-session-value"},
		},
	}

	_, err := executor.Execute(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := gotHeaders.Get("X-Claude-Code-Session-Id"); got != "codex-session-value" {
		t.Errorf("X-Claude-Code-Session-Id = %q, want %q", got, "codex-session-value")
	}
	if _, exists := gotHeaders["X-Missing"]; exists {
		t.Errorf("expected X-Missing to be omitted, got %q", gotHeaders.Get("X-Missing"))
	}
}

func TestCustomMagicHeaders_CPASessionID_OpenAICompat(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name: "compat",
		}},
	})
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url":             server.URL,
			"api_key":              "test-key",
			"header:X-Session":     "$CPA-SESSION-ID",
			"header:Authorization": "Bearer $CPA-SESSION-ID",
			"header:X-Lower-Case":  "$cpa-session-id",
		},
	}

	req := cliproxyexecutor.Request{
		Model:   "gpt-4o",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAI,
		Headers: http.Header{
			"Session-Id": []string{"codex-thread-abc"},
		},
	}

	_, err := executor.Execute(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	wantSession := "codex:codex-thread-abc"
	if got := gotHeaders.Get("X-Session"); got != wantSession {
		t.Errorf("X-Session = %q, want %q", got, wantSession)
	}
	if got := gotHeaders.Get("Authorization"); got != "Bearer "+wantSession {
		t.Errorf("Authorization = %q, want %q", got, "Bearer "+wantSession)
	}
	if got := gotHeaders.Get("X-Lower-Case"); got != wantSession {
		t.Errorf("X-Lower-Case = %q, want %q", got, wantSession)
	}
}

func TestCustomMagicHeaders_CPASessionID_Claude(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}]}`))
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "claude",
		Attributes: map[string]string{
			"base_url":         server.URL,
			"api_key":          "sk-ant-test",
			"header:X-Session": "$CPA-SESSION-ID",
		},
	}

	req := cliproxyexecutor.Request{
		Model:   "claude-3-7-sonnet-20250219",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatClaude,
		Headers: http.Header{
			"X-Claude-Code-Session-Id": []string{"claude-sess-789"},
		},
	}

	_, err := executor.Execute(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	wantSession := "claude:claude-sess-789"
	if got := gotHeaders.Get("X-Session"); got != wantSession {
		t.Errorf("X-Session = %q, want %q", got, wantSession)
	}
}

func TestCustomMagicHeaders_CPASessionID_Codex(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"created_at\":0,\"status\":\"completed\",\"background\":false,\"error\":null,\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	executor := NewCodexExecutor(&config.Config{
		Codex: config.CodexConfig{
			DisableCodexCloaking: true,
		},
	})
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		Attributes: map[string]string{
			"base_url":         server.URL,
			"api_key":          "codex-key",
			"header:X-Session": "$CPA-SESSION-ID",
		},
	}

	req := cliproxyexecutor.Request{
		Model:   "gpt-5-codex",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatCodex,
		Headers: http.Header{
			"Session-Id": []string{"codex-sess-uuid"},
		},
	}

	_, err := executor.Execute(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	wantSession := "codex:codex-sess-uuid"
	if got := gotHeaders.Get("X-Session"); got != wantSession {
		t.Errorf("X-Session = %q, want %q", got, wantSession)
	}
}

func TestCustomMagicHeaders_CPASessionID_XAI(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"created_at\":0,\"status\":\"completed\",\"background\":false,\"error\":null,\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	executor := NewXAIExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "xai",
		Attributes: map[string]string{
			"base_url":         server.URL,
			"api_key":          "xai-key",
			"header:X-Session": "$CPA-SESSION-ID",
		},
	}

	req := cliproxyexecutor.Request{
		Model:   "grok-2",
		Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAI,
		Headers: http.Header{
			"X-Session-ID": []string{"xai-sess-111"},
		},
	}

	_, err := executor.Execute(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	wantSession := "header:xai-sess-111"
	if got := gotHeaders.Get("X-Session"); got != wantSession {
		t.Errorf("X-Session = %q, want %q", got, wantSession)
	}
}

func TestCustomMagicHeaders_CPASessionID_SessionAffinityOnAndOff(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	for _, sessionAffinity := range []bool{true, false} {
		t.Run(map[bool]string{true: "SessionAffinity=true", false: "SessionAffinity=false"}[sessionAffinity], func(t *testing.T) {
			gotHeaders = nil
			cfg := &config.Config{
				Routing: config.RoutingConfig{
					SessionAffinity: sessionAffinity,
				},
				OpenAICompatibility: []config.OpenAICompatibility{{
					Name: "compat",
					Headers: map[string]string{
						"X-CPA-Session": "$CPA-SESSION-ID",
					},
				}},
			}

			executor := NewOpenAICompatExecutor("openai-compatibility", cfg)
			auth := &cliproxyauth.Auth{
				ID:       "auth-compat-1",
				Provider: "openai-compatibility",
				Attributes: map[string]string{
					"base_url":             server.URL,
					"api_key":              "key-1",
					"header:X-CPA-Session": "$CPA-SESSION-ID",
				},
			}

			req := cliproxyexecutor.Request{
				Model:   "gpt-4o",
				Payload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
			}
			opts := cliproxyexecutor.Options{
				SourceFormat: sdktranslator.FormatOpenAI,
				Headers: http.Header{
					"X-Session-ID": []string{"aff-test-sess-99"},
				},
			}

			_, err := executor.Execute(context.Background(), auth, req, opts)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			wantSession := "header:aff-test-sess-99"
			if got := gotHeaders.Get("X-CPA-Session"); got != wantSession {
				t.Errorf("X-CPA-Session = %q, want %q (sessionAffinity=%v)", got, wantSession, sessionAffinity)
			}
		})
	}
}
