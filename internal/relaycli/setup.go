package relaycli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type setupResult struct {
	OK              bool     `json:"ok"`
	BaseURL         string   `json:"base_url"`
	ClientKey       string   `json:"client_key"`
	Config          string   `json:"config"`
	CreatedConfig   bool     `json:"created_config"`
	Service         string   `json:"service"`
	Running         bool     `json:"running"`
	Accounts        int      `json:"accounts"`
	ClaudeCodeEnv   []string `json:"claude_code_env"`
	OpenAIEnv       []string `json:"openai_env"`
	NextSteps       []string `json:"next_steps"`
	ServiceDetail   string   `json:"service_detail,omitempty"`
	ManagementKey   string   `json:"management_key_file"`
	ResetManagement bool     `json:"reset_management_key,omitempty"`
}

// runSetup creates ~/.relay with a config and both keys when missing, installs
// the login service, starts Relay, and waits until it answers. Running it again
// keeps the existing config and keys.
func runSetup(args []string) int {
	asJSON := hasFlag(&args, "json")
	noService := hasFlag(&args, "no-service")
	resetKey := hasFlag(&args, "reset-management-key")
	port := defaultPort
	for i := 0; i < len(args); i++ {
		if args[i] == "--port" && i+1 < len(args) {
			parsed, errAtoi := strconv.Atoi(args[i+1])
			if errAtoi != nil {
				return fail(asJSON, fmt.Errorf("invalid --port %q", args[i+1]))
			}
			port = parsed
			i++
		}
	}

	p := relayPaths()
	res := setupResult{Config: p.config, ManagementKey: p.managementKey}
	if errDir := os.MkdirAll(p.auths, 0o700); errDir != nil {
		return fail(asJSON, errDir)
	}

	if _, errStat := os.Stat(p.config); errors.Is(errStat, os.ErrNotExist) {
		if errCreate := createConfig(p, port); errCreate != nil {
			return fail(asJSON, errCreate)
		}
		res.CreatedConfig = true
	} else if resetKey {
		if errReset := resetManagementKey(p); errReset != nil {
			return fail(asJSON, errReset)
		}
		res.ResetManagement = true
	} else if _, errKey := os.Stat(p.managementKey); errKey != nil {
		return fail(asJSON, fmt.Errorf("%s is missing and Relay keeps only a hash of the key. Run: relay setup --reset-management-key", p.managementKey))
	}

	cfg, errCfg := loadConfig(p.config)
	if errCfg != nil {
		return fail(asJSON, errCfg)
	}
	res.BaseURL = baseURL(cfg.port())
	res.ClientKey = cfg.clientKey()

	// The login service has one fixed label, so only the default ~/.relay
	// install may own it. A RELAY_HOME install (tests, sandboxes) runs without it.
	if os.Getenv("RELAY_HOME") != "" {
		noService = true
	}
	if noService {
		res.Service = "skipped"
		res.Running = waitHealthy(res.BaseURL, res.ClientKey, 0)
	} else {
		detail, errService := installService(p, res.ResetManagement)
		if errService != nil {
			return fail(asJSON, errService)
		}
		res.Service = serviceLabel
		res.ServiceDetail = detail
		res.Running = waitHealthy(res.BaseURL, res.ClientKey, 20*time.Second)
	}

	if res.Running {
		if c, errClient := newClient(); errClient == nil {
			if creds, errList := c.credentials(); errList == nil {
				res.Accounts = len(creds)
			}
		}
	}
	res.OK = res.Running || noService
	res.ClaudeCodeEnv = []string{"ANTHROPIC_BASE_URL=" + res.BaseURL, "ANTHROPIC_AUTH_TOKEN=" + res.ClientKey}
	res.OpenAIEnv = []string{"OPENAI_BASE_URL=" + res.BaseURL + "/v1", "OPENAI_API_KEY=" + res.ClientKey}
	if res.Accounts == 0 {
		res.NextSteps = append(res.NextSteps, "relay accounts add claude", "relay accounts add codex", "OPENCODE_API_KEY=... relay accounts add opencode-go")
	}
	if !res.Running && !noService {
		res.NextSteps = append(res.NextSteps, "relay doctor")
	}
	if !res.Running && noService {
		res.NextSteps = append([]string{"relay --config " + p.config + "   (runs Relay in the foreground)"}, res.NextSteps...)
	}

	output(asJSON, res, func() {
		if res.CreatedConfig {
			fmt.Println("Created", p.config)
		} else {
			fmt.Println("Kept existing", p.config)
		}
		if res.ResetManagement {
			fmt.Println("New management key written to", p.managementKey)
		}
		if res.Running {
			fmt.Println("Relay is running at", res.BaseURL)
		} else if noService {
			fmt.Println("Login service skipped. Start Relay with: relay --config", p.config)
			fmt.Println("(set server.port in the config first if 8317 is in use)")
		} else {
			fmt.Println("Relay did not answer within 20s. Run: relay doctor")
		}
		fmt.Println("\nClaude Code:")
		for _, line := range res.ClaudeCodeEnv {
			fmt.Println("  export " + line)
		}
		fmt.Println("OpenAI-compatible tools:")
		for _, line := range res.OpenAIEnv {
			fmt.Println("  export " + line)
		}
		if len(res.NextSteps) > 0 {
			fmt.Println("\nNext:")
			for _, step := range res.NextSteps {
				fmt.Println("  " + step)
			}
		}
	})
	if !res.OK {
		return 1
	}
	return 0
}

func randomKey() (string, error) {
	buf := make([]byte, 24)
	if _, errRand := rand.Read(buf); errRand != nil {
		return "", errRand
	}
	return "relay-" + hex.EncodeToString(buf), nil
}

// createConfig writes a minimal config and the management key file. The key
// file is written first: Relay replaces the plain key in the config with a hash
// on start, so the file is the only readable copy.
func createConfig(p paths, port int) error {
	clientKey, errClient := randomKey()
	if errClient != nil {
		return errClient
	}
	managementKey, errMgmt := randomKey()
	if errMgmt != nil {
		return errMgmt
	}
	if errWrite := os.WriteFile(p.managementKey, []byte(managementKey+"\n"), 0o600); errWrite != nil {
		return errWrite
	}
	config := fmt.Sprintf(`config-version: 8
server:
  host: "127.0.0.1"
  port: %d
access:
  api-keys:
    - %q
management:
  secret-key: %q
oauth:
  auth-dir: %q
`, port, clientKey, managementKey, p.auths)
	return os.WriteFile(p.config, []byte(config), 0o600)
}

// resetManagementKey writes a new key file and puts the same plain key in the
// config, which Relay hashes again on its next start.
func resetManagementKey(p paths) error {
	data, errRead := os.ReadFile(p.config)
	if errRead != nil {
		return errRead
	}
	key, errKey := randomKey()
	if errKey != nil {
		return errKey
	}
	lines := strings.Split(string(data), "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "secret-key:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			lines[i] = fmt.Sprintf("%ssecret-key: %q", indent, key)
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, "management:", fmt.Sprintf("  secret-key: %q", key))
	}
	if errWrite := os.WriteFile(p.managementKey, []byte(key+"\n"), 0o600); errWrite != nil {
		return errWrite
	}
	return os.WriteFile(p.config, []byte(strings.Join(lines, "\n")), 0o600)
}

// executablePath returns a stable path to this binary. A Homebrew install runs
// from a versioned Cellar path, so the service uses the brew bin link instead.
func executablePath() (string, error) {
	exe, errExe := os.Executable()
	if errExe != nil {
		return "", errExe
	}
	if strings.Contains(exe, "/Cellar/") {
		for _, link := range []string{"/opt/homebrew/bin/relay", "/usr/local/bin/relay"} {
			if _, errStat := os.Stat(link); errStat == nil {
				return link, nil
			}
		}
	}
	if strings.Contains(exe, "/go-build") {
		return "", fmt.Errorf("relay is running from a temporary build (%s); build it first: go build -o ~/.local/bin/relay ./cmd/server", exe)
	}
	return exe, nil
}

func plistContent(exe string, p paths) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, serviceLabel, exe, p.config, p.log, p.log)
}

// installService writes the LaunchAgent and makes sure it is loaded. It
// restarts Relay only when the plist changed or a new management key needs to
// be picked up.
func installService(p paths, restart bool) (string, error) {
	exe, errExe := executablePath()
	if errExe != nil {
		return "", errExe
	}
	want := plistContent(exe, p)
	have, _ := os.ReadFile(p.plist)
	changed := string(have) != want
	if changed {
		if errDir := os.MkdirAll(filepath.Dir(p.plist), 0o755); errDir != nil {
			return "", errDir
		}
		if errWrite := os.WriteFile(p.plist, []byte(want), 0o644); errWrite != nil {
			return "", errWrite
		}
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	target := domain + "/" + serviceLabel
	loaded := exec.Command("launchctl", "print", target).Run() == nil
	switch {
	case !loaded:
		if out, errBoot := exec.Command("launchctl", "bootstrap", domain, p.plist).CombinedOutput(); errBoot != nil {
			return "", fmt.Errorf("launchctl bootstrap failed: %s", strings.TrimSpace(string(out)))
		}
		return "installed and started", nil
	case changed:
		_ = exec.Command("launchctl", "bootout", target).Run()
		if out, errBoot := exec.Command("launchctl", "bootstrap", domain, p.plist).CombinedOutput(); errBoot != nil {
			return "", fmt.Errorf("launchctl bootstrap failed: %s", strings.TrimSpace(string(out)))
		}
		return "updated and restarted", nil
	case restart:
		if out, errKick := exec.Command("launchctl", "kickstart", "-k", target).CombinedOutput(); errKick != nil {
			return "", fmt.Errorf("launchctl kickstart failed: %s", strings.TrimSpace(string(out)))
		}
		return "restarted", nil
	default:
		return "already running", nil
	}
}

// waitHealthy polls /v1/models with the client key until Relay answers 200.
func waitHealthy(base, clientKey string, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	httpClient := &http.Client{Timeout: 3 * time.Second}
	for {
		req, _ := http.NewRequest(http.MethodGet, base+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+clientKey)
		if resp, errDo := httpClient.Do(req); errDo == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}
