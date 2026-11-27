package translator

import (
	_ "github.com/udit19199/relay/internal/translator/claude/interactions"
	_ "github.com/udit19199/relay/internal/translator/claude/openai/chat-completions"
	_ "github.com/udit19199/relay/internal/translator/claude/openai/responses"

	_ "github.com/udit19199/relay/internal/translator/codex/claude"
	_ "github.com/udit19199/relay/internal/translator/codex/interactions"
	_ "github.com/udit19199/relay/internal/translator/codex/openai/chat-completions"
	_ "github.com/udit19199/relay/internal/translator/codex/openai/responses"

	_ "github.com/udit19199/relay/internal/translator/interactions/claude"

	_ "github.com/udit19199/relay/internal/translator/openai/claude"
	_ "github.com/udit19199/relay/internal/translator/openai/interactions/chat-completions"
	_ "github.com/udit19199/relay/internal/translator/openai/interactions/responses"
	_ "github.com/udit19199/relay/internal/translator/openai/openai/chat-completions"
	_ "github.com/udit19199/relay/internal/translator/openai/openai/responses"
)
