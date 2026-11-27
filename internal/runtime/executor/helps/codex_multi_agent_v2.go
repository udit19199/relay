package helps

import (
	"context"
	"net/http"

	"github.com/MoeForge/moego/ojson"
	multiagentv2 "github.com/udit19199/relay/internal/client/codex/optimize-multi-agent-v2"
	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	"github.com/udit19199/relay/internal/thinking"
	openaichatclaude "github.com/udit19199/relay/internal/translator/claude/openai/chat-completions"
	responsesclaude "github.com/udit19199/relay/internal/translator/claude/openai/responses"
	codexclaude "github.com/udit19199/relay/internal/translator/codex/claude"
	interactionsclaude "github.com/udit19199/relay/internal/translator/interactions/claude"
	openaiclaude "github.com/udit19199/relay/internal/translator/openai/claude"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

// RewriteCodexMultiAgentV2InputDoc converts official Codex multi-agent input into
// standard Responses API messages in place on doc when multi-agent v2 optimization is enabled.
func RewriteCodexMultiAgentV2InputDoc(doc *ojson.Doc, ctx context.Context, headers http.Header, cfg *config.Config) bool {
	return multiagentv2.RewriteCodexMultiAgentV2InputDoc(doc, ctx, headers, cfg)
}

// RewriteCodexOrphanDelegationInputDoc converts orphan Codex delegation outputs into
// standard user messages in place on doc when orphan delegation compatibility is enabled.
func RewriteCodexOrphanDelegationInputDoc(doc *ojson.Doc, ctx context.Context, headers http.Header, cfg *config.Config) bool {
	return multiagentv2.RewriteCodexOrphanDelegationInputForConfigDoc(doc, ctx, headers, cfg)
}

// TranslateRequestWithCodexMultiAgentV2 normalizes official Codex multi-agent
// input before translating it to a non-Codex target protocol.
func TranslateRequestWithCodexMultiAgentV2(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, model string, payload []byte, stream bool) []byte {
	if IsCodexUserAgent(headers) {
		payload = NormalizeCodexToolIntegerTypes(payload, headers)
	}
	return multiagentv2.TranslateRequestWithCodexMultiAgentV2(ctx, headers, cfg, from, to, model, payload, stream)
}

// TranslateRequestEnvelopeWithCodexMultiAgentV2 normalizes official Codex
// multi-agent input while preserving the complete request envelope.
func TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, req translatorreg.RequestEnvelope) translatorreg.RequestEnvelope {
	return multiagentv2.TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx, headers, cfg, from, to, req)
}

// TranslateRequestPairWithCodexMultiAgentV2 translates the untouched baseline
// payload and the working payload that later stages mutate in place. Executors
// normally assign the original payload to the request before translating, so the
// same input is translated once, then copied into a
// separate working buffer. This avoids a full
// extra pass over payloads that can reach tens of megabytes.
func TranslateRequestPairWithCodexMultiAgentV2(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, model string, originalPayload, requestPayload []byte, stream bool) (original, working []byte) {
	req := translatorreg.RequestEnvelope{Format: from, Model: model, Stream: stream}
	original, working, _ = TranslateRequestEnvelopePairWithCodexMultiAgentV2(ctx, headers, cfg, from, to, req, originalPayload, requestPayload)
	return original, working
}

// TranslateRequestEnvelopePairWithCodexMultiAgentV2 translates the baseline and
// working payload while preserving request-scoped metadata in req. The returned
// error is the one for the working payload, which is the payload sent upstream;
// a failure of the baseline alone never rejects a working payload that translated.
func TranslateRequestEnvelopePairWithCodexMultiAgentV2(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, req translatorreg.RequestEnvelope, originalPayload, requestPayload []byte) (original, working []byte, err error) {
	originalReq := req
	originalReq.Body = originalPayload
	originalTranslated := TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx, headers, cfg, from, to, originalReq)
	original = originalTranslated.Body
	if sameByteSlice(originalPayload, requestPayload) {
		// The caller mutates the working copy, so it must not share the baseline array.
		// A single translation serves both payloads, so its error is the working error.
		return original, append([]byte(nil), original...), originalTranslated.Err
	}
	workingReq := req
	workingReq.Body = requestPayload
	workingTranslated := TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx, headers, cfg, from, to, workingReq)
	return original, workingTranslated.Body, workingTranslated.Err
}

// sameByteSlice reports whether both slices describe the same bytes of the same
// backing array. It compares identity rather than content so the check stays
// constant time on large payloads.
func sameByteSlice(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return &a[0] == &b[0]
}

// TranslateRequestPairWithAPIKeyModelCompatibility avoids translating identical
// inputs twice while retaining separate buffers.
func TranslateRequestPairWithAPIKeyModelCompatibility(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, model string, originalPayload, requestPayload []byte, stream, isCompat bool) (original, working []byte) {
	original, working, _ = translateRequestPairWithAPIKeyModelCompatibility(ctx, headers, cfg, from, to, model, originalPayload, requestPayload, stream, isCompat)
	return original, working
}

// TranslateRequestPairReturningError reports a content part the target cannot represent.
func TranslateRequestPairReturningError(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, model string, originalPayload, requestPayload []byte, stream, isCompat bool) ([]byte, []byte, error) {
	return translateRequestPairWithAPIKeyModelCompatibility(ctx, headers, cfg, from, to, model, originalPayload, requestPayload, stream, isCompat)
}

func translateRequestPairWithAPIKeyModelCompatibility(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, model string, originalPayload, requestPayload []byte, stream, isCompat bool) (original, working []byte, err error) {
	original, err = translateRequestWithAPIKeyModelCompatibilityForExecutor(ctx, headers, cfg, "", from, to, model, originalPayload, stream, isCompat)
	if sameByteSlice(originalPayload, requestPayload) {
		// A single translation serves both payloads, so its error is the working error.
		return original, append([]byte(nil), original...), err
	}
	// Only the working payload is sent upstream, so only its error counts.
	working, err = translateRequestWithAPIKeyModelCompatibilityForExecutor(ctx, headers, cfg, "", from, to, model, requestPayload, stream, isCompat)
	return original, working, err
}

// TranslateRequestWithAPIKeyModelCompatibility applies compatibility-aware
// request translators when a configured API-key model enables compatibility mode.
func TranslateRequestWithAPIKeyModelCompatibility(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, model string, payload []byte, stream, isCompat bool) []byte {
	return TranslateRequestWithAPIKeyModelCompatibilityForExecutor(ctx, headers, cfg, "", from, to, model, payload, stream, isCompat)
}

// TranslateRequestWithAPIKeyModelCompatibilityForExecutor applies compatibility-aware
// request translation while preserving the actual target executor's schema policy.
func TranslateRequestWithAPIKeyModelCompatibilityForExecutor(ctx context.Context, headers http.Header, cfg *config.Config, targetExecutor string, from, to translatorreg.Format, model string, payload []byte, stream, isCompat bool) []byte {
	body, _ := translateRequestWithAPIKeyModelCompatibilityForExecutor(ctx, headers, cfg, targetExecutor, from, to, model, payload, stream, isCompat)
	return body
}

// TranslateRequestReturningError reports a content part the target cannot represent.
func TranslateRequestReturningError(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, model string, payload []byte, stream, isCompat bool) ([]byte, error) {
	return TranslateRequestReturningErrorForExecutor(ctx, headers, cfg, "", from, to, model, payload, stream, isCompat)
}

// TranslateRequestReturningErrorForExecutor reports a content part the target
// cannot represent while preserving the actual target executor's schema policy.
func TranslateRequestReturningErrorForExecutor(ctx context.Context, headers http.Header, cfg *config.Config, targetExecutor string, from, to translatorreg.Format, model string, payload []byte, stream, isCompat bool) ([]byte, error) {
	return translateRequestWithAPIKeyModelCompatibilityForExecutor(ctx, headers, cfg, targetExecutor, from, to, model, payload, stream, isCompat)
}

func translateRequestWithAPIKeyModelCompatibilityForExecutor(ctx context.Context, headers http.Header, cfg *config.Config, targetExecutor string, from, to translatorreg.Format, model string, payload []byte, stream, isCompat bool) ([]byte, error) {
	needsIntNorm := IsCodexUserAgent(headers) && !isCodexTargetExecutor(targetExecutor)
	if !isCompat {
		if needsIntNorm {
			payload = NormalizeCodexToolIntegerTypes(payload, headers)
		}
		return translateRequestWithCodexMultiAgentV2Checked(ctx, headers, cfg, from, to, model, payload, stream)
	}
	if from == translatorreg.FormatOpenAIResponse {
		if len(payload) > 0 {
			doc := ojson.NewDoc(payload)
			changed := false
			if needsIntNorm && NormalizeCodexToolIntegerTypesDoc(doc, headers) {
				changed = true
			}
			if RewriteCodexOrphanDelegationInputDoc(doc, ctx, headers, cfg) {
				changed = true
			}
			if to != translatorreg.FormatCodex && to != translatorreg.FormatOpenAIResponse {
				if multiagentv2.RewriteCodexMultiAgentV2InputDoc(doc, ctx, headers, cfg, isCompat) {
					changed = true
				}
			}
			if changed {
				payload = doc.Bytes()
			}
		}
	} else if needsIntNorm {
		payload = NormalizeCodexToolIntegerTypes(payload, headers)
	}

	var translated []byte
	var convertErr error
	switch {
	case from == translatorreg.FormatClaude && to == translatorreg.FormatCodex:
		translated, convertErr = codexclaude.ConvertClaudeRequestToCodexWithCompatContext(ctx, model, payload, stream)
	case from == translatorreg.FormatClaude && to == translatorreg.FormatInteractions:
		translated, convertErr = interactionsclaude.ConvertClaudeRequestToInteractionsWithCompat(model, payload, stream)
	case from == translatorreg.FormatClaude && to == translatorreg.FormatOpenAI:
		translated, convertErr = openaiclaude.ConvertClaudeRequestToOpenAIWithCompat(model, payload, stream)
	case from == translatorreg.FormatOpenAI && to == translatorreg.FormatClaude:
		translated, convertErr = openaichatclaude.ConvertOpenAIRequestToClaudeWithCompat(model, payload, stream)
	case from == translatorreg.FormatOpenAIResponse && to == translatorreg.FormatClaude:
		translated, convertErr = responsesclaude.ConvertOpenAIResponsesRequestToClaudeWithCompatContext(ctx, model, payload, stream)
	default:
		return translateRequestWithCodexMultiAgentV2Checked(ctx, headers, cfg, from, to, model, payload, stream)
	}

	summaryConfig := thinking.ExtractTranslatedSummaryConfig(payload, from.String(), to.String())
	translated = thinking.ApplySummaryConfigForModel(translated, to.String(), model, summaryConfig)
	return translated, convertErr
}

// HasCodexMultiAgentV2NamespaceConflictDoc reports whether doc defines
// the reserved optimized namespace, which must remain untouched.
func HasCodexMultiAgentV2NamespaceConflictDoc(doc *ojson.Doc) bool {
	return multiagentv2.HasCodexMultiAgentV2NamespaceConflictDoc(doc)
}

// OptimizeCodexMultiAgentV2RequestForAuthDoc applies the standard Codex MultiAgentV2
// request optimization in place on doc and reports whether the collaboration namespace
// was renamed for upstream use.
func OptimizeCodexMultiAgentV2RequestForAuthDoc(doc *ojson.Doc, ctx context.Context, headers http.Header, cfg *config.Config, auth *coreauth.Auth, isCompat bool) bool {
	if doc == nil || doc.Len() == 0 {
		return false
	}
	if auth != nil && auth.AuthKind() == coreauth.AuthKindAPIKey {
		cfg = cfg.ForAPIKey()
	}
	multiagentv2.RewriteCodexOrphanDelegationInputForConfigDoc(doc, ctx, headers, cfg)
	optimized := multiagentv2.OptimizeCodexMultiAgentV2RequestDoc(doc, ctx, headers, cfg)
	if isCompat {
		multiagentv2.RewriteCodexMultiAgentV2InputDoc(doc, ctx, headers, cfg, true)
	}
	return optimized
}

// RestoreCodexMultiAgentV2Response restores optimized collaboration namespace
// values before an upstream response is translated and returned to the client.
func RestoreCodexMultiAgentV2Response(payload []byte, optimized bool) []byte {
	return multiagentv2.RestoreCodexMultiAgentV2Response(payload, optimized)
}

// translateRequestWithCodexMultiAgentV2Checked is the multi-agent translation that
// keeps the envelope failure instead of dropping it with the body.
func translateRequestWithCodexMultiAgentV2Checked(ctx context.Context, headers http.Header, cfg *config.Config, from, to translatorreg.Format, model string, payload []byte, stream bool) ([]byte, error) {
	translated := multiagentv2.TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx, headers, cfg, from, to, translatorreg.RequestEnvelope{Format: from, Model: model, Stream: stream, Body: payload})
	return translated.Body, translated.Err
}
