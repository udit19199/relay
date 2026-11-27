package api

import (
	"testing"

	"github.com/udit19199/relay/internal/config"
	proxyconfig "github.com/udit19199/relay/internal/config"
	responsesclaude "github.com/udit19199/relay/internal/translator/claude/openai/responses"
)

// setClaudeToolSearchForTest sets upstream.claude.tool-search for one test.
func setClaudeToolSearchForTest(t *testing.T, enabled bool) {
	t.Helper()
	previous := responsesclaude.ClientToolSearchEnabled()
	responsesclaude.SetClientToolSearchEnabled(enabled)
	t.Cleanup(func() { responsesclaude.SetClientToolSearchEnabled(previous) })
}

// The server owns upstream.claude.tool-search: it applies the switch at
// startup and on every config reload.
func TestServerAppliesClaudeToolSearchSwitch(t *testing.T) {
	setClaudeToolSearchForTest(t, false)
	cfg := &proxyconfig.Config{SDKConfig: config.SDKConfig{APIKeys: []string{"test-key"}}}
	cfg.Claude.ToolSearch = true
	server := newTestServerWithConfig(t, cfg)
	if !responsesclaude.ClientToolSearchEnabled() {
		t.Fatal("startup did not enable Claude tool search")
	}

	next := *cfg
	next.Claude.ToolSearch = false
	server.UpdateClients(&next)
	if responsesclaude.ClientToolSearchEnabled() {
		t.Fatal("reload did not disable Claude tool search")
	}

	again := next
	again.Claude.ToolSearch = true
	server.UpdateClients(&again)
	if !responsesclaude.ClientToolSearchEnabled() {
		t.Fatal("reload did not re-enable Claude tool search")
	}
}
