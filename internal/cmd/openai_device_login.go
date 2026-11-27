package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/udit19199/relay/internal/auth/codex"
	authlogin "github.com/udit19199/relay/internal/auth/login"
	"github.com/udit19199/relay/internal/config"
)

const (
	codexLoginModeMetadataKey = "codex_login_mode"
	codexLoginModeDevice      = "device"
)

// DoCodexDeviceLogin triggers the Codex device-code flow while keeping the
// existing codex-login OAuth callback flow intact.
func DoCodexDeviceLogin(cfg *config.Config, options *LoginOptions) {
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
		Metadata: map[string]string{
			codexLoginModeMetadataKey: codexLoginModeDevice,
		},
		Prompt: promptFn,
	}

	_, savedPath, err := manager.Login(context.Background(), "codex", cfg, authOpts)
	if err != nil {
		if authErr, ok := errors.AsType[*codex.AuthenticationError](err); ok {
			log.Error(codex.GetUserFriendlyMessage(authErr))
			if authErr.Type == codex.ErrPortInUse.Type {
				os.Exit(codex.ErrPortInUse.Code)
			}
			return
		}
		fmt.Printf("Codex device authentication failed: %v\n", err)
		return
	}

	if savedPath != "" {
		fmt.Printf("Authentication saved to %s\n", savedPath)
	}
	fmt.Println("Codex device authentication successful!")
}
