package executor

import (
	"testing"

	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
)

func TestApplyPatchActualExecutors(t *testing.T) {
	cfg := &config.Config{}
	for _, exec := range []coreauth.ProviderExecutor{
		NewOpenAICompatExecutor("arbitrary-consumer-provider", cfg), NewClaudeExecutor(cfg),
		NewDevinExecutor(cfg),
		NewCodexExecutor(cfg), NewCodexWebsocketsExecutor(cfg), NewCodexAutoExecutor(cfg),
		NewXAIExecutor(cfg), NewXAIWebsocketsExecutor(cfg), NewXAIAutoExecutor(cfg), NewMetaExecutor(cfg),
	} {
		support, okSupport := exec.(coreauth.ApplyPatchSupport)
		if !okSupport || !support.SupportsApplyPatch() {
			t.Errorf("%T (%s) does not advertise actual support", exec, exec.Identifier())
		}
		if scoped, okScoped := exec.(coreauth.APIKeyConfigExecutor); okScoped {
			bound := scoped.ForAPIKey()
			if bound.Identifier() != exec.Identifier() || !bound.(coreauth.ApplyPatchSupport).SupportsApplyPatch() {
				t.Errorf("%T ForAPIKey lost the actual executor contract", exec)
			}
		}
	}
	for _, exec := range []coreauth.ApplyPatchSupport{&CodexAutoExecutor{}, &XAIAutoExecutor{}} {
		if exec.SupportsApplyPatch() {
			t.Errorf("unconfigured %T advertises support", exec)
		}
	}
}
