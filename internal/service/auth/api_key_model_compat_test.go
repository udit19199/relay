package auth

import (
	"testing"

	internalconfig "github.com/udit19199/relay/internal/config"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

func TestResolvedAPIKeyModelInfoPropagatesIsCompat(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{ClaudeKey: []internalconfig.ClaudeKey{{
		APIKey: "compat-key",
		Prefix: "tenant",
		Models: []internalconfig.ClaudeModel{{
			Name:     "deepseek-upstream",
			Alias:    "deepseek-alias",
			IsCompat: true,
		}},
	}}})
	auth := configuredCapabilityTestAuth("compat-auth", "compat-key")
	registerCapabilityTestAuth(t, manager, auth)

	req := manager.attachResolvedAPIKeyModelInfo(coreexecutor.Request{}, auth, "tenant/deepseek-alias", "deepseek-upstream")
	info, ok := ResolvedAPIKeyModelInfo(req)
	if !ok || info == nil || !info.IsCompat {
		t.Fatalf("ResolvedAPIKeyModelInfo() = (%+v, %t), want IsCompat=true", info, ok)
	}
}

func TestOpenAICompatAttrsTrimsAndToleratesMissingAttributes(t *testing.T) {
	cases := []struct {
		name        string
		auth        *Auth
		providerKey string
		compatName  string
	}{
		{name: "nil auth"},
		{name: "nil attributes", auth: &Auth{}},
		{
			name:        "trimmed values",
			auth:        &Auth{Attributes: map[string]string{"provider_key": " pool ", "compat_name": "\tcompat\n"}},
			providerKey: "pool",
			compatName:  "compat",
		},
	}
	for _, tc := range cases {
		providerKey, compatName := openAICompatAttrs(tc.auth)
		if providerKey != tc.providerKey || compatName != tc.compatName {
			t.Fatalf("%s: openAICompatAttrs() = (%q, %q), want (%q, %q)", tc.name, providerKey, compatName, tc.providerKey, tc.compatName)
		}
	}
}
