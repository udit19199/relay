package config

import (
	"reflect"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

func TestCloneForRuntimeNil(t *testing.T) {
	var cfg *Config
	if got := cfg.CloneForRuntime(); got != nil {
		t.Fatalf("CloneForRuntime() = %#v, want nil", got)
	}
}

func TestCloneForRuntimeDeepCopiesConfig(t *testing.T) {
	cfg := sampleCloneRuntimeConfig()

	clone := cfg.CloneForRuntime()
	if clone == nil {
		t.Fatal("CloneForRuntime() = nil")
	}
	if clone == cfg {
		t.Fatal("CloneForRuntime() returned original pointer")
	}

	mutateOriginalConfig(cfg)

	if clone.APIKeys[0] != "client-key" {
		t.Fatalf("clone.APIKeys[0] = %q, want client-key", clone.APIKeys[0])
	}
	if clone.OAuthExcludedModels["codex"][0] != "hidden-model" {
		t.Fatalf("clone.OAuthExcludedModels[codex][0] = %q, want hidden-model", clone.OAuthExcludedModels["codex"][0])
	}
	if clone.OAuthModelAlias["codex"][0].Alias != "client-model" {
		t.Fatalf("clone.OAuthModelAlias[codex][0].Alias = %q, want client-model", clone.OAuthModelAlias["codex"][0].Alias)
	}

	if clone.OpenAICompatibility[0].Models[0].Thinking.Levels[0] != "low" {
		t.Fatalf("clone thinking level = %q, want low", clone.OpenAICompatibility[0].Models[0].Thinking.Levels[0])
	}
	if got := clone.Payload.Default[0].Params["object"].(map[string]any)["key"]; got != "value" {
		t.Fatalf("clone payload object key = %#v, want value", got)
	}

	clone.APIKeys[0] = "clone-client-key"
	clone.OAuthExcludedModels["codex"][0] = "clone-hidden-model"
	clone.OAuthModelAlias["codex"][0].Alias = "clone-client-model"
	clone.OpenAICompatibility[0].Models[0].Thinking.Levels[0] = "clone-low"
	clone.Payload.Default[0].Params["object"].(map[string]any)["key"] = "clone-value"

	if cfg.APIKeys[0] != "mutated-client-key" {
		t.Fatalf("cfg.APIKeys[0] = %q, want mutated-client-key", cfg.APIKeys[0])
	}
	if cfg.OAuthExcludedModels["codex"][0] != "mutated-hidden-model" {
		t.Fatalf("cfg.OAuthExcludedModels[codex][0] = %q, want mutated-hidden-model", cfg.OAuthExcludedModels["codex"][0])
	}
	if cfg.OAuthModelAlias["codex"][0].Alias != "mutated-client-model" {
		t.Fatalf("cfg.OAuthModelAlias[codex][0].Alias = %q, want mutated-client-model", cfg.OAuthModelAlias["codex"][0].Alias)
	}

	if cfg.OpenAICompatibility[0].Models[0].Thinking.Levels[0] != "mutated-low" {
		t.Fatalf("cfg thinking level = %q, want mutated-low", cfg.OpenAICompatibility[0].Models[0].Thinking.Levels[0])
	}
	if got := cfg.Payload.Default[0].Params["object"].(map[string]any)["key"]; got != "mutated-value" {
		t.Fatalf("cfg payload object key = %#v, want mutated-value", got)
	}
}

func TestCloneForRuntimeDoesNotShareReferenceFields(t *testing.T) {
	cfg := sampleCloneRuntimeConfig()
	clone := cfg.CloneForRuntime()

	assertNoSharedRuntimeReferences(t, reflect.ValueOf(cfg), reflect.ValueOf(clone), "Config")
}

func sampleCloneRuntimeConfig() *Config {
	cacheUserID := true

	return &Config{
		SDKConfig: SDKConfig{
			APIKeys: []string{"client-key"},
			Streaming: StreamingConfig{
				KeepAliveSeconds:      3,
				BootstrapRetries:      2,
				SendKeepAliveBeforeFT: true,
			},
		},
		CodexKey: []CodexKey{{
			APIKey:         "codex-key",
			Models:         []CodexModel{{Name: "codex-upstream", Alias: "codex-client"}},
			Headers:        map[string]string{"X-Codex": "one"},
			ExcludedModels: []string{"codex-hidden-key"},
		}},
		ClaudeKey: []ClaudeKey{{
			APIKey:         "claude-key",
			Models:         []ClaudeModel{{Name: "claude-upstream", Alias: "claude-client"}},
			Headers:        map[string]string{"X-Claude": "one"},
			ExcludedModels: []string{"claude-hidden"},
			Cloak: &CloakConfig{
				SensitiveWords: []string{"secret"},
				CacheUserID:    &cacheUserID,
			},
		}},
		OpenAICompatibility: []OpenAICompatibility{{
			Name:          "compat",
			APIKeyEntries: []OpenAICompatibilityAPIKey{{APIKey: "compat-key", ProxyURL: "http://proxy.local"}},
			Models: []OpenAICompatibilityModel{{
				Name:     "compat-upstream",
				Alias:    "compat-client",
				Thinking: &registry.ThinkingSupport{Levels: []string{"low", "high"}},
			}},
			Headers: map[string]string{"X-Compat": "one"},
		}},
		OAuthExcludedModels: map[string][]string{
			"codex": {"hidden-model"},
		},
		OAuthModelAlias: map[string][]OAuthModelAlias{
			"codex": {{Name: "upstream-model", Alias: "client-model", Fork: true}},
		},
		Payload: PayloadConfig{
			Default: []PayloadRule{{
				Models: []PayloadModelRule{{
					Name:    "model-*",
					Headers: map[string]string{"X-Tier": "gold"},
					Match:   []map[string]any{{"tier": "gold"}},
					Exist:   []string{"$.messages"},
				}},
				Params: map[string]any{
					"object": map[string]any{"key": "value"},
					"array":  []any{"first", map[string]any{"nested": "value"}},
				},
			}},
			Filter: []PayloadFilterRule{{
				Models: []PayloadModelRule{{Name: "model-*"}},
				Params: []string{"$.secret"},
			}},
		},
	}
}

func mutateOriginalConfig(cfg *Config) {
	cfg.APIKeys[0] = "mutated-client-key"
	cfg.OAuthExcludedModels["codex"][0] = "mutated-hidden-model"
	cfg.OAuthModelAlias["codex"][0].Alias = "mutated-client-model"
	cfg.OpenAICompatibility[0].Models[0].Thinking.Levels[0] = "mutated-low"
	cfg.Payload.Default[0].Params["object"].(map[string]any)["key"] = "mutated-value"
}

func assertNoSharedRuntimeReferences(t *testing.T, original, clone reflect.Value, path string) {
	t.Helper()
	if !original.IsValid() || !clone.IsValid() {
		return
	}
	if original.Kind() == reflect.Interface {
		if original.IsNil() || clone.IsNil() {
			return
		}
		assertNoSharedRuntimeReferences(t, original.Elem(), clone.Elem(), path)
		return
	}
	if original.Kind() != clone.Kind() {
		t.Fatalf("%s kind mismatch: %s != %s", path, original.Kind(), clone.Kind())
	}

	switch original.Kind() {
	case reflect.Pointer:
		if original.IsNil() || clone.IsNil() {
			return
		}
		if original.Pointer() == clone.Pointer() {
			t.Fatalf("%s shares pointer %x", path, original.Pointer())
		}
		assertNoSharedRuntimeReferences(t, original.Elem(), clone.Elem(), path+"->"+original.Type().Elem().String())
	case reflect.Map:
		if original.IsNil() || clone.IsNil() {
			return
		}
		if original.Pointer() == clone.Pointer() {
			t.Fatalf("%s shares map pointer %x", path, original.Pointer())
		}
		iter := original.MapRange()
		for iter.Next() {
			key := iter.Key()
			assertNoSharedRuntimeReferences(t, iter.Value(), clone.MapIndex(key), path+"["+keyForPath(key)+"]")
		}
	case reflect.Slice:
		if original.IsNil() || clone.IsNil() {
			return
		}
		if original.Pointer() == clone.Pointer() {
			t.Fatalf("%s shares slice pointer %x", path, original.Pointer())
		}
		for i := 0; i < original.Len(); i++ {
			assertNoSharedRuntimeReferences(t, original.Index(i), clone.Index(i), path+"[]")
		}
	case reflect.Struct:
		for i := 0; i < original.NumField(); i++ {
			field := original.Type().Field(i)
			assertNoSharedRuntimeReferences(t, original.Field(i), clone.Field(i), path+"."+field.Name)
		}
	}
}

func keyForPath(key reflect.Value) string {
	if key.Kind() == reflect.String {
		return key.String()
	}
	return key.Type().String()
}

func TestParseConfigBytes_ProxyConnectionPool(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte("api-keys:\n  codex:\n    - name: pool-group\n      base-url: https://ollama.com/v1\n      connection-pool:\n        enabled: true\n        idle-conn-timeout: 15s\n        max-idle-conns-per-host: 4\n      keys:\n        - api-key: pool-key\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CodexKey) != 1 {
		t.Fatalf("codex entries = %d, want 1", len(cfg.CodexKey))
	}
	pool := cfg.CodexKey[0].ConnectionPool
	if pool == nil || pool.Enabled == nil || !*pool.Enabled || pool.IdleConnTimeout != "15s" || pool.MaxIdleConnsPerHost == nil || *pool.MaxIdleConnsPerHost != 4 {
		t.Fatalf("unexpected pool config: %+v", pool)
	}
	clone := cfg.CloneForRuntime()
	if clone.CodexKey[0].ConnectionPool == pool {
		t.Fatal("clone shares the pool block")
	}
	*clone.CodexKey[0].ConnectionPool.Enabled = false
	if !*cfg.CodexKey[0].ConnectionPool.Enabled {
		t.Fatal("clone shares enabled pointer")
	}
}
