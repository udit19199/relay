// Package cliproxy provides the core service implementation for the CLI Proxy API.
// It includes service lifecycle management, authentication handling, file watching,
// and integration with various AI service providers through a unified interface.
package service

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/udit19199/relay/internal/access"
	configaccess "github.com/udit19199/relay/internal/access/config_access"
	"github.com/udit19199/relay/internal/api"
	authlogin "github.com/udit19199/relay/internal/auth/login"
	"github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/runtime/executor/helps"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	"github.com/udit19199/relay/internal/watcher"
)

// Builder constructs a Service instance with customizable providers.
// It provides a fluent interface for configuring all aspects of the service
// including authentication, file watching, HTTP server options, and lifecycle hooks.
type Builder struct {
	// cfg holds the application configuration.
	cfg *config.Config

	// configPath is the path to the configuration file.
	configPath string

	// tokenProvider handles loading token-based clients.
	tokenProvider TokenClientProvider

	// apiKeyProvider handles loading API key-based clients.
	apiKeyProvider APIKeyClientProvider

	// watcherFactory creates file watcher instances.
	watcherFactory WatcherFactory

	// hooks provides lifecycle callbacks.
	hooks Hooks

	// authManager handles legacy authentication operations.
	authManager *authlogin.Manager

	// accessManager handles request authentication providers.
	accessManager *access.Manager

	// coreManager handles core authentication and execution.
	coreManager *coreauth.Manager

	// cooldownStateStore overrides runtime cooldown persistence.
	cooldownStateStore coreauth.CooldownStateStore

	// postAuthHook is called after auth record creation and before persistence.
	postAuthHook coreauth.PostAuthHook

	// resultPolicy intercepts execution results before quota mutation and persistence.
	resultPolicy coreauth.ResultPolicy

	// serverOptions contains additional server configuration options.
	serverOptions []api.ServerOption
}

// Hooks allows callers to plug into service lifecycle stages.
// These callbacks provide opportunities to perform custom initialization
// and cleanup operations during service startup and shutdown.
type Hooks struct {
	// OnBeforeStart is called before the service starts, allowing configuration
	// modifications or additional setup.
	OnBeforeStart func(*config.Config)

	// OnAfterStart is called after the service has started successfully,
	// providing access to the service instance for additional operations.
	OnAfterStart func(*Service)
}

// NewBuilder creates a Builder with default dependencies left unset.
// Use the fluent interface methods to configure the service before calling Build().
//
// Returns:
//   - *Builder: A new builder instance ready for configuration
func NewBuilder() *Builder {
	return &Builder{}
}

// WithConfig sets the configuration instance used by the service.
//
// Parameters:
//   - cfg: The application configuration
//
// Returns:
//   - *Builder: The builder instance for method chaining
func (b *Builder) WithConfig(cfg *config.Config) *Builder {
	b.cfg = cfg
	return b
}

// WithConfigPath sets the absolute configuration file path used for reload watching.
//
// Parameters:
//   - path: The absolute path to the configuration file
//
// Returns:
//   - *Builder: The builder instance for method chaining
func (b *Builder) WithConfigPath(path string) *Builder {
	b.configPath = path
	return b
}

// WithWatcherFactory allows customizing the watcher factory that handles reloads.
func (b *Builder) WithWatcherFactory(factory WatcherFactory) *Builder {
	b.watcherFactory = factory
	return b
}

// WithCoreAuthManager overrides the runtime auth manager responsible for request execution.
func (b *Builder) WithCoreAuthManager(mgr *coreauth.Manager) *Builder {
	b.coreManager = mgr
	return b
}

// WithServerOptions appends server configuration options used during construction.
func (b *Builder) WithServerOptions(opts ...api.ServerOption) *Builder {
	b.serverOptions = append(b.serverOptions, opts...)
	return b
}

// WithLocalManagementPassword configures a password that is only accepted from localhost management requests.
func (b *Builder) WithLocalManagementPassword(password string) *Builder {
	if password == "" {
		return b
	}
	b.serverOptions = append(b.serverOptions, api.WithLocalManagementPassword(password))
	return b
}

// WithResultPolicy sets an execution result policy invoked before in-memory quota mutations and persistence.
func (b *Builder) WithResultPolicy(policy coreauth.ResultPolicy) *Builder {
	b.resultPolicy = policy
	return b
}

// Build validates inputs, applies defaults, and returns a ready-to-run service.
func (b *Builder) Build() (*Service, error) {
	if b.cfg == nil {
		return nil, fmt.Errorf("cliproxy: configuration is required")
	}
	if b.configPath == "" {
		return nil, fmt.Errorf("cliproxy: configuration path is required")
	}
	tokenProvider := b.tokenProvider
	if tokenProvider == nil {
		tokenProvider = NewFileTokenClientProvider()
	}

	apiKeyProvider := b.apiKeyProvider
	if apiKeyProvider == nil {
		apiKeyProvider = NewAPIKeyClientProvider()
	}

	watcherFactory := b.watcherFactory
	if watcherFactory == nil {
		watcherFactory = defaultWatcherFactory
	}

	authManager := b.authManager
	if authManager == nil {
		authManager = newDefaultAuthManager()
	}

	accessManager := b.accessManager
	if accessManager == nil {
		accessManager = access.NewManager()
	}

	configaccess.Register(&b.cfg.SDKConfig)
	accessManager.SetProviders(access.RegisteredProviders())
	if len(access.RegisteredProviders()) == 0 {
		log.Warn("no API keys configured: the inference API will reject every request until at least one key is set")
	}

	coreManager := b.coreManager
	cooldownStateStore := b.cooldownStateStore
	var appliedRoutingState *routingRuntimeState
	if coreManager == nil {
		tokenStore := authlogin.GetTokenStore()
		if dirSetter, ok := tokenStore.(interface{ SetBaseDir(string) }); ok && b.cfg != nil {
			dirSetter.SetBaseDir(b.cfg.AuthDir)
		}
		if cooldownStateStore == nil {
			if provider, ok := tokenStore.(coreauth.CooldownStateStoreProvider); ok {
				cooldownStateStore = provider.CooldownStateStore()
			}
		}

		routingState := normalizedRoutingRuntimeState(b.cfg)
		coreManager = coreauth.NewManager(tokenStore, newRoutingSelector(routingState), nil)
		appliedRoutingState = &routingState
	}
	// Attach a default RoundTripper provider so providers can opt-in per-auth transports.
	coreManager.SetRoundTripperProvider(newDefaultRoundTripperProvider())
	coreManager.SetConfig(b.cfg)
	coreManager.SetOAuthModelAlias(b.cfg.OAuthModelAlias)
	if b.resultPolicy != nil {
		coreManager.SetResultPolicy(b.resultPolicy)
	}

	service := &Service{
		cfg:                 b.cfg.CloneForRuntime(),
		configPath:          b.configPath,
		tokenProvider:       tokenProvider,
		apiKeyProvider:      apiKeyProvider,
		watcherFactory:      watcherFactory,
		hooks:               b.hooks,
		authManager:         authManager,
		accessManager:       accessManager,
		coreManager:         coreManager,
		cooldownStateStore:  cooldownStateStore,
		appliedRoutingState: appliedRoutingState,
		serverOptions:       append([]api.ServerOption(nil), b.serverOptions...),
	}
	service.proxyPoolPublisher = helps.RegisterProxyPoolPublisher()
	helps.AdvanceProxyPoolGeneration(service.proxyPoolPublisher, service.cfg)
	if b.postAuthHook != nil {
		service.serverOptions = append(service.serverOptions, api.WithPostAuthHook(b.postAuthHook))
	}
	service.serverOptions = append(service.serverOptions,
		api.WithPostAuthPersistHook(service.runtimeAuthSyncHook()),
		api.WithConfigReloadHook(func(_ context.Context, _ *config.Config) {
			service.reloadConfigFromWatcher()
		}),
	)
	return service, nil
}

func (s *Service) runtimeAuthSyncHook() coreauth.PostAuthHook {
	return func(ctx context.Context, auth *coreauth.Auth) error {
		if s == nil || auth == nil || auth.ID == "" {
			return nil
		}
		action := watcher.AuthUpdateActionAdd
		if s.coreManager != nil {
			if _, ok := s.coreManager.GetByID(auth.ID); ok {
				action = watcher.AuthUpdateActionModify
			}
		}
		update := watcher.AuthUpdate{
			Action: action,
			ID:     auth.ID,
			Auth:   auth,
		}
		if watcher := s.currentWatcher(); watcher != nil {
			_, rev := watcher.DispatchPersistedAuthUpdateWithRevision(&update)
			if rev > 0 {
				update.SetRevision(rev)
			}
		}
		// Detach from request cancellation so runtime model registration always completes
		// once the credential has been persisted to disk. If the watcher consumer already
		// claimed this revision, handleAuthUpdate waits for that registration to finish.
		syncCtx := coreauth.WithSkipPersist(context.Background())
		s.handleAuthUpdate(syncCtx, update)
		return nil
	}
}
