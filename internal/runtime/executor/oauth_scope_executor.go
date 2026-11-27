package executor

import coreauth "github.com/udit19199/relay/internal/service/auth"

// ForAPIKey binds configuration once after credential selection. Value receivers
// copy executor configuration while preserving shared transport/session stores.
func (e CodexExecutor) ForAPIKey() coreauth.ProviderExecutor {
	e.cfg = e.cfg.ForAPIKey()
	return &e
}

func (e ClaudeExecutor) ForAPIKey() coreauth.ProviderExecutor {
	e.cfg = e.cfg.ForAPIKey()
	return &e
}

func (e OpenAICompatExecutor) ForAPIKey() coreauth.ProviderExecutor {
	e.cfg = e.cfg.ForAPIKey()
	return &e
}

func (e XAIExecutor) ForAPIKey() coreauth.ProviderExecutor {
	e.cfg = e.cfg.ForAPIKey()
	return &e
}

func (e CodexWebsocketsExecutor) ForAPIKey() coreauth.ProviderExecutor {
	if e.CodexExecutor != nil {
		e.CodexExecutor = e.CodexExecutor.ForAPIKey().(*CodexExecutor)
	}
	return &e
}

func (e XAIWebsocketsExecutor) ForAPIKey() coreauth.ProviderExecutor {
	if e.XAIExecutor != nil {
		e.XAIExecutor = e.XAIExecutor.ForAPIKey().(*XAIExecutor)
	}
	return &e
}

func (e CodexAutoExecutor) ForAPIKey() coreauth.ProviderExecutor {
	if e.httpExec != nil {
		e.httpExec = e.httpExec.ForAPIKey().(*CodexExecutor)
	}
	if e.wsExec != nil {
		e.wsExec = e.wsExec.ForAPIKey().(*CodexWebsocketsExecutor)
	}
	return &e
}

func (e XAIAutoExecutor) ForAPIKey() coreauth.ProviderExecutor {
	if e.httpExec != nil {
		e.httpExec = e.httpExec.ForAPIKey().(*XAIExecutor)
	}
	if e.wsExec != nil {
		e.wsExec = e.wsExec.ForAPIKey().(*XAIWebsocketsExecutor)
	}
	return &e
}
