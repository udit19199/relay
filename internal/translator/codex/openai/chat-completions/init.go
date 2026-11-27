package chat_completions

import (
	. "github.com/udit19199/relay/internal/constant"
	"github.com/udit19199/relay/internal/interfaces"
	"github.com/udit19199/relay/internal/translator/translator"
)

func init() {
	translator.Register(
		OpenAI,
		Codex,
		ConvertOpenAIRequestToCodex,
		interfaces.TranslateResponse{
			Stream:    ConvertCodexResponseToOpenAI,
			NonStream: ConvertCodexResponseToOpenAINonStream,
		},
	)
}
