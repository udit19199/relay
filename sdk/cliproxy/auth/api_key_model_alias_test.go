package auth

import (
	"context"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestLookupAPIKeyUpstreamModel(t *testing.T) {
	cfg := &internalconfig.Config{
		ClaudeKey: []internalconfig.ClaudeKey{
			{
				APIKey:  "k",
				BaseURL: "https://example.com",
				Models: []internalconfig.ClaudeModel{
					{Name: "claude-sonnet-4-5", Alias: "csp"},
					{Name: "claude-haiku-4-5(low)", Alias: "chf"},
				},
			},
		},
	}

	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(cfg)

	ctx := context.Background()
	_, _ = mgr.Register(ctx, &Auth{ID: "a1", Provider: "claude", Attributes: map[string]string{"api_key": "k", "base_url": "https://example.com"}})

	tests := []struct {
		name   string
		authID string
		input  string
		want   string
	}{
		// Fast path + suffix preservation
		{"alias with suffix", "a1", "csp(8192)", "claude-sonnet-4-5(8192)"},
		{"alias without suffix", "a1", "csp", "claude-sonnet-4-5"},

		// Config suffix takes priority
		{"config suffix priority", "a1", "chf(high)", "claude-haiku-4-5(low)"},
		{"config suffix no user suffix", "a1", "chf", "claude-haiku-4-5(low)"},

		// Case insensitive
		{"uppercase alias", "a1", "CSP", "claude-sonnet-4-5"},
		{"mixed case with suffix", "a1", "Csp(4096)", "claude-sonnet-4-5(4096)"},

		// Direct name lookup
		{"upstream name direct", "a1", "claude-sonnet-4-5", "claude-sonnet-4-5"},
		{"upstream name with suffix", "a1", "claude-sonnet-4-5(8192)", "claude-sonnet-4-5(8192)"},

		// Cache miss scenarios
		{"non-existent auth", "non-existent", "csp", ""},
		{"unknown alias", "a1", "unknown-alias", ""},
		{"empty auth ID", "", "csp", ""},
		{"empty model", "a1", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := mgr.lookupAPIKeyUpstreamModel(tt.authID, tt.input)
			if resolved != tt.want {
				t.Errorf("lookupAPIKeyUpstreamModel(%q, %q) = %q, want %q", tt.authID, tt.input, resolved, tt.want)
			}
		})
	}
}

func TestAPIKeyModelAlias_ConfigHotReload(t *testing.T) {
	cfg := &internalconfig.Config{
		ClaudeKey: []internalconfig.ClaudeKey{
			{
				APIKey: "k",
				Models: []internalconfig.ClaudeModel{{Name: "claude-sonnet-4-5", Alias: "csp"}},
			},
		},
	}

	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(cfg)

	ctx := context.Background()
	_, _ = mgr.Register(ctx, &Auth{ID: "a1", Provider: "claude", Attributes: map[string]string{"api_key": "k"}})

	// Initial alias
	if resolved := mgr.lookupAPIKeyUpstreamModel("a1", "csp"); resolved != "claude-sonnet-4-5" {
		t.Fatalf("before reload: got %q, want %q", resolved, "claude-sonnet-4-5")
	}

	// Hot reload with new alias
	mgr.SetConfig(&internalconfig.Config{
		ClaudeKey: []internalconfig.ClaudeKey{
			{
				APIKey: "k",
				Models: []internalconfig.ClaudeModel{{Name: "claude-haiku-4-5", Alias: "csp"}},
			},
		},
	})

	// New alias should take effect
	if resolved := mgr.lookupAPIKeyUpstreamModel("a1", "csp"); resolved != "claude-haiku-4-5" {
		t.Fatalf("after reload: got %q, want %q", resolved, "claude-haiku-4-5")
	}
}

func TestAPIKeyModelAlias_MultipleProviders(t *testing.T) {
	cfg := &internalconfig.Config{
		ClaudeKey: []internalconfig.ClaudeKey{{APIKey: "claude-key", Models: []internalconfig.ClaudeModel{{Name: "claude-sonnet-4", Alias: "cs4"}}}},
		CodexKey:  []internalconfig.CodexKey{{APIKey: "codex-key", Models: []internalconfig.CodexModel{{Name: "o3", Alias: "o"}}}},
		XAIKey:    []internalconfig.XAIKey{{APIKey: "xai-key", Models: []internalconfig.XAIModel{{Name: "grok-4.5", Alias: "grok-latest"}}}},
	}

	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(cfg)

	ctx := context.Background()
	_, _ = mgr.Register(ctx, &Auth{ID: "claude-auth", Provider: "claude", Attributes: map[string]string{"api_key": "claude-key"}})
	_, _ = mgr.Register(ctx, &Auth{ID: "claude-auth", Provider: "claude", Attributes: map[string]string{"api_key": "claude-key"}})
	_, _ = mgr.Register(ctx, &Auth{ID: "codex-auth", Provider: "codex", Attributes: map[string]string{"api_key": "codex-key"}})
	_, _ = mgr.Register(ctx, &Auth{ID: "xai-auth", Provider: "xai", Attributes: map[string]string{"api_key": "xai-key"}})

	tests := []struct {
		authID, input, want string
	}{
		{"claude-auth", "cs4", "claude-sonnet-4"},
		{"codex-auth", "o", "o3"},
		{"xai-auth", "grok-latest", "grok-4.5"},
	}

	for _, tt := range tests {
		if resolved := mgr.lookupAPIKeyUpstreamModel(tt.authID, tt.input); resolved != tt.want {
			t.Errorf("lookupAPIKeyUpstreamModel(%q, %q) = %q, want %q", tt.authID, tt.input, resolved, tt.want)
		}
	}
}

func TestApplyAPIKeyModelAlias(t *testing.T) {
	cfg := &internalconfig.Config{
		ClaudeKey: []internalconfig.ClaudeKey{
			{APIKey: "k", Models: []internalconfig.ClaudeModel{{Name: "claude-sonnet-4-5", Alias: "csp"}}},
		},
	}

	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(cfg)

	ctx := context.Background()
	apiKeyAuth := &Auth{ID: "a1", Provider: "claude", Attributes: map[string]string{"api_key": "k"}}
	oauthAuth := &Auth{ID: "oauth-auth", Provider: "claude", Attributes: map[string]string{"auth_kind": "oauth"}}
	_, _ = mgr.Register(ctx, apiKeyAuth)

	tests := []struct {
		name       string
		auth       *Auth
		inputModel string
		wantModel  string
	}{
		{
			name:       "api_key auth with alias",
			auth:       apiKeyAuth,
			inputModel: "csp(8192)",
			wantModel:  "claude-sonnet-4-5(8192)",
		},
		{
			name:       "oauth auth passthrough",
			auth:       oauthAuth,
			inputModel: "some-model",
			wantModel:  "some-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolvedModel := mgr.applyAPIKeyModelAlias(tt.auth, tt.inputModel)

			if resolvedModel != tt.wantModel {
				t.Errorf("model = %q, want %q", resolvedModel, tt.wantModel)
			}
		})
	}
}

func TestResolveAPIKeyModelAliasWithResult_ForceMapping(t *testing.T) {
	cfg := &internalconfig.Config{
		ClaudeKey: []internalconfig.ClaudeKey{{
			APIKey: "claude-key",
			Models: []internalconfig.ClaudeModel{{
				Name:         "glm-5.2",
				Alias:        "claude-sonnet-latest",
				ForceMapping: true,
			}},
		}},
	}

	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(cfg)

	ctx := context.Background()
	auth := &Auth{ID: "claude-auth", Provider: "claude", Attributes: map[string]string{"api_key": "claude-key"}}
	if _, err := mgr.Register(ctx, auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	result := mgr.resolveAPIKeyModelAliasWithResult(auth, "claude-sonnet-latest")
	if result.UpstreamModel != "glm-5.2" || !result.ForceMapping || result.OriginalAlias != "claude-sonnet-latest" {
		t.Fatalf("resolveAPIKeyModelAliasWithResult() = %+v, want upstream glm-5.2 with force mapping", result)
	}

	noRewrite := mgr.resolveAPIKeyModelAliasWithResult(auth, "glm-5.2")
	if noRewrite.UpstreamModel != "glm-5.2" || noRewrite.ForceMapping || noRewrite.OriginalAlias != "" {
		t.Fatalf("resolveAPIKeyModelAliasWithResult() direct upstream = %+v, want passthrough without rewrite", noRewrite)
	}
}

func TestResolveAPIKeyModelAliasWithResult_SameBasePreservesSuffix(t *testing.T) {
	cfg := &internalconfig.Config{
		ClaudeKey: []internalconfig.ClaudeKey{{
			APIKey: "k",
			Models: []internalconfig.ClaudeModel{{
				Name:         "claude-sonnet-4",
				Alias:        "claude-sonnet-4(8192)",
				ForceMapping: true,
			}},
		}},
	}

	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(cfg)

	ctx := context.Background()
	auth := &Auth{ID: "claude-auth", Provider: "claude", Attributes: map[string]string{"api_key": "k"}}
	if _, err := mgr.Register(ctx, auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	result := mgr.resolveAPIKeyModelAliasWithResult(auth, "claude-sonnet-4(8192)")
	if result.UpstreamModel != "claude-sonnet-4(8192)" || !result.ForceMapping || result.OriginalAlias != "claude-sonnet-4(8192)" {
		t.Fatalf("resolveAPIKeyModelAliasWithResult() = %+v, want same-base suffix preserved", result)
	}
}

func TestResolveAPIKeyModelAliasWithResult_ForceMappingUsesConfigAliasNotRequestSuffix(t *testing.T) {
	cfg := &internalconfig.Config{
		CodexKey: []internalconfig.CodexKey{{
			APIKey: "codex-key",
			Models: []internalconfig.CodexModel{{
				Name:         "gpt-5.5",
				Alias:        "claude-sonnet-4-5",
				ForceMapping: true,
			}},
		}},
	}

	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(cfg)

	ctx := context.Background()
	auth := &Auth{ID: "codex-auth", Provider: "codex", Attributes: map[string]string{"api_key": "codex-key"}}
	if _, err := mgr.Register(ctx, auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	result := mgr.resolveAPIKeyModelAliasWithResult(auth, "claude-sonnet-4-5(high)")
	if result.UpstreamModel != "gpt-5.5(high)" {
		t.Fatalf("upstream = %q want gpt-5.5(high)", result.UpstreamModel)
	}
	if result.OriginalAlias != "claude-sonnet-4-5" {
		t.Fatalf("OriginalAlias = %q want claude-sonnet-4-5", result.OriginalAlias)
	}
}

func TestLookupAPIKeyUpstreamModel_MetaKey(t *testing.T) {
	cfg := &internalconfig.Config{
		MetaKey: []internalconfig.MetaKey{
			{
				APIKey:  "meta-key",
				BaseURL: "https://api.meta.ai/v1",
				Models: []internalconfig.CodexModel{
					{Name: "muse-spark-1.3", Alias: "muse-latest"},
				},
			},
		},
	}

	mgr := NewManager(nil, nil, nil)
	mgr.SetConfig(cfg)

	ctx := context.Background()
	auth := &Auth{
		ID:       "meta-auth-1",
		Provider: "meta",
		Attributes: map[string]string{
			"api_key":   "meta-key",
			"base_url":  "https://api.meta.ai/v1",
			"auth_kind": "apikey",
		},
	}
	if _, err := mgr.Register(ctx, auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	// 1. Fast path: lookup per-auth mapping table compiled during register.
	resolved := mgr.lookupAPIKeyUpstreamModel("meta-auth-1", "muse-latest")
	if resolved != "muse-spark-1.3" {
		t.Fatalf("lookupAPIKeyUpstreamModel() = %q, want muse-spark-1.3", resolved)
	}

	// 2. Slow path: directly call mgr.applyAPIKeyModelAliasWithRouting with an empty alias table to exercise config resolution fallback.
	slowRouting := &apiKeyModelRoutingSnapshot{
		config:  cfg,
		aliases: make(apiKeyModelAliasTable),
	}
	slowResolved := mgr.applyAPIKeyModelAliasWithRouting(slowRouting, auth, "muse-latest")
	if slowResolved != "muse-spark-1.3" {
		t.Fatalf("applyAPIKeyModelAliasWithRouting(slow) = %q, want muse-spark-1.3", slowResolved)
	}

	// 3. Model alias result with force mapping / alias metadata
	aliasResult := mgr.resolveAPIKeyModelAliasWithResult(auth, "muse-latest")
	if aliasResult.UpstreamModel != "muse-spark-1.3" {
		t.Fatalf("resolveAPIKeyModelAliasWithResult() upstream = %q, want muse-spark-1.3", aliasResult.UpstreamModel)
	}

	// 4. Configured alias entries helper
	entries := configuredModelAliasEntries(cfg, auth)
	found := false
	for _, e := range entries {
		if e.GetAlias() == "muse-latest" && e.GetName() == "muse-spark-1.3" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("configuredModelAliasEntries did not contain muse-latest -> muse-spark-1.3: %+v", entries)
	}
}
