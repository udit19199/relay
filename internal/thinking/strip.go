// Package thinking provides unified thinking configuration processing.
package thinking

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/udit19199/relay/internal/util"
)

func stripThinkingFieldsDoc(doc *ojson.Doc, format string, keepTopLevelEffort ...bool) bool {
	if doc == nil {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(format))
	if provider == "openai-response" {
		provider = "codex"
	}
	keepEffort := len(keepTopLevelEffort) > 0 && keepTopLevelEffort[0]

	var paths []string
	switch provider {
	case "claude":
		if keepEffort {
			paths = []string{"thinking"}
		} else {
			paths = []string{"thinking", "output_config.effort"}
		}
	case "interactions":
		paths = []string{
			"generation_config.thinking_level",
			"generation_config.thinkingLevel",
			"generation_config.thinking_budget",
			"generation_config.thinkingBudget",
			"generation_config.thinking_summaries",
			"generation_config.thinkingSummaries",
			"generation_config.thinking_config",
			"generation_config.thinkingConfig",
		}
	case "openai":
		if keepEffort {
			paths = []string{"reasoning"}
		} else {
			paths = []string{"reasoning_effort", "reasoning"}
		}
	case "codex", "xai":
		if !keepEffort {
			paths = []string{"reasoning"}
		}
	default:
		return false
	}

	modified := false
	for _, path := range paths {
		if doc.Get(path).Exists() {
			if doc.Delete(path) == nil {
				modified = true
			}
		}
	}

	// Avoid leaving an empty output_config object for Claude when effort was the only field.
	if provider == "claude" && !keepEffort {
		if oc := doc.Get("output_config"); oc.Exists() && oc.IsObject() && len(oc.Map()) == 0 {
			if util.DeleteJSONDocIfExists(doc, "output_config") == nil {
				modified = true
			}
		}
	}
	return modified
}
