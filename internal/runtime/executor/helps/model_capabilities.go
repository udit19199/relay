package helps

import (
	"github.com/MoeForge/moego/ojson"
	"github.com/udit19199/relay/internal/registry"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	"github.com/udit19199/relay/internal/thinking"
)

// APIKeyModelIsCompat reports whether the selected API-key model enables
// compatibility handling for Claude thinking blocks.
func APIKeyModelIsCompat(req coreexecutor.Request) bool {
	modelInfo, ok := coreauth.ResolvedModelInfo(req)
	return ok && modelInfo != nil && modelInfo.IsCompat
}

// ApplyRequestThinking preserves the registry lookup path unless the auth
// manager bound authoritative model capabilities to this execution attempt.
func ApplyRequestThinking(body []byte, req coreexecutor.Request, opts coreexecutor.Options, fromFormat, toFormat, provider string) ([]byte, error) {
	return ApplyRequestThinkingWithModelInfo(body, req, opts, fromFormat, toFormat, provider, nil)
}

// ApplyRequestThinkingDoc applies request thinking onto doc in place, preserving
// the registry lookup path unless the auth manager bound authoritative model
// capabilities to this execution attempt.
func ApplyRequestThinkingDoc(doc *ojson.Doc, req coreexecutor.Request, opts coreexecutor.Options, fromFormat, toFormat, providerKey string) error {
	return ApplyRequestThinkingWithModelInfoDoc(doc, req, opts, fromFormat, toFormat, providerKey, nil)
}

// ApplyRequestThinkingWithModelInfo applies request thinking and falls back to
// fallbackModelInfo when neither request metadata nor the global registry
// resolves a model definition.
func ApplyRequestThinkingWithModelInfo(body []byte, req coreexecutor.Request, opts coreexecutor.Options, fromFormat, toFormat, provider string, fallbackModelInfo *registry.ModelInfo) ([]byte, error) {
	originalSource := opts.OriginalRequest
	if len(originalSource) == 0 {
		originalSource = req.Payload
	}
	source := req.Payload
	if len(source) == 0 {
		source = opts.OriginalRequest
	}
	summaryConfig := translatedRequestSummaryConfig(body, req.Payload, originalSource, req.Model, fromFormat, toFormat)
	if modelInfo, ok := coreauth.ResolvedModelInfo(req); ok {
		return thinking.ApplyThinkingWithModelInfoAndSummary(body, source, req.Model, fromFormat, toFormat, provider, modelInfo, summaryConfig)
	}
	return thinking.ApplyThinkingWithFallbackModelInfoAndSummary(body, source, req.Model, fromFormat, toFormat, provider, fallbackModelInfo, summaryConfig)
}

// ApplyRequestThinkingWithModelInfoDoc applies request thinking onto doc in place
// and falls back to fallbackModelInfo when neither request metadata nor the global
// registry resolves a model definition.
func ApplyRequestThinkingWithModelInfoDoc(doc *ojson.Doc, req coreexecutor.Request, opts coreexecutor.Options, fromFormat, toFormat, provider string, fallbackModelInfo *registry.ModelInfo) error {
	if doc == nil {
		return nil
	}
	originalSource := opts.OriginalRequest
	if len(originalSource) == 0 {
		originalSource = req.Payload
	}
	source := req.Payload
	if len(source) == 0 {
		source = opts.OriginalRequest
	}
	summaryConfig := translatedRequestSummaryConfigDoc(doc, req.Payload, originalSource, req.Model, fromFormat, toFormat)
	if modelInfo, ok := coreauth.ResolvedModelInfo(req); ok {
		return thinking.ApplyThinkingWithModelInfoAndSummaryDoc(doc, source, req.Model, fromFormat, toFormat, provider, modelInfo, summaryConfig)
	}
	return thinking.ApplyThinkingWithFallbackModelInfoAndSummaryDoc(doc, source, req.Model, fromFormat, toFormat, provider, fallbackModelInfo, summaryConfig)
}
