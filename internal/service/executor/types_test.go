package executor

import (
	"testing"

	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

func TestResponseFormatOrSourceUsesExplicitResponseFormat(t *testing.T) {
	opts := Options{
		SourceFormat:   translatorreg.FormatOpenAI,
		ResponseFormat: translatorreg.FormatClaude,
	}

	if got := ResponseFormatOrSource(opts); got != translatorreg.FormatClaude {
		t.Fatalf("ResponseFormatOrSource() = %q, want %q", got, translatorreg.FormatClaude)
	}
}

func TestResponseFormatOrSourceFallsBackToSourceFormat(t *testing.T) {
	opts := Options{SourceFormat: translatorreg.FormatClaude}

	if got := ResponseFormatOrSource(opts); got != translatorreg.FormatClaude {
		t.Fatalf("ResponseFormatOrSource() = %q, want %q", got, translatorreg.FormatClaude)
	}
}
