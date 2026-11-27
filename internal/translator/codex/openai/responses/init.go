package responses

import (
	. "github.com/udit19199/relay/internal/constant"
	"github.com/udit19199/relay/internal/interfaces"
	"github.com/udit19199/relay/internal/translator/translator"
)

func init() {
	translator.Register(
		OpenaiResponse,
		Codex,
		ConvertOpenAIResponsesRequestToCodex,
		interfaces.TranslateResponse{
			Stream:    ConvertCodexResponseToOpenAIResponses,
			NonStream: ConvertCodexResponseToOpenAIResponsesNonStream,
		},
	)
}
