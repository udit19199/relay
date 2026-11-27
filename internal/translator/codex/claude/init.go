package claude

import (
	"context"

	. "github.com/udit19199/relay/internal/constant"
	"github.com/udit19199/relay/internal/interfaces"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
	"github.com/udit19199/relay/internal/translator/translator"
)

func init() {
	translator.Register(
		Claude,
		Codex,
		ConvertClaudeRequestToCodex,
		interfaces.TranslateResponse{
			Stream:     ConvertCodexResponseToClaude,
			NonStream:  ConvertCodexResponseToClaudeNonStream,
			TokenCount: ClaudeTokenCount,
		},
	)
	translatorreg.RegisterRequestEnvelope(translatorreg.FormatClaude, translatorreg.FormatCodex,
		func(ctx context.Context, req translatorreg.RequestEnvelope) translatorreg.RequestEnvelope {
			req.Body, req.Err = ConvertClaudeRequestToCodexContext(ctx, req.Model, req.Body, req.Stream)
			return req
		})
}
