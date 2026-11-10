// Package relaycli is Relay's command line for people and coding agents:
// setup, status, doctor, accounts, and fallback. Every command talks to the
// running proxy through its management API, prints plain text by default, and
// prints one JSON object with --json so an agent can read the result.
package relaycli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultPort  = 8317
	serviceLabel = "com.relay.proxy"
)

// Run handles a Relay subcommand. handled is false when args do not start with
// one, so the caller falls through to the normal server flags.
func Run(args []string) (code int, handled bool) {
	if len(args) == 0 {
		return 0, false
	}
	commands := map[string]func([]string) int{
		"setup":    runSetup,
		"status":   runStatus,
		"doctor":   runDoctor,
		"accounts": runAccounts,
		"fallback": runFallback,
		"help":     runHelp,
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return 0, false
	}
	return cmd(args[1:]), true
}

func runHelp([]string) int {
	fmt.Print(`Relay keeps your coding agent working when an AI plan hits its limit.

Commands:
  relay setup [--port N] [--no-service] [--reset-management-key]
                                    create ~/.relay, keys and the login service, then start Relay
  relay status                      accounts, limits left, and fallback order
  relay doctor                      check the install and print a fix for each problem
  relay accounts add <provider> [--no-browser]
                                    sign in (claude, codex, kimi, xai, devin, meta), or add
                                    opencode-go from OPENCODE_API_KEY or --key-stdin
  relay accounts remove <name>      delete a credential
  relay fallback                    show fallback chains
  relay fallback set <model> <provider>[:<model>] ...
                                    serve <model> from the first provider that has capacity
  relay fallback clear <model>      remove a fallback chain

Add --json to any command for JSON output: one object per line. "accounts add"
prints a sign_in_link line first and the result line last.
RELAY_HOME moves ~/.relay elsewhere; such installs never use the login service.
Run "relay --config <file>" to start the proxy in the foreground.
Stop the login service with: launchctl bootout gui/$(id -u)/com.relay.proxy
`)
	return 0
}

// paths holds Relay's files. RELAY_HOME overrides ~/.relay.
type paths struct {
	home, config, managementKey, auths, log, plist string
}

func relayPaths() paths {
	home := os.Getenv("RELAY_HOME")
	userHome, _ := os.UserHomeDir()
	if home == "" {
		home = filepath.Join(userHome, ".relay")
	}
	return paths{
		home:          home,
		config:        filepath.Join(home, "config.yaml"),
		managementKey: filepath.Join(home, "management.key"),
		auths:         filepath.Join(home, "auths"),
		log:           filepath.Join(home, "server.log"),
		plist:         filepath.Join(userHome, "Library", "LaunchAgents", serviceLabel+".plist"),
	}
}

// relayConfig reads only the fields the CLI needs. Both the v8 layout and the
// legacy spellings are accepted.
type relayConfig struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"server"`
	Host   string `yaml:"host"`
	Port   int    `yaml:"port"`
	Access struct {
		APIKeys []string `yaml:"api-keys"`
	} `yaml:"access"`
	Management struct {
		SecretKey string `yaml:"secret-key"`
	} `yaml:"management"`
	RemoteManagement struct {
		SecretKey string `yaml:"secret-key"`
	} `yaml:"remote-management"`
}

func loadConfig(path string) (*relayConfig, error) {
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		return nil, errRead
	}
	var cfg relayConfig
	if errParse := yaml.Unmarshal(data, &cfg); errParse != nil {
		return nil, fmt.Errorf("parse %s: %w", path, errParse)
	}
	return &cfg, nil
}

func (c *relayConfig) port() int {
	if c.Server.Port != 0 {
		return c.Server.Port
	}
	if c.Port != 0 {
		return c.Port
	}
	return defaultPort
}

func (c *relayConfig) host() string {
	if c.Server.Host != "" {
		return c.Server.Host
	}
	return c.Host
}

func (c *relayConfig) clientKey() string {
	if len(c.Access.APIKeys) > 0 {
		return c.Access.APIKeys[0]
	}
	return ""
}

func (c *relayConfig) secretKey() string {
	if c.Management.SecretKey != "" {
		return c.Management.SecretKey
	}
	return c.RemoteManagement.SecretKey
}

func baseURL(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

// client talks to a running Relay with the management key.
type client struct {
	base, managementKey, clientKey string
	http                           *http.Client
}

// newClient reads the config and the management key file. Commands that need
// a running Relay call it first.
func newClient() (*client, error) {
	p := relayPaths()
	cfg, errCfg := loadConfig(p.config)
	if errCfg != nil {
		return nil, fmt.Errorf("Relay is not set up (%v). Run: relay setup", errCfg)
	}
	key, errKey := os.ReadFile(p.managementKey)
	if errKey != nil {
		return nil, fmt.Errorf("management key missing at %s. Run: relay doctor", p.managementKey)
	}
	return &client{
		base:          baseURL(cfg.port()),
		managementKey: strings.TrimSpace(string(key)),
		clientKey:     cfg.clientKey(),
		http:          &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// do sends a management request and decodes a JSON reply into out (if not nil).
func (c *client) do(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			return errMarshal
		}
		reader = bytes.NewReader(data)
	}
	req, errReq := http.NewRequest(method, c.base+path, reader)
	if errReq != nil {
		return errReq
	}
	req.Header.Set("Authorization", "Bearer "+c.managementKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, errDo := c.http.Do(req)
	if errDo != nil {
		return fmt.Errorf("Relay is not answering at %s. Run: relay doctor", c.base)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return &httpError{status: resp.StatusCode, body: strings.TrimSpace(string(data))}
	}
	if out == nil {
		return nil
	}
	if errDecode := json.Unmarshal(data, out); errDecode != nil {
		return fmt.Errorf("unexpected reply from %s: %w", path, errDecode)
	}
	return nil
}

type httpError struct {
	status int
	body   string
}

func (e *httpError) Error() string {
	var reply struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(e.body), &reply) == nil && reply.Error != "" {
		return fmt.Sprintf("Relay answered %d: %s", e.status, reply.Error)
	}
	return fmt.Sprintf("Relay answered %d", e.status)
}

// credential is one entry from GET /v8/management/credentials.
type credential struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	Type          string `json:"type"`
	AuthIndex     string `json:"auth_index"`
	Email         string `json:"email"`
	Label         string `json:"label"`
	Status        string `json:"status"`
	StatusMessage string `json:"status_message"`
	Disabled      bool   `json:"disabled"`
	Unavailable   bool   `json:"unavailable"`
	Priority      int    `json:"priority"`
	Quota         struct {
		Signals map[string]any `json:"signals"`
	} `json:"quota"`
}

func (c credential) provider() string {
	if c.Provider != "" {
		return strings.ToLower(c.Provider)
	}
	return strings.ToLower(c.Type)
}

func (c *client) credentials() ([]credential, error) {
	var reply struct {
		Files []credential `json:"files"`
	}
	if errDo := c.do(http.MethodGet, "/v8/management/credentials", nil, &reply); errDo != nil {
		return nil, errDo
	}
	return reply.Files, nil
}

// output prints value as JSON when asJSON is set, otherwise calls text.
func output(asJSON bool, value any, text func()) {
	if asJSON {
		// One object per line, so a command that reports progress (accounts
		// add) can be read line by line.
		_ = json.NewEncoder(os.Stdout).Encode(value)
		return
	}
	text()
}

// fail reports an error in the requested format and returns exit code 1.
func fail(asJSON bool, err error) int {
	if asJSON {
		output(true, map[string]any{"ok": false, "error": err.Error()}, nil)
	} else {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	return 1
}

// hasFlag removes a boolean flag from args and reports whether it was present.
func hasFlag(args *[]string, name string) bool {
	found := false
	kept := (*args)[:0]
	for _, arg := range *args {
		if arg == "--"+name || arg == "-"+name {
			found = true
			continue
		}
		kept = append(kept, arg)
	}
	*args = kept
	return found
}
