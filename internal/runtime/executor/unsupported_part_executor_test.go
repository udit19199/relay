package executor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	translatorcommon "github.com/udit19199/relay/internal/translator/common"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

// A turn whose only content is a file the target cannot receive must be refused
// before any upstream call, instead of forwarding an empty turn.
func TestExecutorsRefuseAFileOnlyTurnBeforeCallingUpstream(t *testing.T) {
	var upstreamCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const (
		claudeFile = `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":[{"type":"container_upload","file_id":"file_not_stored"}]}]}`
		openAIFile = `{"model":"m","messages":[{"role":"user","content":[{"type":"file","file":{"file_id":"file-not-stored"}}]}]}`
		// The attachment-only user turn is followed by a developer step that must not hide it.
		interactionsDeveloper = `{"model":"m","input":[{"type":"user_input","content":[{"type":"text","text":"hello"}]},{"type":"model_output","content":[{"type":"text","text":"hi"}]},{"type":"user_input","content":[{"type":"document","uri":"gs://b/a.pdf"}]},{"type":"user_input","role":"developer","content":[{"type":"text","text":"note"}]}]}`
		// The same emptied audio turn is followed by an instruction step that names itself by role or by type alone.
		interactionsCodexAudioPrefix        = `{"model":"m","input":[{"type":"user_input","content":[{"type":"text","text":"hello"}]},{"type":"model_output","content":[{"type":"text","text":"hi"}]},{"type":"user_input","content":[{"type":"audio","uri":"gs://b/a.wav"}]},`
		interactionsCodexAudioRoleDeveloper = interactionsCodexAudioPrefix + `{"type":"user_input","role":"developer","content":[{"type":"text","text":"note"}]}]}`
		interactionsCodexAudioTypeSystem    = interactionsCodexAudioPrefix + `{"type":"system","content":[{"type":"text","text":"note"}]}]}`
		interactionsCodexAudioTypeDeveloper = interactionsCodexAudioPrefix + `{"type":"developer","content":[{"type":"text","text":"note"}]}]}`
	)
	keyAuth := func(provider string, extra map[string]string) *coreauth.Auth {
		attrs := map[string]string{"api_key": "k", "base_url": server.URL}
		for k, v := range extra {
			attrs[k] = v
		}
		return &coreauth.Auth{Provider: provider, Attributes: attrs}
	}
	targets := []struct {
		name     string
		exec     coreauth.ProviderExecutor
		auth     *coreauth.Auth
		source   string
		payload  string
		wantPart string
	}{
		{"claude to openai-compat", NewOpenAICompatExecutor("openai-compatibility", &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{Name: "compat"}}}),
			keyAuth("openai-compatibility", map[string]string{"base_url": server.URL + "/v1", "compat_name": "compat", "provider_key": "compat"}), "claude", claudeFile, "container_upload"},
		{"claude to codex", NewCodexExecutor(&config.Config{}), keyAuth("codex", map[string]string{"plan_type": "pro"}), "claude", claudeFile, "container_upload"},
		{"openai to claude", NewClaudeExecutor(&config.Config{}), keyAuth("claude", nil), "openai", openAIFile, "file"},
		{"interactions developer step to claude", NewClaudeExecutor(&config.Config{}), keyAuth("claude", nil), "interactions", interactionsDeveloper, "document"},
		{"interactions developer role to codex", NewCodexExecutor(&config.Config{}), keyAuth("codex", map[string]string{"plan_type": "pro"}), "interactions", interactionsCodexAudioRoleDeveloper, "audio"},
		{"interactions system type to codex", NewCodexExecutor(&config.Config{}), keyAuth("codex", map[string]string{"plan_type": "pro"}), "interactions", interactionsCodexAudioTypeSystem, "audio"},
		{"interactions developer type to codex", NewCodexExecutor(&config.Config{}), keyAuth("codex", map[string]string{"plan_type": "pro"}), "interactions", interactionsCodexAudioTypeDeveloper, "audio"},
	}
	for _, target := range targets {
		req := coreexecutor.Request{Model: "m", Payload: []byte(target.payload)}
		opts := func(stream bool) coreexecutor.Options {
			return coreexecutor.Options{SourceFormat: translatorreg.FromString(target.source), Stream: stream}
		}
		calls := map[string]func() error{
			"Execute": func() error {
				_, err := target.exec.Execute(context.Background(), target.auth, req, opts(false))
				return err
			},
			"ExecuteStream": func() error {
				_, err := target.exec.ExecuteStream(context.Background(), target.auth, req, opts(true))
				return err
			},
			"CountTokens": func() error {
				_, err := target.exec.CountTokens(context.Background(), target.auth, req, opts(false))
				return err
			},
		}
		for method, call := range calls {
			err := call()
			var part *translatorcommon.UnsupportedPartError
			if !errors.As(err, &part) || part.Type != target.wantPart {
				t.Errorf("%s %s: err = %v, want unsupported %s", target.name, method, err, target.wantPart)
			}
		}
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream was called %d times, want 0", upstreamCalls)
	}
}
