package helps_test

import (
	"context"
	"net/http"

	"testing"

	"github.com/MoeForge/moego/ojson"
	internalconfig "github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/registry"
	helps "github.com/udit19199/relay/internal/runtime/executor/helps"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	_ "github.com/udit19199/relay/internal/thinking/provider/claude"
	_ "github.com/udit19199/relay/internal/thinking/provider/codex"
	_ "github.com/udit19199/relay/internal/thinking/provider/openai"
	_ "github.com/udit19199/relay/internal/translator"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

type configuredThinkingExecutor struct {
	seenModel        string
	resolved         bool
	selectedInfo     *registry.ModelInfo
	provider         string
	targetFormat     string
	body             []byte
	translateRequest bool
	translatedBody   []byte
}

func (e *configuredThinkingExecutor) Identifier() string {
	if e.provider != "" {
		return e.provider
	}
	return "claude"
}

func (e *configuredThinkingExecutor) Execute(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	e.seenModel = req.Model
	modelInfo, resolved := coreauth.ResolvedAPIKeyModelInfo(req)
	e.resolved = resolved && modelInfo != nil
	e.selectedInfo, _ = coreauth.ResolvedModelInfo(req)
	body := []byte(`{"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}}`)
	if e.body != nil {
		body = e.body
	}
	if e.translateRequest {
		body = translatorreg.TranslateRequest(opts.SourceFormat, translatorreg.FormatClaude, req.Model, req.Payload, opts.Stream)
		e.translatedBody = append(e.translatedBody[:0], body...)
	}
	toFormat := e.targetFormat
	if toFormat == "" {
		toFormat = "claude"
	}
	out, err := helps.ApplyRequestThinking(body, req, opts, opts.SourceFormat.String(), toFormat, e.Identifier())
	return coreexecutor.Response{Payload: out}, err
}

func (e *configuredThinkingExecutor) ExecuteStream(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	response, err := e.Execute(ctx, auth, req, opts)
	if err != nil {
		return nil, err
	}
	chunks := make(chan coreexecutor.StreamChunk, 1)
	chunks <- coreexecutor.StreamChunk{Payload: response.Payload}
	close(chunks)
	return &coreexecutor.StreamResult{Chunks: chunks}, nil
}

func (*configuredThinkingExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *configuredThinkingExecutor) CountTokens(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}

func (*configuredThinkingExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestApplyRequestThinkingConfigurationUpdateSelectedCodexModel(t *testing.T) {
	const current = `{"reasoning":{"effort":"xhigh"},"input":[{"type":"configuration_update","reasoning":{"effort":"low"}},{"type":"configuration_update","reasoning":{"effort":"high"}},{"role":"user","content":"ok"}]}`
	const original = `{"reasoning":{"effort":"xhigh"},"input":[{"type":"configuration_update","reasoning":{"effort":"low"}},{"role":"user","content":"ok"}]}`
	const translated = `{"reasoning":{"effort":"xhigh"},"input":[{"role":"user","content":"ok"}]}`

	for _, tc := range []struct {
		name       string
		supported  bool
		model      string
		body       string
		wantTop    string
		wantUpdate int
	}{
		{name: "supported native updates pass through", supported: true, model: "tenant/public", body: current, wantTop: "xhigh", wantUpdate: 2},
		{name: "supported translated target without updates stays unchanged", supported: true, model: "tenant/public", body: translated, wantTop: "xhigh"},
		{name: "unsupported translated target promotes current update", model: "tenant/public", body: translated, wantTop: "high"},
		{name: "unsupported target removes stale update", model: "tenant/public", body: original, wantTop: "high"},
		{name: "unsupported suffix overrides current update", model: "tenant/public(medium)", body: translated, wantTop: "medium"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := coreauth.NewManager(nil, nil, nil)
			manager.SetConfig(&internalconfig.Config{
				SDKConfig: internalconfig.SDKConfig{},
				CodexKey: []internalconfig.CodexKey{{
					APIKey: "selected-update-key",
					Models: []internalconfig.CodexModel{{
						Name: "opaque-route", Alias: "public", SupportConfigurationUpdate: tc.supported,
						Thinking: &registry.ThinkingSupport{Levels: []string{"low", "medium", "high", "xhigh"}},
					}},
				}},
			})
			executor := &configuredThinkingExecutor{provider: "codex", targetFormat: "codex", body: []byte(tc.body)}
			manager.RegisterExecutor(executor)
			auth := &coreauth.Auth{
				ID: "selected-update-auth", Provider: "codex", Prefix: "tenant",
				Attributes: map[string]string{
					coreauth.AttributeAuthKind: coreauth.AuthKindAPIKey,
					coreauth.AttributeAPIKey:   "selected-update-key",
					coreauth.AttributeSource:   "config:codex[0]",
				},
			}
			modelRegistry := registry.GetGlobalRegistry()
			modelRegistry.RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "tenant/public", Type: "codex"}})
			t.Cleanup(func() { modelRegistry.UnregisterClient(auth.ID) })
			if registered, errRegister := manager.Register(t.Context(), auth); errRegister != nil || registered == nil {
				t.Fatalf("Register() = (%+v, %v), want auth", registered, errRegister)
			}

			response, errExecute := manager.Execute(t.Context(), []string{"codex"}, coreexecutor.Request{
				Model: tc.model, Payload: []byte(current), Format: translatorreg.FormatOpenAIResponse,
			}, coreexecutor.Options{SourceFormat: translatorreg.FormatOpenAIResponse, OriginalRequest: []byte(original)})
			if errExecute != nil {
				t.Fatalf("Execute() error = %v", errExecute)
			}
			if executor.selectedInfo == nil || executor.selectedInfo.SupportConfigurationUpdate != tc.supported {
				t.Fatalf("selected model capability = %+v, want update support %t", executor.selectedInfo, tc.supported)
			}
			wantModel := "opaque-route"
			if tc.model == "tenant/public(medium)" {
				wantModel += "(medium)"
			}
			if executor.seenModel != wantModel {
				t.Fatalf("executor model = %q, want %q", executor.seenModel, wantModel)
			}
			if got := ojson.GetBytes(response.Payload, "reasoning.effort").String(); got != tc.wantTop {
				t.Fatalf("reasoning.effort = %q, want %q; body=%s", got, tc.wantTop, response.Payload)
			}
			updates := 0
			for _, item := range ojson.GetBytes(response.Payload, "input").Array() {
				if item.Get("type").String() == "configuration_update" {
					updates++
				}
			}
			if updates != tc.wantUpdate {
				t.Fatalf("updates = %d, want %d; body=%s", updates, tc.wantUpdate, response.Payload)
			}
			if tc.supported && string(response.Payload) != tc.body {
				t.Fatalf("supported native body changed: got %s, want %s", response.Payload, tc.body)
			}
		})
	}
}

func TestApplyRequestThinkingConfigurationUpdateCrossProtocol(t *testing.T) {
	const current = `{"reasoning":{"effort":"xhigh"},"input":[{"type":"configuration_update","reasoning":{"effort":"low"}},{"type":"configuration_update","reasoning":{"effort":"high"}}]}`
	const original = `{"reasoning":{"effort":"xhigh"},"input":[{"type":"configuration_update","reasoning":{"effort":"low"}}]}`
	for _, tc := range []struct {
		name     string
		format   string
		body     string
		path     string
		want     string
		noSource bool
	}{
		{name: "Codex uses current update without bound model", format: "codex", body: `{"reasoning":{"effort":"xhigh"},"input":[{"role":"user","content":"ok"}]}`, path: "reasoning.effort", want: "high"},
		{name: "OpenAI Chat uses current update", format: "openai", body: `{"reasoning_effort":"medium","messages":[]}`, path: "reasoning_effort", want: "high"},
		{name: "Claude uses current update", format: "claude", body: `{"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"},"max_tokens":4096}`, path: "output_config.effort", want: "high"},
		{name: "empty current payload falls back to original", format: "openai", body: `{"reasoning_effort":"medium","messages":[]}`, path: "reasoning_effort", want: "low", noSource: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := coreexecutor.Request{Model: "task5-unknown-route", Payload: []byte(current)}
			if tc.noSource {
				req.Payload = nil
			}
			out, err := helps.ApplyRequestThinking([]byte(tc.body), req, coreexecutor.Options{OriginalRequest: []byte(original)}, "openai-response", tc.format, tc.format)
			if err != nil {
				t.Fatalf("ApplyRequestThinking() error = %v", err)
			}
			if got := ojson.GetBytes(out, tc.path).String(); got != tc.want {
				t.Fatalf("%s = %q, want %q; body=%s", tc.path, got, tc.want, out)
			}
		})
	}
}

func TestApplyRequestThinkingUsesExactClaudeModeForSummaryOnlyRequest(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{
		SDKConfig: internalconfig.SDKConfig{},
		ClaudeKey: []internalconfig.ClaudeKey{{
			APIKey: "summary-selected-key",
			Models: []internalconfig.ClaudeModel{{
				Name:  "summary-shared-upstream",
				Alias: "summary-public-model",
				Thinking: &registry.ThinkingSupport{
					Min: 1024,
					Max: 16000,
				},
			}},
		}},
	})
	executor := &configuredThinkingExecutor{translateRequest: true}
	manager.RegisterExecutor(executor)
	auth := &coreauth.Auth{
		ID:       "summary-selected-auth",
		Provider: "claude",
		Prefix:   "summary-tenant",
		Attributes: map[string]string{
			coreauth.AttributeAuthKind: coreauth.AuthKindAPIKey,
			coreauth.AttributeAPIKey:   "summary-selected-key",
			coreauth.AttributeSource:   "config:claude[0]",
		},
	}

	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{
		ID: "summary-tenant/summary-public-model", Type: "claude",
	}})
	modelRegistry.RegisterClient("summary-unrelated-auth", auth.Provider, []*registry.ModelInfo{{
		ID: "summary-shared-upstream", Type: "claude",
		Thinking: &registry.ThinkingSupport{Levels: []string{"high"}},
	}})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(auth.ID)
		modelRegistry.UnregisterClient("summary-unrelated-auth")
	})
	if registered, errRegister := manager.Register(t.Context(), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	} else if registered == nil {
		t.Fatal("Register() returned nil auth")
	}

	original := []byte(`{"model":"summary-tenant/summary-public-model","reasoning":{"summary":"auto"},"input":"hi"}`)
	response, errExecute := manager.Execute(t.Context(), []string{"claude"}, coreexecutor.Request{
		Model:   "summary-tenant/summary-public-model",
		Payload: original,
		Format:  translatorreg.FormatOpenAIResponse,
	}, coreexecutor.Options{
		SourceFormat:    translatorreg.FormatOpenAIResponse,
		OriginalRequest: original,
	})
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if got := ojson.GetBytes(executor.translatedBody, "thinking.type").String(); got != "adaptive" {
		t.Fatalf("pre-executor thinking.type = %q, want global adaptive trigger; body=%s", got, executor.translatedBody)
	}
	if got := ojson.GetBytes(response.Payload, "thinking.type").String(); got != "enabled" {
		t.Fatalf("thinking.type = %q, want exact manual mode; body=%s", got, response.Payload)
	}
	if got := ojson.GetBytes(response.Payload, "thinking.budget_tokens").Int(); got != 1024 {
		t.Fatalf("thinking.budget_tokens = %d, want exact minimum 1024; body=%s", got, response.Payload)
	}
	if got := ojson.GetBytes(response.Payload, "thinking.display").String(); got != "summarized" {
		t.Fatalf("thinking.display = %q, want summarized; body=%s", got, response.Payload)
	}
	if ojson.GetBytes(response.Payload, "output_config.effort").Exists() {
		t.Fatalf("manual thinking retained adaptive effort: %s", response.Payload)
	}
}

func TestApplyRequestThinkingUsesSelectedPrefixedAPIKeyModel(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{
		SDKConfig: internalconfig.SDKConfig{},
		ClaudeKey: []internalconfig.ClaudeKey{{
			APIKey: "selected-key",
			Models: []internalconfig.ClaudeModel{{
				Name: "shared-upstream", Alias: "public-model",
				Thinking: &registry.ThinkingSupport{Levels: []string{"high"}},
			}},
		}},
	})
	executor := &configuredThinkingExecutor{}
	manager.RegisterExecutor(executor)
	auth := &coreauth.Auth{
		ID:       "selected-auth",
		Provider: "claude",
		Prefix:   "tenant",
		Attributes: map[string]string{
			coreauth.AttributeAuthKind: coreauth.AuthKindAPIKey,
			coreauth.AttributeAPIKey:   "selected-key",
			coreauth.AttributeSource:   "config:claude[0]",
		},
	}

	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "tenant/public-model", Type: "claude"}})
	modelRegistry.RegisterClient("unrelated-auth", auth.Provider, []*registry.ModelInfo{{
		ID: "shared-upstream", Type: "claude",
		Thinking: &registry.ThinkingSupport{Levels: []string{"max"}},
	}})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(auth.ID)
		modelRegistry.UnregisterClient("unrelated-auth")
	})
	ctx := t.Context()
	registered, errRegister := manager.Register(ctx, auth)
	if errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}
	if registered == nil {
		t.Fatal("Register() returned nil auth")
	}

	original := []byte(`{"model":"tenant/public-model","reasoning_effort":"max","messages":[{"role":"user","content":"hello"}]}`)
	req := coreexecutor.Request{
		Model:   "tenant/public-model",
		Payload: original,
		Format:  translatorreg.FormatOpenAI,
	}
	opts := coreexecutor.Options{
		SourceFormat:    translatorreg.FormatOpenAI,
		OriginalRequest: original,
	}
	assertResponse := func(path string, payload []byte) {
		t.Helper()
		if executor.seenModel != "shared-upstream" {
			t.Fatalf("%s executor model = %q, want shared-upstream", path, executor.seenModel)
		}
		if !executor.resolved {
			t.Fatalf("%s request did not receive selected model capabilities", path)
		}
		if got := ojson.GetBytes(payload, "output_config.effort").String(); got != "high" {
			t.Fatalf("%s output effort = %q, want selected credential capability high; body=%s", path, got, payload)
		}
	}

	response, errExecute := manager.Execute(ctx, []string{"claude"}, req, opts)
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	assertResponse("execute", response.Payload)

	countResponse, errCount := manager.ExecuteCount(ctx, []string{"claude"}, req, opts)
	if errCount != nil {
		t.Fatalf("ExecuteCount() error = %v", errCount)
	}
	assertResponse("count", countResponse.Payload)

	streamResult, errStream := manager.ExecuteStream(ctx, []string{"claude"}, req, opts)
	if errStream != nil {
		t.Fatalf("ExecuteStream() error = %v", errStream)
	}
	var streamPayload []byte
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			t.Fatalf("ExecuteStream() chunk error = %v", chunk.Err)
		}
		streamPayload = append(streamPayload, chunk.Payload...)
	}
	assertResponse("stream", streamPayload)
}
