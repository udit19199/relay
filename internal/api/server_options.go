package api

import (
	"context"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/udit19199/relay/internal/api/handlers"
	"github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/logging"
	"github.com/udit19199/relay/internal/service/auth"
)

type serverOptionConfig struct {
	extraMiddleware       []gin.HandlerFunc
	engineConfigurator    func(*gin.Engine)
	routerConfigurator    func(*gin.Engine, *handlers.BaseAPIHandler, *config.Config)
	requestLoggerFactory  func(*config.Config, string) logging.RequestLogger
	localPassword         string
	keepAliveEnabled      bool
	keepAliveTimeout      time.Duration
	keepAliveOnTimeout    func()
	postAuthHook          auth.PostAuthHook
	postAuthPersistHook   auth.PostAuthHook
	configReloadHook      func(context.Context, *config.Config)
	exampleAPIKeySafeMode bool
}

// ServerOption customises HTTP server construction.
type ServerOption func(*serverOptionConfig)

func defaultRequestLoggerFactory(cfg *config.Config, configPath string) logging.RequestLogger {
	configDir := filepath.Dir(configPath)
	logsDir := logging.ResolveLogDirectory(cfg)
	logger := logging.NewFileRequestLogger(cfg.RequestLog, logsDir, configDir, cfg.ErrorLogsMaxFiles)
	return logger
}

func effectiveSDKConfig(cfg *config.Config) *config.SDKConfig {
	if cfg == nil {
		return nil
	}
	sdkCfg := cfg.SDKConfig
	sdkCfg.CodexOrphanDelegationCompatibility = cfg.Codex.OrphanDelegationCompatibility
	sdkCfg.CodexResponseSteering = cfg.Codex.ResponseSteering
	return &sdkCfg
}

// WithLocalManagementPassword stores a runtime-only management password accepted for localhost requests.
func WithLocalManagementPassword(password string) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.localPassword = password
	}
}

// WithKeepAliveEndpoint enables a keep-alive endpoint with the provided timeout and callback.
func WithKeepAliveEndpoint(timeout time.Duration, onTimeout func()) ServerOption {
	return func(cfg *serverOptionConfig) {
		if timeout <= 0 || onTimeout == nil {
			return
		}
		cfg.keepAliveEnabled = true
		cfg.keepAliveTimeout = timeout
		cfg.keepAliveOnTimeout = onTimeout
	}
}

// WithRequestLoggerFactory customises request logger creation.
func WithRequestLoggerFactory(factory func(*config.Config, string) logging.RequestLogger) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.requestLoggerFactory = factory
	}
}

// WithPostAuthHook registers a hook to be called after auth record creation.
func WithPostAuthHook(hook auth.PostAuthHook) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.postAuthHook = hook
	}
}

// WithPostAuthPersistHook registers a hook to be called after auth persistence.
func WithPostAuthPersistHook(hook auth.PostAuthHook) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.postAuthPersistHook = hook
	}
}

// WithConfigReloadHook registers a callback used after management saves config changes.
func WithConfigReloadHook(hook func(context.Context, *config.Config)) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.configReloadHook = hook
	}
}

// WithExampleAPIKeySafeMode blocks proxy API endpoints while template API keys remain configured.
func WithExampleAPIKeySafeMode() ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.exampleAPIKeySafeMode = true
	}
}
