package claude

import "github.com/udit19199/relay/internal/auth/oauthcallback"

// OAuthServer handles the local HTTP server for OAuth callbacks.
type OAuthServer = oauthcallback.Server

// OAuthResult contains the result of the OAuth callback.
type OAuthResult = oauthcallback.Result

// NewOAuthServer creates an OAuth callback server listening on port.
func NewOAuthServer(port int) *OAuthServer {
	return oauthcallback.New(port, oauthcallback.Config{
		CallbackPath:       "/callback",
		DefaultPlatformURL: "https://console.anthropic.com/",
		SuccessHTML:        LoginSuccessHtml,
		SetupNoticeHTML:    SetupNoticeHtml,
	})
}
