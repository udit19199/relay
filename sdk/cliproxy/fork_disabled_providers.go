package cliproxy

import (
	"strings"
	"sync"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// forkDisabledProviders lists providers stripped in this fork.
// This fork keeps Claude/Codex/Kimi/xAI/Devin/Meta and drops all
// Google families (Gemini, Vertex, AI Studio) plus Antigravity.
var forkDisabledProviders = map[string]bool{
	"antigravity":         true,
	"gemini":              true,
	"gemini-interactions": true,
	"gemini-cli":          true,
	"vertex":              true,
	"aistudio":            true,
}

// isForkDisabledProvider reports whether a provider key is stripped in this fork.
func isForkDisabledProvider(provider string) bool {
	return forkDisabledProviders[strings.ToLower(strings.TrimSpace(provider))]
}

// isForkDisabledAuth reports whether an auth entry belongs to a stripped provider.
func isForkDisabledAuth(auth *coreauth.Auth) bool {
	if auth == nil {
		return false
	}
	if isForkDisabledProvider(auth.Provider) {
		return true
	}
	if auth.Attributes != nil {
		if isForkDisabledProvider(auth.Attributes["provider_key"]) {
			return true
		}
	}
	return false
}

// forkDisabledLogged remembers which ignored credentials were already logged,
// so registration refreshes do not repeat the warning.
var forkDisabledLogged sync.Map

// dropForkDisabledAuth logs an ignored credential once and unregisters its
// executor only when the credential's own provider is stripped. A match via
// provider_key (for example an openai-compatibility entry named "gemini")
// must not remove the shared executor that other credentials use.
func (s *Service) dropForkDisabledAuth(auth *coreauth.Auth) {
	// Only credential files are worth a warning; startup also passes one
	// placeholder entry per provider, which has no file.
	if auth.FileName != "" {
		if _, seen := forkDisabledLogged.LoadOrStore(auth.ID, struct{}{}); !seen {
			log.Warnf("ignoring %s credential %s: this provider is turned off in Relay", auth.Provider, auth.FileName)
		}
	}
	if isForkDisabledProvider(auth.Provider) {
		s.coreManager.UnregisterExecutor(strings.ToLower(strings.TrimSpace(auth.Provider)))
	}
}
