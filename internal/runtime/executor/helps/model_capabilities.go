package helps

import (
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// APIKeyModelIsCompat reports whether the selected API-key model enables
// compatibility handling for Claude thinking blocks.
func APIKeyModelIsCompat(req cliproxyexecutor.Request) bool {
	modelInfo, ok := cliproxyauth.ResolvedModelInfo(req)
	return ok && modelInfo != nil && modelInfo.IsCompat
}

// ApplyRequestThinking preserves the registry lookup path unless the auth
// manager bound authoritative model capabilities to this execution attempt.
func ApplyRequestThinking(body []byte, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, fromFormat, toFormat, provider string) ([]byte, error) {
	return ApplyRequestThinkingWithModelInfo(body, req, opts, fromFormat, toFormat, provider, nil)
}

// ApplyRequestThinkingDoc applies request thinking onto doc in place, preserving
// the registry lookup path unless the auth manager bound authoritative model
// capabilities to this execution attempt.
func ApplyRequestThinkingDoc(doc *ojson.Doc, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, fromFormat, toFormat, providerKey string) error {
	return ApplyRequestThinkingWithModelInfoDoc(doc, req, opts, fromFormat, toFormat, providerKey, nil)
}

// ApplyRequestThinkingWithModelInfo applies request thinking and falls back to
// fallbackModelInfo when neither request metadata nor the global registry
// resolves a model definition.
func ApplyRequestThinkingWithModelInfo(body []byte, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, fromFormat, toFormat, provider string, fallbackModelInfo *registry.ModelInfo) ([]byte, error) {
	originalSource := opts.OriginalRequest
	if len(originalSource) == 0 {
		originalSource = req.Payload
	}
	source := req.Payload
	if len(source) == 0 {
		source = opts.OriginalRequest
	}
	summaryConfig := translatedRequestSummaryConfig(body, req.Payload, originalSource, req.Model, fromFormat, toFormat)
	if modelInfo, ok := cliproxyauth.ResolvedModelInfo(req); ok {
		return thinking.ApplyThinkingWithModelInfoAndSummary(body, source, req.Model, fromFormat, toFormat, provider, modelInfo, summaryConfig)
	}
	return thinking.ApplyThinkingWithFallbackModelInfoAndSummary(body, source, req.Model, fromFormat, toFormat, provider, fallbackModelInfo, summaryConfig)
}

// ApplyRequestThinkingWithModelInfoDoc applies request thinking onto doc in place
// and falls back to fallbackModelInfo when neither request metadata nor the global
// registry resolves a model definition.
func ApplyRequestThinkingWithModelInfoDoc(doc *ojson.Doc, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, fromFormat, toFormat, provider string, fallbackModelInfo *registry.ModelInfo) error {
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
	if modelInfo, ok := cliproxyauth.ResolvedModelInfo(req); ok {
		return thinking.ApplyThinkingWithModelInfoAndSummaryDoc(doc, source, req.Model, fromFormat, toFormat, provider, modelInfo, summaryConfig)
	}
	return thinking.ApplyThinkingWithFallbackModelInfoAndSummaryDoc(doc, source, req.Model, fromFormat, toFormat, provider, fallbackModelInfo, summaryConfig)
}
