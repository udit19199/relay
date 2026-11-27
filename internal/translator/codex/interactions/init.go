package interactions

import (
	. "github.com/udit19199/relay/internal/constant"
	"github.com/udit19199/relay/internal/interfaces"
	"github.com/udit19199/relay/internal/translator/translator"
)

func init() {
	translator.Register(
		Interactions,
		Codex,
		ConvertInteractionsRequestToCodex,
		interfaces.TranslateResponse{
			Stream:    ConvertCodexResponseToInteractions,
			NonStream: ConvertCodexResponseToInteractionsNonStream,
		},
	)
}
