// Package registry provides model definitions and lookup helpers for various AI providers.
// Static model metadata is loaded from the embedded models.json file and can be refreshed from network.
package registry

import (
	"strings"
)

// staticModelsJSON mirrors the top-level structure of models.json.
type staticModelsJSON struct {
	Claude    []*ModelInfo `json:"claude"`
	CodexFree []*ModelInfo `json:"codex-free"`
	CodexTeam []*ModelInfo `json:"codex-team"`
	CodexPlus []*ModelInfo `json:"codex-plus"`
	CodexPro  []*ModelInfo `json:"codex-pro"`
	XAI       []*ModelInfo `json:"xai"`
	Devin     []*ModelInfo `json:"devin"`
}

// GetClaudeModels returns the standard Claude model definitions.
func GetClaudeModels() []*ModelInfo {
	return cloneModelInfos(getModels().Claude)
}

// GetCodexFreeModels returns model definitions for the Codex free plan tier.
func GetCodexFreeModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexFree)
}

// GetCodexTeamModels returns model definitions for the Codex team plan tier.
func GetCodexTeamModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexTeam)
}

// GetCodexPlusModels returns model definitions for the Codex plus plan tier.
func GetCodexPlusModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexPlus)
}

// GetCodexProModels returns model definitions for the Codex pro plan tier.
func GetCodexProModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexPro)
}

var staticDevinModels = []*ModelInfo{
	devinBuiltinSWE16SlowModelInfo(),
	{
		ID:                  "devin/swe-2",
		Type:                "devin",
		OwnedBy:             "cognition",
		DisplayName:         "SWE-2",
		ContextLength:       262000,
		MaxCompletionTokens: 128000,
		Thinking: &ThinkingSupport{
			Levels: []string{"medium", "high", "max"},
		},
	},
	{
		ID:                  "devin/claude-fable-5-1",
		Type:                "devin",
		OwnedBy:             "anthropic",
		DisplayName:         "Claude Fable 5.1",
		ContextLength:       1000000,
		MaxCompletionTokens: 64000,
		Thinking: &ThinkingSupport{
			Levels: []string{"low", "medium", "high", "xhigh", "max"},
		},
	},
	{
		ID:                  "devin/gpt-6-astra",
		Type:                "devin",
		OwnedBy:             "openai",
		DisplayName:         "GPT-6 Astra",
		ContextLength:       1000000,
		MaxCompletionTokens: 64000,
		Thinking: &ThinkingSupport{
			Levels: []string{"low", "medium", "high", "xhigh", "max"},
		},
	},
	{
		ID:                  "devin/glm-5-2",
		Type:                "devin",
		OwnedBy:             "zhipu",
		DisplayName:         "GLM-5.2",
		ContextLength:       200000,
		MaxCompletionTokens: 64000,
		Thinking: &ThinkingSupport{
			Levels: []string{"none", "high"},
		},
	},
	{
		ID:                  "devin/glm-5-3",
		Type:                "devin",
		OwnedBy:             "zhipu",
		DisplayName:         "GLM-5.3",
		ContextLength:       1048576,
		MaxCompletionTokens: 128000,
		Thinking: &ThinkingSupport{
			Levels: []string{"low", "high", "max"},
		},
	},
	{
		ID:                  "devin/glm-5-3-flash",
		Type:                "devin",
		OwnedBy:             "zhipu",
		DisplayName:         "GLM-5.3 Flash",
		ContextLength:       1000000,
		MaxCompletionTokens: 128000,
		Thinking: &ThinkingSupport{
			Levels: []string{"low", "high", "max"},
		},
	},
	{
		ID:                  "devin/gpt-5-6-sol",
		Type:                "devin",
		OwnedBy:             "openai",
		DisplayName:         "GPT-5.6 Sol",
		ContextLength:       1000000,
		MaxCompletionTokens: 128000,
		Thinking: &ThinkingSupport{
			Levels: []string{"none", "low", "medium", "high", "xhigh", "max"},
		},
	},
	{
		ID:                  "devin/gemini-3-8-flash",
		Type:                "devin",
		OwnedBy:             "google",
		DisplayName:         "Gemini 3.8 Flash",
		ContextLength:       1048576,
		MaxCompletionTokens: 65536,
		Thinking: &ThinkingSupport{
			Levels: []string{"low", "medium", "high"},
		},
	},
	{
		ID:                  "devin/grok-4-6",
		Type:                "devin",
		OwnedBy:             "xai",
		DisplayName:         "Grok 4.6",
		ContextLength:       500000,
		MaxCompletionTokens: 131072,
		Thinking: &ThinkingSupport{
			Levels: []string{"low", "medium", "high", "xhigh"},
		},
	},
	{
		ID:                  "devin/deepseek-v4-flash",
		Type:                "devin",
		OwnedBy:             "deepseek",
		DisplayName:         "DeepSeek V4 Flash",
		ContextLength:       1048576,
		MaxCompletionTokens: 64000,
		Thinking: &ThinkingSupport{
			Levels: []string{"high", "max"},
		},
	},
	{
		ID:                  "devin/deepseek-v4-1-flash",
		Type:                "devin",
		OwnedBy:             "deepseek",
		DisplayName:         "DeepSeek V4.1 Flash",
		ContextLength:       1048576,
		MaxCompletionTokens: 64000,
		Thinking: &ThinkingSupport{
			Levels: []string{"high", "max"},
		},
	},
}

// GetXAIModels returns the standard xAI Grok model definitions.
func GetXAIModels() []*ModelInfo {
	return cloneModelInfos(getModels().XAI)
}
func upsertModelInfos(models []*ModelInfo, extras ...*ModelInfo) []*ModelInfo {
	if len(extras) == 0 {
		return models
	}

	extraIDs := make(map[string]struct{}, len(extras))
	extraList := make([]*ModelInfo, 0, len(extras))
	for _, extra := range extras {
		if extra == nil {
			continue
		}
		id := strings.TrimSpace(extra.ID)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, exists := extraIDs[key]; exists {
			continue
		}
		extraIDs[key] = struct{}{}
		extraList = append(extraList, cloneModelInfo(extra))
	}

	if len(extraList) == 0 {
		return models
	}

	filtered := make([]*ModelInfo, 0, len(models)+len(extraList))
	for _, model := range models {
		if model == nil {
			continue
		}
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if _, exists := extraIDs[strings.ToLower(id)]; exists {
			continue
		}
		filtered = append(filtered, model)
	}

	filtered = append(filtered, extraList...)
	return filtered
}

// cloneModelInfos returns a shallow copy of the slice with each element deep-cloned.
func cloneModelInfos(models []*ModelInfo) []*ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]*ModelInfo, len(models))
	for i, m := range models {
		out[i] = cloneModelInfo(m)
	}
	return out
}

// GetStaticModelDefinitionsByChannel returns static model definitions for a given channel/provider.
// It returns nil when the channel is unknown.
//
// Supported channels:
//   - claude
//   - codex
//   - xai
//   - devin
//   - meta
func GetStaticModelDefinitionsByChannel(channel string) []*ModelInfo {
	key := strings.ToLower(strings.TrimSpace(channel))
	switch key {
	case "claude":
		return GetClaudeModels()
	case "codex":
		return GetCodexProModels()
	case "xai", "x-ai", "grok":
		return GetXAIModels()
	case "devin":
		return GetDevinModels()
	default:
		return nil
	}
}

// LookupStaticModelInfoByChannel searches one provider-specific static section.
// It does not fall back across providers, so callers can preserve provenance.
func LookupStaticModelInfoByChannel(modelID, channel string) *ModelInfo {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	models := GetStaticModelDefinitionsByChannel(channel)
	for _, model := range models {
		if model != nil && model.ID == modelID {
			return cloneModelInfo(model)
		}
	}
	if strings.EqualFold(strings.TrimSpace(channel), "claude") {
		for _, familyID := range claudeFamilyModelIDs(modelID) {
			for _, model := range models {
				if model != nil && model.ID == familyID {
					return cloneModelInfo(model)
				}
			}
		}
	}
	return nil
}

// LookupStaticModelInfo searches all static model definitions for a model by ID.
// Returns nil if no matching model is found.
func LookupStaticModelInfo(modelID string) *ModelInfo {
	if modelID == "" {
		return nil
	}

	data := getModels()
	allModels := [][]*ModelInfo{
		data.Claude,
		data.CodexPro,
		data.XAI,
		GetDevinModels(),
	}
	for _, models := range allModels {
		for _, m := range models {
			if m != nil && m.ID == modelID {
				return cloneModelInfo(m)
			}
		}
	}

	// Dated and bracketed Claude variants (claude-sonnet-5-5-20261001,
	// claude-opus-5-5[1m]) share the capabilities of their catalog entry.
	for _, familyID := range claudeFamilyModelIDs(modelID) {
		for _, m := range data.Claude {
			if m != nil && m.ID == familyID {
				return cloneModelInfo(m)
			}
		}
	}

	return nil
}

// claudeFamilyModelIDs returns the catalog IDs a Claude model variant may belong
// to: without a "[...]" context suffix, then also without a "-YYYYMMDD" date.
func claudeFamilyModelIDs(modelID string) []string {
	if !strings.HasPrefix(modelID, "claude-") {
		return nil
	}
	var ids []string
	base := modelID
	if i := strings.IndexByte(base, '['); i > 0 && strings.HasSuffix(base, "]") {
		base = base[:i]
		ids = append(ids, base)
	}
	if n := len(base); n > 9 && base[n-9] == '-' && isEightDigits(base[n-8:]) {
		ids = append(ids, base[:n-9])
	}
	return ids
}

func isEightDigits(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
