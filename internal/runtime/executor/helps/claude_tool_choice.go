package helps

import (
	"regexp"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/udit19199/relay/internal/thinking"
	"github.com/udit19199/relay/internal/util"
)

var claude55Model = regexp.MustCompile(`^claude-(opus|sonnet)-5-5(?:-(high|reasoning))?(?:-[0-9]{8})?(?:\[[^\[\]]+\])?$`)

// NormalizeClaudeToolChoiceDoc downgrades forced tool use on Claude 5.5 to auto in place on doc.
func NormalizeClaudeToolChoiceDoc(provider, model string, doc *ojson.Doc) bool {
	if doc == nil || doc.Len() == 0 || provider != "claude" {
		return false
	}
	model = strings.ToLower(thinking.ParseSuffix(strings.TrimSpace(model)).ModelName)
	if !claude55Model.MatchString(model) {
		return false
	}
	switch doc.Get("tool_choice.type").String() {
	case "any", "tool":
		_ = doc.Set("tool_choice.type", "auto")
		_ = util.DeleteJSONDocIfExists(doc, "tool_choice.name")
		return true
	}
	return false
}
