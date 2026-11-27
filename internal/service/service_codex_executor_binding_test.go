package service

import (
	"context"
	"github.com/gorilla/websocket"
	"github.com/udit19199/relay/internal/config"
	internalconfig "github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/runtime/executor"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestEnsureExecutorsForAuth_CodexDoesNotReplaceInNormalMode(t *testing.T) {
	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	auth := &coreauth.Auth{
		ID:       "codex-auth-1",
		Provider: "codex",
		Status:   coreauth.StatusActive,
	}

	service.ensureExecutorsForAuth(auth)
	firstExecutor, okFirst := service.coreManager.Executor("codex")
	if !okFirst || firstExecutor == nil {
		t.Fatal("expected codex executor after first bind")
	}

	service.ensureExecutorsForAuth(auth)
	secondExecutor, okSecond := service.coreManager.Executor("codex")
	if !okSecond || secondExecutor == nil {
		t.Fatal("expected codex executor after second bind")
	}

	if firstExecutor != secondExecutor {
		t.Fatal("expected codex executor to stay unchanged in normal mode")
	}
}

func TestEnsureExecutorsForAuthWithMode_CodexForceReplace(t *testing.T) {
	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	auth := &coreauth.Auth{
		ID:       "codex-auth-2",
		Provider: "codex",
		Status:   coreauth.StatusActive,
	}

	service.ensureExecutorsForAuth(auth)
	firstExecutor, okFirst := service.coreManager.Executor("codex")
	if !okFirst || firstExecutor == nil {
		t.Fatal("expected codex executor after first bind")
	}

	service.ensureExecutorsForAuthWithMode(auth, true)
	secondExecutor, okSecond := service.coreManager.Executor("codex")
	if !okSecond || secondExecutor == nil {
		t.Fatal("expected codex executor after forced rebind")
	}

	if firstExecutor == secondExecutor {
		t.Fatal("expected codex executor replacement in force mode")
	}
}

func TestEnsureExecutorsForAuth_XAIDoesNotReplaceInNormalMode(t *testing.T) {
	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	auth := &coreauth.Auth{
		ID:       "xai-auth-1",
		Provider: "xai",
		Status:   coreauth.StatusActive,
	}

	service.ensureExecutorsForAuth(auth)
	firstExecutor, okFirst := service.coreManager.Executor("xai")
	if !okFirst || firstExecutor == nil {
		t.Fatal("expected xai executor after first bind")
	}
	if _, isXAIAutoExecutor := firstExecutor.(*executor.XAIAutoExecutor); !isXAIAutoExecutor {
		t.Fatalf("xai executor type = %T, want *executor.XAIAutoExecutor", firstExecutor)
	}

	service.ensureExecutorsForAuth(auth)
	secondExecutor, okSecond := service.coreManager.Executor("xai")
	if !okSecond || secondExecutor == nil {
		t.Fatal("expected xai executor after second bind")
	}
	if firstExecutor != secondExecutor {
		t.Fatal("expected xai executor to stay unchanged in normal mode")
	}
}

func TestEnsureExecutorsForAuthWithMode_XAIForceReplace(t *testing.T) {
	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	auth := &coreauth.Auth{
		ID:       "xai-auth-2",
		Provider: "xai",
		Status:   coreauth.StatusActive,
	}

	service.ensureExecutorsForAuth(auth)
	firstExecutor, okFirst := service.coreManager.Executor("xai")
	if !okFirst || firstExecutor == nil {
		t.Fatal("expected xai executor after first bind")
	}

	service.ensureExecutorsForAuthWithMode(auth, true)
	secondExecutor, okSecond := service.coreManager.Executor("xai")
	if !okSecond || secondExecutor == nil {
		t.Fatal("expected xai executor after forced rebind")
	}
	if firstExecutor == secondExecutor {
		t.Fatal("expected xai executor replacement in force mode")
	}
	if _, isXAIAutoExecutor := secondExecutor.(*executor.XAIAutoExecutor); !isXAIAutoExecutor {
		t.Fatalf("xai executor type = %T, want *executor.XAIAutoExecutor", secondExecutor)
	}
}

func TestEnsureExecutorsForAuth_XAIReplacesExecutorAfterConfigUpdate(t *testing.T) {
	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	auth := &coreauth.Auth{
		ID:       "xai-auth-config-update",
		Provider: "xai",
		Status:   coreauth.StatusActive,
	}

	service.ensureExecutorsForAuth(auth)
	firstExecutor, okFirst := service.coreManager.Executor("xai")
	if !okFirst || firstExecutor == nil {
		t.Fatal("expected xai executor before config update")
	}

	service.applyWatcherConfigUpdate(&config.Config{})
	service.ensureExecutorsForAuth(auth)

	secondExecutor, okSecond := service.coreManager.Executor("xai")
	if !okSecond || secondExecutor == nil {
		t.Fatal("expected xai executor after config update")
	}
	if firstExecutor == secondExecutor {
		t.Fatal("expected stale xai executor replacement after config update")
	}
	if _, isXAIAutoExecutor := secondExecutor.(*executor.XAIAutoExecutor); !isXAIAutoExecutor {
		t.Fatalf("xai executor type = %T, want *executor.XAIAutoExecutor", secondExecutor)
	}
}

func TestApplyWatcherConfigUpdate_PreservesExistingWebsocketConnections(t *testing.T) {
	testCases := []struct {
		name     string
		provider string
		model    string
	}{
		{name: "codex", provider: "codex", model: "gpt-6-astra"},
		{name: "xai", provider: "xai", model: "grok-4.6"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			var connections atomic.Int32
			var closedConnections atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, errUpgrade := upgrader.Upgrade(w, r, nil)
				if errUpgrade != nil {
					t.Errorf("upgrade websocket: %v", errUpgrade)
					return
				}
				connections.Add(1)
				defer func() {
					closedConnections.Add(1)
					_ = conn.Close()
				}()
				for {
					if _, _, errRead := conn.ReadMessage(); errRead != nil {
						return
					}
					completed := []byte(`{"type":"response.completed","response":{"id":"resp-1","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
					if errWrite := conn.WriteMessage(websocket.TextMessage, completed); errWrite != nil {
						return
					}
				}
			}))
			defer server.Close()

			initialCfg := &config.Config{
				SDKConfig: config.SDKConfig{
					DisableImageGeneration: internalconfig.DisableImageGenerationAll,
				},
			}
			manager := coreauth.NewManager(nil, nil, nil)
			service := &Service{
				cfg:         initialCfg,
				coreManager: manager,
			}

			auth := &coreauth.Auth{
				ID:       tt.provider + "-ws-hot-reload-auth",
				Provider: tt.provider,
				Status:   coreauth.StatusActive,
				Attributes: map[string]string{
					"base_url":   server.URL,
					"websockets": "true",
				},
				Metadata: map[string]any{
					"access_token": "test-token",
					"websockets":   true,
				},
			}
			if _, errReg := manager.Register(context.Background(), auth); errReg != nil {
				t.Fatalf("register auth: %v", errReg)
			}
			GlobalModelRegistry().RegisterClient(auth.ID, tt.provider, []*ModelInfo{{ID: tt.model}})
			t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(auth.ID) })
			service.ensureExecutorsForAuth(auth)

			sessionID := "hot-reload-ws-session-" + tt.provider
			t.Cleanup(func() { manager.CloseExecutionSession(sessionID) })

			runTurn := func(ctx context.Context, payload string) {
				t.Helper()
				result, errStream := manager.ExecuteStream(
					ctx,
					[]string{tt.provider},
					coreexecutor.Request{Model: tt.model, Payload: []byte(payload)},
					coreexecutor.Options{
						Stream:         true,
						SourceFormat:   translatorreg.FormatOpenAIResponse,
						ResponseFormat: translatorreg.FormatOpenAIResponse,
						Metadata: map[string]any{
							coreexecutor.ExecutionSessionMetadataKey: sessionID,
							coreexecutor.PinnedAuthMetadataKey:       auth.ID,
						},
					},
				)
				if errStream != nil {
					t.Fatalf("ExecuteStream error = %v", errStream)
				}
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						t.Fatalf("ExecuteStream chunk error = %v", chunk.Err)
					}
				}
			}

			wsCtx := coreexecutor.WithDownstreamWebsocket(context.Background())
			runTurn(wsCtx, `{"model":"`+tt.model+`","input":[{"type":"message","role":"user","content":"turn 1"}]}`)
			if got := connections.Load(); got != 1 {
				t.Fatalf("connections after turn 1 = %d, want 1", got)
			}

			reloadedCfg := &config.Config{
				SDKConfig: config.SDKConfig{
					ProxyURL:               "http://127.0.0.1:1",
					DisableImageGeneration: internalconfig.DisableImageGenerationAll,
				},
				Routing: internalconfig.RoutingConfig{
					Strategy: "fill-first",
				},
			}
			service.applyWatcherConfigUpdate(reloadedCfg)

			updatedExec, okUpdated := manager.Executor(tt.provider)
			if !okUpdated || updatedExec == nil {
				t.Fatalf("expected %s executor after config hot reload", tt.provider)
			}
			if cfgUser, okCfgUser := updatedExec.(interface{ UsesConfig(*config.Config) bool }); !okCfgUser || !cfgUser.UsesConfig(service.currentConfig()) {
				t.Fatalf("expected %s executor to use reloaded config", tt.provider)
			}
			if got := closedConnections.Load(); got != 0 {
				t.Fatalf("closed upstream connections after config hot reload = %d, want 0", got)
			}

			continuationCtx := coreexecutor.WithRequiredUpstreamWebsocket(wsCtx)
			runTurn(continuationCtx, `{"model":"`+tt.model+`","previous_response_id":"resp-1","input":[{"type":"message","role":"user","content":"turn 2"}]}`)
			if got := connections.Load(); got != 1 {
				t.Fatalf("connections after continuation turn = %d, want 1 (reused existing connection)", got)
			}

			newSessionID := sessionID + "-new"
			t.Cleanup(func() { manager.CloseExecutionSession(newSessionID) })
			_, errNewStream := manager.ExecuteStream(
				wsCtx,
				[]string{tt.provider},
				coreexecutor.Request{Model: tt.model, Payload: []byte(`{"model":"` + tt.model + `","input":[{"type":"message","role":"user","content":"new session"}]}`)},
				coreexecutor.Options{
					Stream:         true,
					SourceFormat:   translatorreg.FormatOpenAIResponse,
					ResponseFormat: translatorreg.FormatOpenAIResponse,
					Metadata: map[string]any{
						coreexecutor.ExecutionSessionMetadataKey: newSessionID,
						coreexecutor.PinnedAuthMetadataKey:       auth.ID,
					},
				},
			)
			if errNewStream == nil {
				t.Fatal("expected new session after config hot reload to use new proxy configuration and fail to dial 127.0.0.1:1")
			}
		})
	}
}

func TestRegisterExecutorsForAuths_DisabledXAIDoesNotSkipSubsequentForceReplace(t *testing.T) {
	initialCfg := &config.Config{}
	reloadedCfg := &config.Config{}
	service := &Service{
		cfg:         initialCfg,
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	disabledAuth := &coreauth.Auth{
		ID:       "xai-disabled",
		Provider: "xai",
		Disabled: true,
	}
	activeAuth := &coreauth.Auth{
		ID:       "xai-active",
		Provider: "xai",
		Status:   coreauth.StatusActive,
	}

	service.ensureExecutorsForAuth(activeAuth)
	firstExecutor, okFirst := service.coreManager.Executor("xai")
	if !okFirst || firstExecutor == nil {
		t.Fatal("expected initial xai executor")
	}

	service.cfg = reloadedCfg
	service.registerExecutorsForAuths([]*coreauth.Auth{disabledAuth, activeAuth}, true)

	secondExecutor, okSecond := service.coreManager.Executor("xai")
	if !okSecond || secondExecutor == nil {
		t.Fatal("expected xai executor after force replace")
	}
	if firstExecutor == secondExecutor {
		t.Fatal("expected active xai auth after disabled xai auth to replace executor")
	}
}
