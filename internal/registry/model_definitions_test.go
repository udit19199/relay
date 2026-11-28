package registry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexConfigurationUpdateCapability(t *testing.T) {
	// Counterexample: present in every tier without support_configuration_update.
	const incapableModelID = "gpt-5.6-luna"
	for _, tier := range []struct {
		name    string
		models  func() []*ModelInfo
		capable []string
	}{
		{name: "free", models: GetCodexFreeModels, capable: []string{"gpt-6-luna"}},
		{name: "team", models: GetCodexTeamModels, capable: []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna"}},
		{name: "plus", models: GetCodexPlusModels, capable: []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna"}},
		{name: "pro", models: GetCodexProModels, capable: []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna"}},
	} {
		t.Run(tier.name, func(t *testing.T) {
			byID := make(map[string]*ModelInfo)
			for _, info := range tier.models() {
				if info != nil {
					byID[info.ID] = info
				}
			}
			for _, id := range append([]string{incapableModelID}, tier.capable...) {
				info := byID[id]
				if info == nil {
					t.Errorf("%s is missing", id)
					continue
				}
				want := id != incapableModelID
				if info.SupportConfigurationUpdate != want {
					t.Errorf("%s configuration_update = %v, want %v", id, info.SupportConfigurationUpdate, want)
				}
				if cloned := cloneModelInfo(info); cloned.SupportConfigurationUpdate != want {
					t.Errorf("%s clone configuration_update = %v, want %v", id, cloned.SupportConfigurationUpdate, want)
				}
				raw, errMarshal := json.Marshal(info)
				if errMarshal != nil {
					t.Fatal(errMarshal)
				}
				if strings.Contains(string(raw), "support_configuration_update") || strings.Contains(string(raw), "SupportConfigurationUpdate") {
					t.Errorf("%s exposed configuration_update capability: %s", id, raw)
				}
			}
		})
	}

	for _, tt := range []struct {
		name string
		raw  string
		want bool
	}{
		{name: "true", raw: `{"id":"test","support_configuration_update":true}`, want: true},
		{name: "false", raw: `{"id":"test","support_configuration_update":false}`},
		{name: "absent", raw: `{"id":"test"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			info := ModelInfo{SupportConfigurationUpdate: true}
			if errUnmarshal := json.Unmarshal([]byte(tt.raw), &info); errUnmarshal != nil {
				t.Fatal(errUnmarshal)
			}
			if info.SupportConfigurationUpdate != tt.want {
				t.Errorf("capability = %v, want %v", info.SupportConfigurationUpdate, tt.want)
			}
		})
	}
}

func TestModelOverrideHeadersFromEmbeddedModels(t *testing.T) {
	const wantUA = "codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.154.0)"
	got := ModelOverrideHeaders("gpt-5.6-luna")
	if got == nil {
		t.Fatal("ModelOverrideHeaders(gpt-5.6-luna) = nil, want headers")
	}
	if got["user-agent"] != wantUA {
		t.Fatalf("user-agent = %q, want %q", got["user-agent"], wantUA)
	}
	if got := ModelOverrideHeaders("gpt-5.4"); got != nil {
		t.Fatalf("ModelOverrideHeaders(gpt-5.4) = %#v, want nil", got)
	}
}
func TestGetDevinModelsFallback(t *testing.T) {
	devinModels := GetDevinModels()
	if len(devinModels) == 0 {
		t.Fatal("GetDevinModels() returned empty list")
	}

	foundSWE2 := false
	foundFable := false
	foundGemini38 := false
	foundGrok46 := false
	foundDeepSeekV4Flash := false
	foundDeepSeekV41Flash := false
	for _, m := range devinModels {
		if m != nil && m.ID == "devin/swe-2" {
			foundSWE2 = true
			if m.Type != "devin" {
				t.Errorf("devin/swe-2 Type = %q, want devin", m.Type)
			}
		}
		if m != nil && m.ID == "devin/claude-fable-5-1" {
			foundFable = true
		}
		if m != nil && m.ID == "devin/gemini-3-8-flash" {
			foundGemini38 = true
			if m.OwnedBy != "google" {
				t.Errorf("devin/gemini-3-8-flash OwnedBy = %q, want google", m.OwnedBy)
			}
		}
		if m != nil && m.ID == "devin/grok-4-6" {
			foundGrok46 = true
			if m.OwnedBy != "xai" {
				t.Errorf("devin/grok-4-6 OwnedBy = %q, want xai", m.OwnedBy)
			}
		}
		if m != nil && m.ID == "devin/deepseek-v4-flash" {
			foundDeepSeekV4Flash = true
			if m.OwnedBy != "deepseek" {
				t.Errorf("devin/deepseek-v4-flash OwnedBy = %q, want deepseek", m.OwnedBy)
			}
		}
		if m != nil && m.ID == "devin/deepseek-v4-1-flash-1017" {
			foundDeepSeekV41Flash = true
			if m.OwnedBy != "deepseek" {
				t.Errorf("devin/deepseek-v4-1-flash-1017 OwnedBy = %q, want deepseek", m.OwnedBy)
			}
		}
	}
	if !foundSWE2 {
		t.Error("expected devin/swe-2 in GetDevinModels()")
	}
	if !foundFable {
		t.Error("expected devin/claude-fable-5-1 in GetDevinModels()")
	}
	if !foundGemini38 {
		t.Error("expected devin/gemini-3-8-flash in GetDevinModels()")
	}
	if !foundGrok46 {
		t.Error("expected devin/grok-4-6 in GetDevinModels()")
	}
	if !foundDeepSeekV4Flash {
		t.Error("expected devin/deepseek-v4-flash in GetDevinModels()")
	}
	if !foundDeepSeekV41Flash {
		t.Error("expected devin/deepseek-v4-1-flash-1017 in GetDevinModels()")
	}

	byChannel := GetStaticModelDefinitionsByChannel("devin")
	if len(byChannel) == 0 {
		t.Fatal("GetStaticModelDefinitionsByChannel(\"devin\") returned empty list")
	}

	info := LookupStaticModelInfo("devin/swe-2")
	if info == nil {
		t.Fatal("LookupStaticModelInfo(\"devin/swe-2\") = nil, want valid model")
	}
	if info.DisplayName != "SWE-2" {
		t.Errorf("info.DisplayName = %q, want SWE-2", info.DisplayName)
	}
}
