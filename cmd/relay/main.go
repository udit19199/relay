// Package main provides the entry point for the CLI Proxy API server.
// This server acts as a proxy that provides OpenAI/Claude/Codex compatible API interfaces
// for CLI models, allowing CLI models to be used with tools and libraries designed for standard AI APIs.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	configaccess "github.com/udit19199/relay/internal/access/config_access"
	"github.com/udit19199/relay/internal/api"
	authlogin "github.com/udit19199/relay/internal/auth/login"
	"github.com/udit19199/relay/internal/buildinfo"
	"github.com/udit19199/relay/internal/cmd"
	"github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/githubauth"
	"github.com/udit19199/relay/internal/logging"
	"github.com/udit19199/relay/internal/registry"
	"github.com/udit19199/relay/internal/relaycli"
	"github.com/udit19199/relay/internal/safemode"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	_ "github.com/udit19199/relay/internal/translator"
	"github.com/udit19199/relay/internal/util"
)

var (
	Version           = "dev"
	Commit            = "none"
	BuildDate         = "unknown"
	DefaultConfigPath = ""
)

// init initializes the shared logger setup.
func init() {
	logging.SetupBaseLogger()
	buildinfo.Version = Version
	buildinfo.Commit = Commit
	buildinfo.BuildDate = BuildDate
}

func shouldEnableExampleAPIKeySafeMode(cfg *config.Config, commandMode, cloudConfigMissing bool) bool {
	if cfg == nil || commandMode || cloudConfigMissing {
		return false
	}
	return safemode.HasExampleAPIKeys(cfg.APIKeys)
}

// main is the entry point of the application.
// It parses command-line flags, loads configuration, and starts the appropriate
// service based on the provided flags (login, codex-login, or server mode).
func main() {
	// Relay's own commands (setup, status, doctor, accounts, fallback) run and exit.
	if code, handled := relaycli.Run(os.Args[1:]); handled {
		os.Exit(code)
	}
	fmt.Printf("Relay Version: %s, Commit: %s, BuiltAt: %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)

	// Command-line flags to control the application's behavior.
	var codexLogin bool
	var codexDeviceLogin bool
	var claudeLogin bool
	var noBrowser bool
	var oauthCallbackPort int
	var loginProxyURL string
	var xaiLogin bool
	var devinLogin bool
	var siwcBind string
	var configPath string
	var password string
	var localModel bool

	// Define command-line flags for different operation modes.
	flag.BoolVar(&codexLogin, "codex-login", false, "Login to Codex using OAuth")
	flag.BoolVar(&codexDeviceLogin, "codex-device-login", false, "Login to Codex using device code flow")
	flag.BoolVar(&claudeLogin, "claude-login", false, "Login to Claude using OAuth")
	flag.BoolVar(&noBrowser, "no-browser", false, "Don't open browser automatically for OAuth")
	flag.IntVar(&oauthCallbackPort, "oauth-callback-port", 0, "Override OAuth callback port (defaults to provider-specific port)")
	flag.StringVar(&loginProxyURL, "login-proxy", "", "Proxy URL used for the OAuth login flow and saved as the new credential's proxy_url (http, https, socks5, socks5h)")
	flag.BoolVar(&xaiLogin, "xai-login", false, "Login to xAI using OAuth")
	flag.BoolVar(&devinLogin, "devin-login", false, "Login to Devin using OAuth")
	flag.StringVar(&configPath, "config", DefaultConfigPath, "Configure File Path")
	flag.StringVar(&siwcBind, "siwc-bind", "", "Bind Sign in with ChatGPT (SIWC) credentials to a Codex credential file")
	flag.StringVar(&password, "password", "", "")
	flag.BoolVar(&localModel, "local-model", false, "Use embedded model catalogs unless models.catalog, models.codex-catalog, or models.devin-catalog explicitly overrides the source")

	flag.CommandLine.Usage = func() {
		out := flag.CommandLine.Output()
		_, _ = fmt.Fprintf(out, "Usage of %s\n", os.Args[0])
		flag.CommandLine.VisitAll(func(f *flag.Flag) {
			if f.Name == "password" {
				return
			}
			s := fmt.Sprintf("  -%s", f.Name)
			name, unquoteUsage := flag.UnquoteUsage(f)
			if name != "" {
				s += " " + name
			}
			if len(s) <= 4 {
				s += "	"
			} else {
				s += "\n    "
			}
			if unquoteUsage != "" {
				s += unquoteUsage
			}
			if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
				s += fmt.Sprintf(" (default %s)", f.DefValue)
			}
			_, _ = fmt.Fprint(out, s+"\n")
		})
	}

	// Parse the command-line flags.
	flag.Parse()

	// Core application variables.
	var err error
	var cfg *config.Config
	var isCloudDeploy bool

	wd, err := os.Getwd()
	if err != nil {
		log.Errorf("failed to get working directory: %v", err)
		return
	}

	// Load environment variables from .env if present.
	if errLoad := godotenv.Load(filepath.Join(wd, ".env")); errLoad != nil {
		if !errors.Is(errLoad, os.ErrNotExist) {
			log.WithError(errLoad).Warn("failed to load .env file")
		}
	}

	// Check for cloud deploy mode only on first execution
	// Read env var name in uppercase: DEPLOY
	deployEnv := os.Getenv("DEPLOY")
	if deployEnv == "cloud" {
		isCloudDeploy = true
	}

	// Determine and load the configuration file.
	var configFilePath string
	if configPath != "" {
		configFilePath = configPath
		cfg, err = config.LoadConfigOptional(configPath, isCloudDeploy)
	} else {
		wd, err = os.Getwd()
		if err != nil {
			log.Errorf("failed to get working directory: %v", err)
			return
		}
		configFilePath = filepath.Join(wd, "config.yaml")
		cfg, err = config.LoadConfigOptional(configFilePath, isCloudDeploy)
	}
	if err != nil {
		log.Errorf("failed to load config: %v", err)
		return
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	// In cloud deploy mode, check if we have a valid configuration
	var configFileExists bool
	if isCloudDeploy {
		if info, errStat := os.Stat(configFilePath); errStat != nil {
			// Don't mislead: API server will not start until configuration is provided.
			log.Info("Cloud deploy mode: No configuration file detected; standing by for configuration")
			configFileExists = false
		} else if info.IsDir() {
			log.Info("Cloud deploy mode: Config path is a directory; standing by for configuration")
			configFileExists = false
		} else if cfg.Port == 0 {
			// LoadConfigOptional returns empty config when file is empty or invalid.
			// Config file exists but is empty or invalid; treat as missing config
			log.Info("Cloud deploy mode: Configuration file is empty or invalid; standing by for valid configuration")
			configFileExists = false
		} else {
			log.Info("Cloud deploy mode: Configuration file detected; starting service")
			configFileExists = true
		}
	}
	coreauth.SetQuotaCooldownDisabled(cfg.DisableCooling)
	coreauth.SetTransientErrorCooldownSeconds(cfg.TransientErrorCooldownSeconds)
	coreauth.SetCredentialQuotaProbeIntervalSeconds(cfg.QuotaProbeIntervalSeconds)

	if err = logging.ConfigureLogOutput(cfg); err != nil {
		log.Errorf("failed to configure log output: %v", err)
		return
	}

	log.Infof("Relay Version: %s, Commit: %s, BuiltAt: %s", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)

	// Set the log level based on the configuration.
	util.SetLogLevel(cfg)

	if resolvedAuthDir, errResolveAuthDir := util.ResolveAuthDir(cfg.AuthDir); errResolveAuthDir != nil {
		log.Errorf("failed to resolve auth directory: %v", errResolveAuthDir)
		return
	} else {
		cfg.AuthDir = resolvedAuthDir
	}
	githubauth.SetToken(cfg.GitHubToken)

	// Create login options to be used in authentication flows.
	options := &cmd.LoginOptions{
		NoBrowser:    noBrowser,
		CallbackPort: oauthCallbackPort,
		ProxyURL:     loginProxyURL,
	}

	commandMode := siwcBind != "" || codexLogin || codexDeviceLogin || claudeLogin || xaiLogin || devinLogin
	cloudConfigMissing := isCloudDeploy && !configFileExists
	exampleAPIKeySafeMode := shouldEnableExampleAPIKeySafeMode(cfg, commandMode, cloudConfigMissing)
	serverOptions := []api.ServerOption(nil)
	if exampleAPIKeySafeMode {
		matches := safemode.ExampleAPIKeys(cfg.APIKeys)
		log.WithField("api_keys", strings.Join(matches, ",")).Error("unsafe example API key configured; proxy API endpoints disabled until api-keys is updated")
		serverOptions = append(serverOptions, api.WithExampleAPIKeySafeMode())
	}

	// Register the shared token store once so all components use the same persistence backend.
	authlogin.RegisterTokenStore(authlogin.NewFileTokenStore())

	// Register built-in access providers before constructing services.
	configaccess.Register(&cfg.SDKConfig)
	// Handle different command modes based on the provided flags.

	if siwcBind != "" {
		// Handle Sign in with ChatGPT (SIWC) credential binding
		cmd.DoSIWCBind(cfg, siwcBind, options)
	} else if codexLogin {
		// Handle Codex login
		cmd.DoCodexLogin(cfg, options)
	} else if codexDeviceLogin {
		// Handle Codex device-code login
		cmd.DoCodexDeviceLogin(cfg, options)
	} else if claudeLogin {
		// Handle Claude login
		cmd.DoClaudeLogin(cfg, options)
	} else if xaiLogin {
		cmd.DoXAILogin(cfg, options)
	} else if devinLogin {
		cmd.DoDevinLogin(cfg, options)
	} else {
		// In cloud deploy mode without config file, just wait for shutdown signals
		if isCloudDeploy && !configFileExists {
			// No config file available, just wait for shutdown
			cmd.WaitForCloudDeploy()
			return
		}
		if localModel {
			log.Info("Local model mode: using embedded catalogs unless an explicit catalog source is configured")
		}
		// Start the main proxy service
		registry.SetLocalModelCatalogs(localModel)
		cmd.StartServiceWithOptions(cfg, configFilePath, password, serverOptions...)
	}
}
