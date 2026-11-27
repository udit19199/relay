package util

import "github.com/udit19199/relay/internal/githubauth"

// ResolveGitHubToken returns the GitHub API token in priority order:
// 1. server.github-token
// 2. GITHUB_TOKEN
// 3. github_token
func ResolveGitHubToken() string {
	return githubauth.ResolveToken()
}
