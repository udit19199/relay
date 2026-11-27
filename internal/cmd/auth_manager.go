package cmd

import (
	authlogin "github.com/udit19199/relay/internal/auth/login"
)

// newAuthManager creates a new authentication manager instance with all supported
// authenticators and a file-based token store. It initializes authenticators for
// Codex, Claude, xAI, Devin, and Meta providers.
//
// Returns:
//   - *authlogin.Manager: A configured authentication manager instance
func newAuthManager() *authlogin.Manager {
	store := authlogin.GetTokenStore()
	manager := authlogin.NewManager(store,
		authlogin.NewCodexAuthenticator(),
		authlogin.NewClaudeAuthenticator(),
		authlogin.NewXAIAuthenticator(),
		authlogin.NewDevinAuthenticator(),
		authlogin.NewMetaAuthenticator(),
	)
	return manager
}
