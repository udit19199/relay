package responses

import (
	. "github.com/udit19199/relay/internal/constant"
	"github.com/udit19199/relay/internal/interfaces"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
	"github.com/udit19199/relay/internal/translator/translator"
)

func init() {
	translator.Register(
		OpenaiResponse,
		Claude,
		nil,
		interfaces.TranslateResponse{
			Stream:    ConvertClaudeResponseToOpenAIResponses,
			NonStream: ConvertClaudeResponseToOpenAIResponsesNonStream,
		},
	)
	// The envelope form receives the request context, which carries the
	// upstream.claude.tool-search value the executor pinned for the request.
	translatorreg.RegisterRequestEnvelope(
		translatorreg.FormatOpenAIResponse,
		translatorreg.FormatClaude,
		ConvertOpenAIResponsesRequestEnvelopeToClaude,
	)
}
