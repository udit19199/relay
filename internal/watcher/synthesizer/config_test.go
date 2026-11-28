package synthesizer

import (
	"testing"
	"time"

	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
)

func TestNewConfigSynthesizer(t *testing.T) {
	synth := NewConfigSynthesizer()
	if synth == nil {
		t.Fatal("expected non-nil synthesizer")
	}
}

func TestConfigSynthesizer_Synthesize_NilContext(t *testing.T) {
	synth := NewConfigSynthesizer()
	auths, err := synth.Synthesize(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 0 {
		t.Fatalf("expected empty auths, got %d", len(auths))
	}
}

func TestConfigSynthesizer_Synthesize_NilConfig(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config:      nil,
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 0 {
		t.Fatalf("expected empty auths, got %d", len(auths))
	}
}

func TestConfigSynthesizer_ClaudeKeys(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			ClaudeKey: []config.ClaudeKey{
				{
					APIKey:                  "sk-ant-api-xxx",
					Prefix:                  "main",
					BaseURL:                 "https://api.anthropic.com",
					DisableCooling:          boolPointer(true),
					RebuildMidSystemMessage: true,
					NativeToolSearch:        true,
					FingerprintProfile:      "claude-code-cli",
					Models: []config.ClaudeModel{
						{Name: "claude-3-opus"},
						{Name: "claude-3-sonnet"},
					},
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}

	if auths[0].Provider != "claude" {
		t.Errorf("expected provider claude, got %s", auths[0].Provider)
	}
	if auths[0].Label != "claude-apikey" {
		t.Errorf("expected label claude-apikey, got %s", auths[0].Label)
	}
	if auths[0].Prefix != "main" {
		t.Errorf("expected prefix main, got %s", auths[0].Prefix)
	}
	if auths[0].Attributes["api_key"] != "sk-ant-api-xxx" {
		t.Errorf("expected api_key sk-ant-api-xxx, got %s", auths[0].Attributes["api_key"])
	}
	if auths[0].Attributes["config_index"] != "0" {
		t.Errorf("expected config_index 0, got %s", auths[0].Attributes["config_index"])
	}
	if _, ok := auths[0].Attributes["models_hash"]; !ok {
		t.Error("expected models_hash in attributes")
	}
	if got := auths[0].Attributes["rebuild_mid_system_message"]; got != "true" {
		t.Errorf("expected rebuild_mid_system_message=true, got %s", got)
	}
	if got := auths[0].Attributes["fingerprint_profile"]; got != "claude-code-cli" {
		t.Errorf("expected fingerprint_profile=claude-code-cli, got %s", got)
	}
	if got := auths[0].Attributes["native_tool_search"]; got != "true" {
		t.Errorf("expected native_tool_search=true, got %s", got)
	}
	if v, ok := auths[0].Metadata["disable_cooling"].(bool); !ok || !v {
		t.Errorf("expected disable_cooling=true, got %v", auths[0].Metadata["disable_cooling"])
	}
}

func TestConfigSynthesizer_ClaudeKeys_NativeToolSearchDefaultOff(t *testing.T) {
	auths, err := NewConfigSynthesizer().Synthesize(&SynthesisContext{
		Config:      &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "key", BaseURL: "https://gateway.example.com"}}},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	})
	if err != nil || len(auths) != 1 {
		t.Fatalf("synthesize: auths=%d err=%v", len(auths), err)
	}
	if value, exists := auths[0].Attributes["native_tool_search"]; exists {
		t.Fatalf("native_tool_search = %q, want absent by default", value)
	}
}

func TestConfigSynthesizer_ClaudeKeys_SkipsEmptyAndHeaders(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			ClaudeKey: []config.ClaudeKey{
				{APIKey: ""},    // empty, should be skipped
				{APIKey: "   "}, // whitespace, should be skipped
				{APIKey: "valid-key", Headers: map[string]string{"X-Custom": "value"}},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth (empty keys skipped), got %d", len(auths))
	}
	if auths[0].Attributes["header:X-Custom"] != "value" {
		t.Errorf("expected header:X-Custom=value, got %s", auths[0].Attributes["header:X-Custom"])
	}
}

func TestConfigSynthesizer_CodexKeys(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			CodexKey: []config.CodexKey{
				{
					APIKey:               "codex-key-123",
					Prefix:               "dev",
					BaseURL:              "https://api.openai.com",
					ProxyURL:             "http://proxy.local",
					Websockets:           true,
					AlphaSearch:          true,
					DisableCooling:       boolPointer(true),
					DisableCodexCloaking: boolPointer(true),
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}

	if auths[0].Provider != "codex" {
		t.Errorf("expected provider codex, got %s", auths[0].Provider)
	}
	if auths[0].Label != "codex-apikey" {
		t.Errorf("expected label codex-apikey, got %s", auths[0].Label)
	}
	if auths[0].ProxyURL != "http://proxy.local" {
		t.Errorf("expected proxy_url http://proxy.local, got %s", auths[0].ProxyURL)
	}
	if auths[0].Attributes["websockets"] != "true" {
		t.Errorf("expected websockets=true, got %s", auths[0].Attributes["websockets"])
	}
	if auths[0].Attributes[coreauth.AttributeCodexAlphaSearch] != "true" {
		t.Errorf("expected codex_alpha_search=true, got %s", auths[0].Attributes[coreauth.AttributeCodexAlphaSearch])
	}
	if auths[0].Attributes[coreauth.AttributeCodexDisableCloaking] != "true" {
		t.Errorf("expected codex_disable_cloaking=true, got %s", auths[0].Attributes[coreauth.AttributeCodexDisableCloaking])
	}
	if v, ok := auths[0].Metadata["disable_cooling"].(bool); !ok || !v {
		t.Errorf("expected disable_cooling=true, got %v", auths[0].Metadata["disable_cooling"])
	}
}

func TestConfigSynthesizer_XAIKeys(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			XAIKey: []config.XAIKey{{
				APIKey:         "xai-key-123",
				Prefix:         "grok",
				BaseURL:        "https://api.x.ai/v1",
				ProxyURL:       "http://proxy.local",
				Websockets:     true,
				AlphaSearch:    true,
				DisableCooling: boolPointer(true),
				Headers:        map[string]string{"X-Custom": "value"},
				Models:         []config.XAIModel{{Name: "grok-4.5", Alias: "grok-latest"}},
			}},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, errSynthesize := synth.Synthesize(ctx)
	if errSynthesize != nil {
		t.Fatalf("Synthesize() error = %v", errSynthesize)
	}
	if len(auths) != 1 {
		t.Fatalf("auth count = %d, want 1", len(auths))
	}
	auth := auths[0]
	if auth.Provider != "xai" {
		t.Fatalf("provider = %q, want xai", auth.Provider)
	}
	if auth.Label != "xai-apikey" {
		t.Fatalf("label = %q, want xai-apikey", auth.Label)
	}
	if auth.Attributes["websockets"] != "true" {
		t.Fatalf("websockets = %q, want true", auth.Attributes["websockets"])
	}
	if _, exists := auth.Attributes[coreauth.AttributeCodexAlphaSearch]; exists {
		t.Fatal("xAI auth unexpectedly contains codex_alpha_search")
	}
	if auth.Attributes["base_url"] != "https://api.x.ai/v1" {
		t.Fatalf("base_url = %q, want https://api.x.ai/v1", auth.Attributes["base_url"])
	}
	if auth.Attributes["header:X-Custom"] != "value" {
		t.Fatalf("custom header = %q, want value", auth.Attributes["header:X-Custom"])
	}
	if auth.Attributes["models_hash"] == "" {
		t.Fatal("models_hash is empty")
	}
	if auth.ProxyURL != "http://proxy.local" {
		t.Fatalf("proxy URL = %q, want http://proxy.local", auth.ProxyURL)
	}
	if disabled, ok := auth.Metadata["disable_cooling"].(bool); !ok || !disabled {
		t.Fatalf("disable_cooling = %#v, want true", auth.Metadata["disable_cooling"])
	}
}
func TestConfigSynthesizer_XAIKeys_AllowsEmptyAPIKeyWithBaseURL(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			XAIKey: []config.XAIKey{
				{
					APIKey:  "",
					BaseURL: "https://custom-xai.example.com",
					Headers: map[string]string{"Custom-Auth": "secret"},
				},
				{
					APIKey:  "   ",
					BaseURL: "https://custom-xai-2.example.com",
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 2 {
		t.Fatalf("expected 2 auths for empty API keys with base URL, got %d", len(auths))
	}
	if auths[0].Attributes["base_url"] != "https://custom-xai.example.com" {
		t.Fatalf("expected base_url=https://custom-xai.example.com, got %s", auths[0].Attributes["base_url"])
	}
	if auths[0].Attributes["header:Custom-Auth"] != "secret" {
		t.Fatalf("expected header:Custom-Auth=secret, got %s", auths[0].Attributes["header:Custom-Auth"])
	}
	if auths[0].Attributes["auth_kind"] != "apikey" {
		t.Fatalf("expected auth_kind=apikey, got %s", auths[0].Attributes["auth_kind"])
	}
	if _, exists := auths[0].Attributes["api_key"]; exists {
		t.Fatalf("expected no api_key attribute for empty key, got %s", auths[0].Attributes["api_key"])
	}
}

func TestConfigSynthesizer_ClaudeKeys_AllowsEmptyAPIKeyWithBaseURL(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			ClaudeKey: []config.ClaudeKey{
				{
					APIKey:  "",
					BaseURL: "https://custom-claude.example.com",
					Headers: map[string]string{"Custom-Auth": "secret"},
				},
				{
					APIKey:  "   ",
					BaseURL: "https://custom-claude-2.example.com",
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 2 {
		t.Fatalf("expected 2 auths for empty API keys with base URL, got %d", len(auths))
	}
	if auths[0].Attributes["base_url"] != "https://custom-claude.example.com" {
		t.Fatalf("expected base_url=https://custom-claude.example.com, got %s", auths[0].Attributes["base_url"])
	}
	if auths[0].Attributes["header:Custom-Auth"] != "secret" {
		t.Fatalf("expected header:Custom-Auth=secret, got %s", auths[0].Attributes["header:Custom-Auth"])
	}
	if auths[0].Attributes["auth_kind"] != "apikey" {
		t.Fatalf("expected auth_kind=apikey, got %s", auths[0].Attributes["auth_kind"])
	}
	if _, exists := auths[0].Attributes["api_key"]; exists {
		t.Fatalf("expected no api_key attribute for empty key, got %s", auths[0].Attributes["api_key"])
	}
}

func TestConfigSynthesizer_CodexKeys_SkipsEmptyAndHeaders(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			CodexKey: []config.CodexKey{
				{APIKey: ""},   // empty key without base URL, should be skipped
				{APIKey: "  "}, // whitespace key without base URL, should be skipped
				{APIKey: "valid-key", Headers: map[string]string{"Authorization": "Bearer xyz"}},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth (empty keys skipped), got %d", len(auths))
	}
	if auths[0].Attributes["header:Authorization"] != "Bearer xyz" {
		t.Errorf("expected header:Authorization=Bearer xyz, got %s", auths[0].Attributes["header:Authorization"])
	}
	if _, exists := auths[0].Attributes[coreauth.AttributeCodexAlphaSearch]; exists {
		t.Fatal("default alpha-search=false unexpectedly generated codex_alpha_search")
	}
}

func TestConfigSynthesizer_CodexKeys_AllowsEmptyAPIKeyWithBaseURL(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			CodexKey: []config.CodexKey{
				{
					APIKey:  "",
					BaseURL: "https://custom-codex.example.com",
					Headers: map[string]string{"Custom-Auth": "secret"},
				},
				{
					APIKey:  "   ",
					BaseURL: "https://custom-codex-2.example.com",
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 2 {
		t.Fatalf("expected 2 auths for empty API keys with base URL, got %d", len(auths))
	}
	if auths[0].Attributes["base_url"] != "https://custom-codex.example.com" {
		t.Fatalf("expected base_url=https://custom-codex.example.com, got %s", auths[0].Attributes["base_url"])
	}
	if auths[0].Attributes["header:Custom-Auth"] != "secret" {
		t.Fatalf("expected header:Custom-Auth=secret, got %s", auths[0].Attributes["header:Custom-Auth"])
	}
	if auths[0].Attributes["auth_kind"] != "apikey" {
		t.Fatalf("expected auth_kind=apikey, got %s", auths[0].Attributes["auth_kind"])
	}
	if _, exists := auths[0].Attributes["api_key"]; exists {
		t.Fatalf("expected no api_key attribute for empty key, got %s", auths[0].Attributes["api_key"])
	}
}

func TestConfigSynthesizer_OpenAICompat(t *testing.T) {
	tests := []struct {
		name    string
		compat  []config.OpenAICompatibility
		wantLen int
	}{
		{
			name: "with APIKeyEntries",
			compat: []config.OpenAICompatibility{
				{
					Name:           "CustomProvider",
					BaseURL:        "https://custom.api.com",
					DisableCooling: boolPointer(true),
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: "key-1"},
						{APIKey: "key-2"},
					},
				},
			},
			wantLen: 2,
		},
		{
			name: "empty APIKeyEntries included (legacy)",
			compat: []config.OpenAICompatibility{
				{
					Name:    "EmptyKeys",
					BaseURL: "https://empty.api.com",
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: ""},
						{APIKey: "   "},
					},
				},
			},
			wantLen: 2,
		},
		{
			name: "without APIKeyEntries (fallback)",
			compat: []config.OpenAICompatibility{
				{
					Name:    "NoKeyProvider",
					BaseURL: "https://no-key.api.com",
				},
			},
			wantLen: 1,
		},
		{
			name: "empty name defaults",
			compat: []config.OpenAICompatibility{
				{
					Name:    "",
					BaseURL: "https://default.api.com",
				},
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synth := NewConfigSynthesizer()
			ctx := &SynthesisContext{
				Config: &config.Config{
					OpenAICompatibility: tt.compat,
				},
				Now:         time.Now(),
				IDGenerator: NewStableIDGenerator(),
			}

			auths, err := synth.Synthesize(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(auths) != tt.wantLen {
				t.Fatalf("expected %d auths, got %d", tt.wantLen, len(auths))
			}
			if tt.name == "with APIKeyEntries" {
				for i := range auths {
					if v, ok := auths[i].Metadata["disable_cooling"].(bool); !ok || !v {
						t.Fatalf("expected auth[%d].disable_cooling=true, got %v", i, auths[i].Metadata["disable_cooling"])
					}
				}
			}
		})
	}
}

func TestConfigSynthesizer_OpenAICompat_UsesNamespacedProviderKey(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			OpenAICompatibility: []config.OpenAICompatibility{
				{
					Name:    "kimi",
					BaseURL: "https://kimi-compatible.example.com/v1",
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: "test-key"},
					},
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	auth := auths[0]
	if auth.Provider != "openai-compatible-kimi" {
		t.Fatalf("provider = %q, want openai-compatible-kimi", auth.Provider)
	}
	if auth.Attributes["provider_key"] != "openai-compatible-kimi" {
		t.Fatalf("provider_key = %q, want openai-compatible-kimi", auth.Attributes["provider_key"])
	}
	if auth.Attributes["compat_name"] != "kimi" {
		t.Fatalf("compat_name = %q, want kimi", auth.Attributes["compat_name"])
	}
	if auth.Attributes["config_index"] != "0" {
		t.Fatalf("config_index = %q, want 0", auth.Attributes["config_index"])
	}
}

func TestConfigSynthesizer_OpenAICompat_WithModelsHash(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			OpenAICompatibility: []config.OpenAICompatibility{
				{
					Name:    "TestProvider",
					BaseURL: "https://test.api.com",
					Models: []config.OpenAICompatibilityModel{
						{Name: "model-a"},
						{Name: "model-b"},
					},
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: "key-with-models"},
					},
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	if _, ok := auths[0].Attributes["models_hash"]; !ok {
		t.Error("expected models_hash in attributes")
	}
	if auths[0].Attributes["api_key"] != "key-with-models" {
		t.Errorf("expected api_key key-with-models, got %s", auths[0].Attributes["api_key"])
	}
}

func TestConfigSynthesizer_OpenAICompat_FallbackWithModels(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			OpenAICompatibility: []config.OpenAICompatibility{
				{
					Name:    "NoKeyWithModels",
					BaseURL: "https://nokey.api.com",
					Models: []config.OpenAICompatibilityModel{
						{Name: "model-x"},
					},
					Headers: map[string]string{"X-API": "header-value"},
					// No APIKeyEntries - should use fallback path
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	if _, ok := auths[0].Attributes["models_hash"]; !ok {
		t.Error("expected models_hash in fallback path")
	}
	if auths[0].Attributes["header:X-API"] != "header-value" {
		t.Errorf("expected header:X-API=header-value, got %s", auths[0].Attributes["header:X-API"])
	}
}

func TestConfigSynthesizer_IDStability(t *testing.T) {
	cfg := &config.Config{
		ClaudeKey: []config.ClaudeKey{
			{APIKey: "stable-key", Prefix: "test"},
		},
	}

	// Generate IDs twice with fresh generators
	synth1 := NewConfigSynthesizer()
	ctx1 := &SynthesisContext{
		Config:      cfg,
		Now:         time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		IDGenerator: NewStableIDGenerator(),
	}
	auths1, _ := synth1.Synthesize(ctx1)

	synth2 := NewConfigSynthesizer()
	ctx2 := &SynthesisContext{
		Config:      cfg,
		Now:         time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		IDGenerator: NewStableIDGenerator(),
	}
	auths2, _ := synth2.Synthesize(ctx2)

	if auths1[0].ID != auths2[0].ID {
		t.Errorf("same config should produce same ID: got %q and %q", auths1[0].ID, auths2[0].ID)
	}
}

func TestConfigSynthesizer_AllProviders(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			ClaudeKey: []config.ClaudeKey{
				{APIKey: "claude-key"},
			},
			CodexKey: []config.CodexKey{
				{APIKey: "codex-key"},
			},
			XAIKey: []config.XAIKey{
				{APIKey: "xai-key"},
			},
			OpenAICompatibility: []config.OpenAICompatibility{
				{Name: "compat", BaseURL: "https://compat.api"},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 4 {
		t.Fatalf("expected 4 auths, got %d", len(auths))
	}

	providers := make(map[string]bool)
	for _, a := range auths {
		providers[a.Provider] = true
	}

	expected := []string{"claude", "codex", "xai", "openai-compatible-compat"}
	for _, p := range expected {
		if !providers[p] {
			t.Errorf("expected provider %s not found", p)
		}
	}
}

func TestConfigSynthesizer_RequestRetry(t *testing.T) {
	zero := 0
	positive := 2
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			ClaudeKey: []config.ClaudeKey{
				{APIKey: "claude-positive", RequestRetry: &positive},
			},
			CodexKey: []config.CodexKey{
				{APIKey: "codex-zero", RequestRetry: &zero},
			},
			XAIKey: []config.XAIKey{
				{APIKey: "xai-positive", RequestRetry: &positive},
			},
			OpenAICompatibility: []config.OpenAICompatibility{
				{
					Name:         "compat",
					BaseURL:      "https://compat.api",
					RequestRetry: &zero,
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: "compat-key"},
					},
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, errSynthesize := synth.Synthesize(ctx)
	if errSynthesize != nil {
		t.Fatalf("Synthesize() error = %v", errSynthesize)
	}

	want := map[string]any{
		"claude-positive": 2,
		"codex-zero":      0,
		"xai-positive":    2,
		"compat-key":      0,
	}
	got := make(map[string]any, len(auths))
	for _, auth := range auths {
		key := auth.Attributes["api_key"]
		if auth.Metadata == nil {
			got[key] = nil
			continue
		}
		if value, exists := auth.Metadata["request_retry"]; exists {
			got[key] = value
			continue
		}
		got[key] = nil
	}
	for key, expected := range want {
		actual, exists := got[key]
		if !exists {
			t.Fatalf("missing synthesized auth for %s", key)
		}
		if actual != expected {
			t.Fatalf("%s request_retry = %v, want %v", key, actual, expected)
		}
	}
}

func TestConfigSynthesizer_RequestScopedErrors(t *testing.T) {
	synth := NewConfigSynthesizer()
	rules := []config.RequestScopedErrorRule{
		{
			Status: 400,
			Match:  []string{"maximum_context_length"},
			Action: "stop",
		},
	}

	ctx := &SynthesisContext{
		Config: &config.Config{
			ClaudeKey: []config.ClaudeKey{
				{APIKey: "claude-key", RequestScopedErrors: rules},
			},
			CodexKey: []config.CodexKey{
				{APIKey: "codex-key", BaseURL: "https://codex.api", RequestScopedErrors: rules},
			},
			XAIKey: []config.XAIKey{
				{APIKey: "xai-key", BaseURL: "https://xai.api", RequestScopedErrors: rules},
			},
			OpenAICompatibility: []config.OpenAICompatibility{
				{
					Name:                "compat",
					BaseURL:             "https://compat.api",
					RequestScopedErrors: rules,
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: "compat-key"},
					},
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, errSynthesize := synth.Synthesize(ctx)
	if errSynthesize != nil {
		t.Fatalf("Synthesize() error = %v", errSynthesize)
	}

	for _, auth := range auths {
		if auth.Metadata == nil {
			t.Fatalf("auth %s has nil metadata", auth.ID)
		}
		val, exists := auth.Metadata["request_scoped_errors"]
		if !exists {
			t.Fatalf("auth %s missing request_scoped_errors in metadata", auth.ID)
		}
		extracted, ok := val.([]config.RequestScopedErrorRule)
		if !ok || len(extracted) != 1 || extracted[0].Action != "stop" {
			t.Fatalf("auth %s unexpected request_scoped_errors: %#v", auth.ID, val)
		}
	}
}

// TestConfigSynthesizer_ConnectionPoolOverride pins the per-credential pool opt-in:
// the override declared on an API-key entry must reach the auth record so the
// generic proxy client can pool just that credential.
func TestConfigSynthesizer_ConnectionPoolOverride(t *testing.T) {
	enabled := true
	maxIdle := 4
	cfg := &config.Config{
		CodexKey: []config.CodexKey{{
			APIKey:   "ollama-key",
			BaseURL:  "https://ollama.com/v1",
			ProxyURL: "direct",
			ConnectionPool: &config.ProxyConnectionPoolConfig{
				Enabled:             &enabled,
				IdleConnTimeout:     "300s",
				MaxIdleConnsPerHost: &maxIdle,
			},
		}},
		ClaudeKey: []config.ClaudeKey{{APIKey: "claude-key", BaseURL: "https://api.anthropic.com"}},
	}
	auths, err := NewConfigSynthesizer().Synthesize(&SynthesisContext{
		Config:      cfg,
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var codexAuth, claudeAuth *coreauth.Auth
	for _, a := range auths {
		switch {
		case a.Provider == "codex":
			codexAuth = a
		case a.Provider == "claude":
			claudeAuth = a
		}
	}
	if codexAuth == nil || claudeAuth == nil {
		t.Fatalf("missing synthesised auths: codex=%v claude=%v", codexAuth, claudeAuth)
	}
	pool := codexAuth.ProxyConnectionPool
	if pool == nil {
		t.Fatal("codex override did not reach the auth record")
	}
	if pool.Enabled == nil || !*pool.Enabled || pool.IdleConnTimeout != "300s" || pool.MaxIdleConnsPerHost == nil || *pool.MaxIdleConnsPerHost != 4 {
		t.Fatalf("unexpected override: %+v", pool)
	}
	if claudeAuth.ProxyConnectionPool != nil {
		t.Fatal("entry without a connection-pool block must stay unpooled")
	}
	// The override must be copied, never aliased to live config values.
	*pool.Enabled = false
	if !enabled {
		t.Fatal("auth override aliases the config pointer")
	}
}

// TestConfigSynthesizer_OpenAICompatConnectionPoolMerge pins field-level inheritance
// for OpenAI-compatible keys: a per-key block only replaces the fields it sets.
func TestConfigSynthesizer_OpenAICompatConnectionPoolMerge(t *testing.T) {
	groupEnabled := true
	groupMax := 2
	cfg := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name:    "compat",
			BaseURL: "https://compat.example.com",
			ConnectionPool: &config.ProxyConnectionPoolConfig{
				Enabled:             &groupEnabled,
				IdleConnTimeout:     "300s",
				MaxIdleConnsPerHost: &groupMax,
			},
			APIKeyEntries: []config.OpenAICompatibilityAPIKey{
				{APIKey: "inherits"},
				{APIKey: "partial", ConnectionPool: &config.ProxyConnectionPoolConfig{IdleConnTimeout: "60s"}},
			},
		}},
	}
	auths, err := NewConfigSynthesizer().Synthesize(&SynthesisContext{
		Config:      cfg,
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	byKey := map[string]*coreauth.ProxyConnectionPoolOverride{}
	for _, a := range auths {
		byKey[a.Attributes["api_key"]] = a.ProxyConnectionPool
	}
	inherits := byKey["inherits"]
	if inherits == nil || inherits.Enabled == nil || !*inherits.Enabled || inherits.IdleConnTimeout != "300s" {
		t.Fatalf("key without an override must inherit the group block: %+v", inherits)
	}
	if inherits.MaxIdleConnsPerHost == nil || *inherits.MaxIdleConnsPerHost != 2 {
		t.Fatalf("inheriting key lost max-idle: %+v", inherits)
	}
	partial := byKey["partial"]
	if partial == nil || partial.Enabled == nil || !*partial.Enabled {
		t.Fatalf("partial override must keep the group's enabled value: %+v", partial)
	}
	if partial.IdleConnTimeout != "60s" {
		t.Fatalf("partial override idle = %q, want 60s", partial.IdleConnTimeout)
	}
	if partial.MaxIdleConnsPerHost == nil || *partial.MaxIdleConnsPerHost != 2 {
		t.Fatalf("partial override must inherit max-idle-conns-per-host: %+v", partial)
	}
}

// TestConfigSynthesizer_EmptyConnectionPoolBlockOptsIn pins that writing the block
// without fields requests the pool defaults. Dropping it during synthesis disabled
// pooling for `connection-pool: {}` and for keys inheriting an empty group block.
func TestConfigSynthesizer_EmptyConnectionPoolBlockOptsIn(t *testing.T) {
	cfg := &config.Config{CodexKey: []config.CodexKey{{
		APIKey:         "empty-block",
		BaseURL:        "https://ollama.com/v1",
		ProxyURL:       "direct",
		ConnectionPool: &config.ProxyConnectionPoolConfig{},
	}}}
	auths, err := NewConfigSynthesizer().Synthesize(&SynthesisContext{
		Config:      cfg,
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("auths = %d, want 1", len(auths))
	}
	if auths[0].ProxyConnectionPool == nil {
		t.Fatal("an empty connection-pool block is still an opt-in and must reach the auth record")
	}
	if enabled := auths[0].ProxyConnectionPool.Enabled; enabled != nil {
		t.Fatalf("empty block must leave enabled unset (defaults to on), got %v", *enabled)
	}

	// An OpenAI-compatible provider with no API keys still synthesizes one auth
	// record (e.g. a local Ollama/vLLM daemon) and must inherit the group pool block.
	keyless := &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
		Name:           "local-ollama",
		BaseURL:        "http://127.0.0.1:11434/v1",
		ConnectionPool: &config.ProxyConnectionPoolConfig{},
	}}}
	keylessAuths, err := NewConfigSynthesizer().Synthesize(&SynthesisContext{
		Config:      keyless,
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keylessAuths) != 1 || keylessAuths[0].ProxyConnectionPool == nil {
		t.Fatalf("keyless OpenAI-compatible entry lost its group connection-pool block: %+v", keylessAuths)
	}
}
