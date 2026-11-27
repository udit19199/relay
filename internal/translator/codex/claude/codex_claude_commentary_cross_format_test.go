package claude

import (
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	interactionsclaude "github.com/udit19199/relay/internal/translator/interactions/claude"
	openaiclaude "github.com/udit19199/relay/internal/translator/openai/claude"
)

// A client may replay CPA-relayed commentary history to a non-Codex upstream.
// Every Claude request translator must restore the internal marker as plain
// assistant text before its own thinking handling, and the marker literal must
// never appear in the translated request.
const crossFormatCommentaryPayload = `{"model":"test-model","messages":[{"role":"user","content":"First turn"},{"role":"assistant","content":[{"type":"thinking","thinking":"COMMENTARY_SENTINEL","signature":"cpa:commentary:v1"},{"type":"text","text":"FINAL_SENTINEL"}]},{"role":"user","content":"Next turn"}]}`

func assertCommentaryRestoredAsText(t *testing.T, name string, out []byte, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: conversion error: %v", name, err)
	}
	if strings.Contains(string(out), "cpa:commentary:v1") {
		t.Fatalf("%s: internal marker leaked into translated request: %s", name, out)
	}
	if !strings.Contains(string(out), "COMMENTARY_SENTINEL") {
		t.Fatalf("%s: commentary text lost in translation: %s", name, out)
	}
	if !strings.Contains(string(out), "FINAL_SENTINEL") {
		t.Fatalf("%s: final text lost in translation: %s", name, out)
	}
}

func TestCommentaryMarkerRestoredByOpenAIChatTranslator(t *testing.T) {
	out, err := openaiclaude.ConvertClaudeRequestToOpenAI("test-model", []byte(crossFormatCommentaryPayload), false)
	assertCommentaryRestoredAsText(t, "openai default", out, err)
	if strings.Contains(string(out), "reasoning_content") && strings.Contains(string(out), "COMMENTARY_SENTINEL") && ojson.GetBytesNoCopy(out, "messages.1.reasoning_content").String() == "COMMENTARY_SENTINEL" {
		t.Fatalf("openai default: commentary must not become reasoning_content: %s", out)
	}

	out, err = openaiclaude.ConvertClaudeRequestToOpenAIWithCompat("test-model", []byte(crossFormatCommentaryPayload), false)
	assertCommentaryRestoredAsText(t, "openai compat", out, err)
	if got := ojson.GetBytesNoCopy(out, "messages.1.reasoning_content").String(); got == "COMMENTARY_SENTINEL" {
		t.Fatalf("openai compat: commentary must not become reasoning_content: %s", out)
	}
}

func TestCommentaryMarkerRestoredByInteractionsTranslator(t *testing.T) {
	out, err := interactionsclaude.ConvertClaudeRequestToInteractions("test-model", []byte(crossFormatCommentaryPayload), false)
	assertCommentaryRestoredAsText(t, "interactions default", out, err)

	out, err = interactionsclaude.ConvertClaudeRequestToInteractionsWithCompat("test-model", []byte(crossFormatCommentaryPayload), false)
	assertCommentaryRestoredAsText(t, "interactions compat", out, err)
	if strings.Contains(string(out), `"type":"thought"`) {
		t.Fatalf("interactions compat: commentary must not become a thought step: %s", out)
	}
}
