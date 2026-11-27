package helps

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/udit19199/relay/internal/thinking"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

// ApplyThinkingWithSourcePayloadDoc preserves summary visibility from the original
// client payload while applying thinking configuration onto doc in place.
func ApplyThinkingWithSourcePayloadDoc(doc *ojson.Doc, currentSourcePayload, originalSourcePayload []byte, model, fromFormat, toFormat, providerKey string) error {
	if doc == nil {
		return nil
	}
	summary := translatedRequestSummaryConfigDoc(doc, currentSourcePayload, originalSourcePayload, model, fromFormat, toFormat)
	return thinking.ApplyThinkingWithSummaryDoc(doc, model, fromFormat, toFormat, providerKey, summary)
}

// translatedRequestSummaryConfig gives the translated target payload precedence
// so an earlier request rewrite can remove or rewrite a canonical summary field.
// The original source is consulted only when the payload that was translated no
// longer carries the inbound intent, or when the target could not represent that
// intent until model-aware thinking is applied later (notably Claude).
func translatedRequestSummaryConfig(body, currentSourcePayload, originalSourcePayload []byte, model, fromFormat, toFormat string) thinking.SummaryConfig {
	fromFormat = strings.ToLower(strings.TrimSpace(fromFormat))
	toFormat = strings.ToLower(strings.TrimSpace(toFormat))

	var targetSummary thinking.SummaryConfig
	if fromFormat == toFormat {
		targetSummary = thinking.ExtractSummaryConfig(body, toFormat)
	} else {
		targetSummary = thinking.ExtractExplicitSummaryConfig(body, toFormat)
	}
	if targetSummary.Mode != thinking.SummaryUnspecified {
		return targetSummary
	}

	currentSummary := thinking.ExtractTranslatedSummaryConfig(currentSourcePayload, fromFormat, toFormat)
	originalSummary := thinking.ExtractTranslatedSummaryConfig(originalSourcePayload, fromFormat, toFormat)
	if currentSummary.Mode == thinking.SummaryUnspecified {
		return originalSummary
	}

	from := translatorreg.FromString(fromFormat)
	to := translatorreg.FromString(toFormat)
	if !translatorreg.HasRequestTransformer(from, to) {
		// A missing translation must remain source-shaped. Same-format requests
		// were handled by targetSummary above, including explicit native aliases.
		return thinking.SummaryConfig{}
	}

	candidate := thinking.ApplySummaryConfigForModel(body, toFormat, model, currentSummary)
	if thinking.ExtractExplicitSummaryConfig(candidate, toFormat).Mode != thinking.SummaryUnspecified {
		// Registry translation applied this field before later request normalization. If
		// it is absent now but can be represented on the normalized body, the
		// rewrite deliberately removed it and must remain authoritative.
		return thinking.SummaryConfig{}
	}

	// Some intents cannot be represented until the final model-aware pass. For
	// example, Claude display is invalid on disabled thinking, but a suffix can
	// subsequently activate adaptive thinking. Preserve the source in that case.
	return currentSummary
}

func translatedRequestSummaryConfigDoc(doc *ojson.Doc, currentSourcePayload, originalSourcePayload []byte, model, fromFormat, toFormat string) thinking.SummaryConfig {
	if doc == nil || doc.Len() == 0 {
		return thinking.SummaryConfig{}
	}
	fromFormat = strings.ToLower(strings.TrimSpace(fromFormat))
	toFormat = strings.ToLower(strings.TrimSpace(toFormat))

	var targetSummary thinking.SummaryConfig
	if fromFormat == toFormat {
		targetSummary = thinking.ExtractSummaryConfigDoc(doc, toFormat)
	} else {
		targetSummary = thinking.ExtractExplicitSummaryConfigDoc(doc, toFormat)
	}
	if targetSummary.Mode != thinking.SummaryUnspecified {
		return targetSummary
	}

	currentSummary := thinking.ExtractTranslatedSummaryConfig(currentSourcePayload, fromFormat, toFormat)
	originalSummary := thinking.ExtractTranslatedSummaryConfig(originalSourcePayload, fromFormat, toFormat)
	if currentSummary.Mode == thinking.SummaryUnspecified {
		return originalSummary
	}

	from := translatorreg.FromString(fromFormat)
	to := translatorreg.FromString(toFormat)
	if !translatorreg.HasRequestTransformer(from, to) {
		return thinking.SummaryConfig{}
	}

	if thinking.CanRepresentSummaryConfigForModelDoc(doc, toFormat, model, currentSummary) {
		return thinking.SummaryConfig{}
	}

	return currentSummary
}
