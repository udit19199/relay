package config

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// ParseConfigBytes parses a YAML configuration payload into Config and applies the same
// in-memory normalizations as LoadConfigOptional, without persisting any changes to disk.
func ParseConfigBytes(data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("config payload is empty")
	}

	if errValidate := validateCredentialWeightYAML(data); errValidate != nil {
		return nil, errValidate
	}

	var cfg Config
	// Keep defaults aligned with LoadConfigOptional.
	cfg.Host = "127.0.0.1" // Default loopback only; set host: 0.0.0.0 explicitly to bind all interfaces
	cfg.LoggingToFile = false
	cfg.LogsMaxTotalSizeMB = 0
	cfg.ErrorLogsMaxFiles = 10
	cfg.DisableCooling = false
	cfg.SaveCooldownStatus = false
	cfg.TransientErrorCooldownSeconds = 0
	cfg.Pprof.Enable = false
	cfg.Pprof.Addr = DefaultPprofAddr

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config payload: %w", err)
	}
	if errValidate := validateTrustedProxies(cfg.TrustedProxies); errValidate != nil {
		return nil, errValidate
	}

	if errValidate := cfg.ValidateCredentialWeights(); errValidate != nil {
		return nil, errValidate
	}

	// Hash remote management key if plaintext is detected (nested), but do NOT persist.
	if cfg.RemoteManagement.SecretKey != "" && !looksLikeBcrypt(cfg.RemoteManagement.SecretKey) {
		hashed, errHash := bcrypt.GenerateFromPassword([]byte(cfg.RemoteManagement.SecretKey), bcrypt.DefaultCost)
		if errHash != nil {
			return nil, fmt.Errorf("hash remote management key: %w", errHash)
		}
		cfg.RemoteManagement.SecretKey = string(hashed)
	}

	cfg.Pprof.Addr = strings.TrimSpace(cfg.Pprof.Addr)
	if cfg.Pprof.Addr == "" {
		cfg.Pprof.Addr = DefaultPprofAddr
	}

	cfg.KVStore.Path = strings.TrimSpace(cfg.KVStore.Path)

	if cfg.LogsMaxTotalSizeMB < 0 {
		cfg.LogsMaxTotalSizeMB = 0
	}

	if cfg.ErrorLogsMaxFiles < 0 {
		cfg.ErrorLogsMaxFiles = 10
	}

	if cfg.MaxRetryCredentials < 0 {
		cfg.MaxRetryCredentials = 0
	}

	cfg.NormalizeClaudeConfig()

	// Apply the same sanitization pipeline.
	cfg.SanitizeCodexKeys()
	cfg.SanitizeXAIKeys()
	cfg.SanitizeCodexHeaderDefaults()
	cfg.SanitizeClaudeHeaderDefaults()
	cfg.SanitizeClaudeKeys()
	cfg.SanitizeOpenAICompatibility()
	cfg.OAuthExcludedModels = NormalizeOAuthExcludedModels(cfg.OAuthExcludedModels)
	cfg.SanitizeOAuthModelAlias()
	cfg.SanitizeOAuthSettings()
	cfg.SanitizeOAuthRequestScopedErrors()
	cfg.SanitizePayloadRules()

	return &cfg, nil
}
