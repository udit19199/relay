package cliproxy

import (
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
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
