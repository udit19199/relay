package config

import "testing"

func TestParseConfigBytesPreservesCoolingOverridePresence(t *testing.T) {
	cfg, errParse := ParseConfigBytes([]byte(`
disable-cooling: true
claude-api-key:
  - api-key: claude-key
    disable-cooling: false
codex-api-key:
  - api-key: codex-key
    base-url: https://codex.example.com
    disable-cooling: false
xai-api-key:
  - api-key: xai-key
    base-url: https://api.x.ai/v1
    disable-cooling: false
openai-compatibility:
  - name: compat
    base-url: https://compat.example.com
    disable-cooling: false
    api-key-entries:
      - api-key: compat-key
`))
	if errParse != nil {
		t.Fatalf("ParseConfigBytes() error = %v", errParse)
	}

	overrides := map[string]*bool{
		"claude":               cfg.ClaudeKey[0].DisableCooling,
		"codex":                cfg.CodexKey[0].DisableCooling,
		"xai":                  cfg.XAIKey[0].DisableCooling,
		"openai compatibility": cfg.OpenAICompatibility[0].DisableCooling,
	}
	for name, override := range overrides {
		if override == nil || *override {
			t.Errorf("%s disable-cooling = %v, want explicit false", name, override)
		}
	}
}
