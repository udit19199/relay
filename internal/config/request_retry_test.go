package config

import "testing"

func TestParseConfigBytesRequestRetry(t *testing.T) {
	cfg, errParse := ParseConfigBytes([]byte(`
codex-api-key:
  - api-key: "codex-neg"
    base-url: "https://codex.example.com"
    request-retry: -1
xai-api-key:
  - api-key: "xai-zero"
    base-url: "https://api.x.ai/v1"
    request-retry: 0
claude-api-key:
  - api-key: "claude-three"
    request-retry: 3
openai-compatibility:
  - name: "compat"
    base-url: "https://compat.example.com/v1"
    request-retry: 0
    api-key-entries:
      - api-key: "compat-key"
`))
	if errParse != nil {
		t.Fatalf("ParseConfigBytes() error = %v", errParse)
	}

	if len(cfg.CodexKey) != 1 || cfg.CodexKey[0].RequestRetry == nil || *cfg.CodexKey[0].RequestRetry != -1 {
		t.Fatalf("codex[0].request-retry = %v, want -1", valueOrNil(cfg.CodexKey))
	}
	if len(cfg.XAIKey) != 1 || cfg.XAIKey[0].RequestRetry == nil || *cfg.XAIKey[0].RequestRetry != 0 {
		t.Fatalf("xai[0].request-retry = %v, want 0", valueOrNil(cfg.XAIKey))
	}
	if len(cfg.ClaudeKey) != 1 || cfg.ClaudeKey[0].RequestRetry == nil || *cfg.ClaudeKey[0].RequestRetry != 3 {
		t.Fatalf("claude[0].request-retry = %v, want 3", valueOrNil(cfg.ClaudeKey))
	}
	if len(cfg.OpenAICompatibility) != 1 || cfg.OpenAICompatibility[0].RequestRetry == nil || *cfg.OpenAICompatibility[0].RequestRetry != 0 {
		t.Fatalf("openai-compatibility[0].request-retry = %v, want 0", cfg.OpenAICompatibility[0].RequestRetry)
	}
}

func valueOrNil[T any](items []T) any {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}
