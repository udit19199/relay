package helps

import (
	"strings"

	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
	"github.com/udit19199/relay/internal/util"
)

// IsNativeCodexRequest checks the client dialect for use inside Codex executors.
func IsNativeCodexRequest(body []byte, opts coreexecutor.Options) bool {
	for _, format := range []translatorreg.Format{opts.SourceFormat, coreexecutor.ResponseFormatOrSource(opts)} {
		name := strings.TrimSpace(format.String())
		if !strings.EqualFold(name, translatorreg.FormatCodex.String()) && !strings.EqualFold(name, translatorreg.FormatOpenAIResponse.String()) {
			return false
		}
	}
	return util.IsCodexResponsesLiteRequest(body, opts.Headers)
}
