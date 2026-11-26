package translator

import (
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/interactions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/openai/chat-completions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/openai/responses"

	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/claude"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/interactions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/openai/chat-completions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/openai/responses"

	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/interactions/claude"

	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/claude"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/interactions/chat-completions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/interactions/responses"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/openai/chat-completions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/openai/responses"
)
