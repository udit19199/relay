package cmd

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	authlogin "github.com/udit19199/relay/internal/auth/login"
	"github.com/udit19199/relay/internal/config"
)

// DoDevinLogin triggers the OAuth PKCE or headless token flow for Devin / Cognition and saves credentials.
func DoDevinLogin(cfg *config.Config, options *LoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}

	promptFn := options.Prompt
	if promptFn == nil {
		promptFn = defaultProjectPrompt()
	}

	manager := newAuthManager()
	authOpts := &authlogin.LoginOptions{
		NoBrowser:    options.NoBrowser,
		CallbackPort: options.CallbackPort,
		ProxyURL:     options.ProxyURL,
		Metadata:     map[string]string{},
		Prompt:       promptFn,
	}

	record, savedPath, err := manager.Login(context.Background(), "devin", cfg, authOpts)
	if err != nil {
		log.Errorf("Devin authentication failed: %v", err)
		return
	}

	if savedPath != "" {
		fmt.Printf("Authentication saved to %s\n", savedPath)
	}
	if record != nil && record.Label != "" {
		fmt.Printf("Authenticated as %s\n", record.Label)
	}
	fmt.Println("Devin authentication successful!")
}
