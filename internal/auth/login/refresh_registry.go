package login

import (
	"time"

	coreauth "github.com/udit19199/relay/internal/service/auth"
)

func init() {
	registerRefreshLead("codex", func() Authenticator { return NewCodexAuthenticator() })
	registerRefreshLead("claude", func() Authenticator { return NewClaudeAuthenticator() })
	registerRefreshLead("xai", func() Authenticator { return NewXAIAuthenticator() })
	registerRefreshLead("devin", func() Authenticator { return NewDevinAuthenticator() })
	registerRefreshLead("meta", func() Authenticator { return NewMetaAuthenticator() })
}

func registerRefreshLead(provider string, factory func() Authenticator) {
	coreauth.RegisterRefreshLeadProvider(provider, func() *time.Duration {
		if factory == nil {
			return nil
		}
		auth := factory()
		if auth == nil {
			return nil
		}
		return auth.RefreshLead()
	})
}
