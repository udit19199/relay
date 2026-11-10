package relaycli

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// window is one usage limit: how much is left and when it resets.
type window struct {
	Name    string     `json:"name"`
	Left    int        `json:"left_percent"`
	Resets  *time.Time `json:"resets_at,omitempty"`
	Limited bool       `json:"limited,omitempty"`
}

type accountStatus struct {
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	Account  string   `json:"account,omitempty"`
	State    string   `json:"state"`
	Message  string   `json:"message,omitempty"`
	Priority int      `json:"priority"`
	Limits   []window `json:"limits,omitempty"`
}

type statusResult struct {
	OK       bool            `json:"ok"`
	Running  bool            `json:"running"`
	BaseURL  string          `json:"base_url"`
	Accounts []accountStatus `json:"accounts"`
	Fallback []fallbackChain `json:"fallback"`
}

func runStatus(args []string) int {
	asJSON := hasFlag(&args, "json")
	c, errClient := newClient()
	if errClient != nil {
		return fail(asJSON, errClient)
	}
	creds, errList := c.credentials()
	if errList != nil {
		return fail(asJSON, errList)
	}
	res := statusResult{OK: true, Running: true, BaseURL: c.base, Accounts: []accountStatus{}}
	for _, cred := range creds {
		res.Accounts = append(res.Accounts, c.accountStatus(cred))
	}
	sort.SliceStable(res.Accounts, func(i, j int) bool { return res.Accounts[i].Priority > res.Accounts[j].Priority })
	res.Fallback, _ = c.fallbackChains(creds)

	output(asJSON, res, func() {
		fmt.Println("Relay is running at", res.BaseURL)
		if len(res.Accounts) == 0 {
			fmt.Println("\nNo accounts yet. Add one: relay accounts add claude")
		} else {
			fmt.Println("\nAccounts")
			for _, a := range res.Accounts {
				line := fmt.Sprintf("  %-12s %-34s %-8s", a.Provider, firstNonEmpty(a.Account, a.Name), a.State)
				var parts []string
				for _, w := range a.Limits {
					part := fmt.Sprintf("%s %d%% left", w.Name, w.Left)
					if w.Resets != nil {
						part += " (resets " + w.Resets.Local().Format("Mon 15:04") + ")"
					}
					parts = append(parts, part)
				}
				if len(parts) > 0 {
					line += "  " + strings.Join(parts, ", ")
				}
				if a.Message != "" && a.State != "ok" {
					line += "  " + a.Message
				}
				fmt.Println(line)
			}
		}
		printFallback(res.Fallback)
	})
	return 0
}

func (c *client) accountStatus(cred credential) accountStatus {
	a := accountStatus{
		Name:     cred.Name,
		Provider: cred.provider(),
		Account:  firstNonEmpty(cred.Email, cred.Label),
		Priority: cred.Priority,
		State:    "ok",
	}
	switch {
	case cred.Disabled:
		a.State = "disabled"
	case cred.Unavailable:
		a.State = "limited"
		a.Message = oneLine(cred.StatusMessage)
	case cred.Status == "error":
		a.State = "error"
		a.Message = oneLine(cred.StatusMessage)
	}
	if a.Provider == "opencode-go" {
		a.Limits = c.openCodeGoLimits(cred.AuthIndex)
	} else {
		a.Limits = signalLimits(cred.Quota.Signals)
	}
	return a
}

// signalLimits reads the limit headers Relay saw on the account's last
// response, for example Anthropic-Ratelimit-Unified-5h-Utilization and -Reset.
func signalLimits(signals map[string]any) []window {
	lower := make(map[string]string, len(signals))
	for key, value := range signals {
		lower[strings.ToLower(key)] = fmt.Sprint(value)
	}
	var out []window
	for _, spec := range []struct{ name, marker string }{{"5h", "5h"}, {"week", "7d"}} {
		for key, value := range lower {
			if !strings.Contains(key, "-"+spec.marker+"-") || !strings.HasSuffix(key, "utilization") {
				continue
			}
			used, errParse := strconv.ParseFloat(value, 64)
			if errParse != nil {
				continue
			}
			w := window{Name: spec.name, Left: clampPercent(1 - used)}
			resetKey := strings.TrimSuffix(key, "utilization") + "reset"
			if seconds, errReset := strconv.ParseInt(lower[resetKey], 10, 64); errReset == nil {
				reset := time.Unix(seconds, 0)
				if reset.Before(time.Now()) {
					w.Left = 100 // the window already reset since the last response
				} else {
					w.Resets = &reset
				}
			}
			status := lower[strings.TrimSuffix(key, "utilization")+"status"]
			w.Limited = status != "" && status != "allowed" && status != "allowed_warning"
			out = append(out, w)
			break
		}
	}
	return out
}

func (c *client) openCodeGoLimits(authIndex string) []window {
	var reply struct {
		Groups []struct {
			Buckets []struct {
				Window            string  `json:"window"`
				RemainingFraction float64 `json:"remainingFraction"`
				ResetTime         string  `json:"resetTime"`
			} `json:"buckets"`
		} `json:"groups"`
	}
	// The quota endpoint exists only under /v0; the notch app uses it too.
	if errDo := c.do(http.MethodPost, "/v0/management/quota/fetch", map[string]string{"auth_index": authIndex}, &reply); errDo != nil {
		return nil
	}
	var out []window
	for _, group := range reply.Groups {
		for _, bucket := range group.Buckets {
			w := window{Name: bucket.Window, Left: clampPercent(bucket.RemainingFraction)}
			if reset, errParse := time.Parse(time.RFC3339, bucket.ResetTime); errParse == nil {
				w.Resets = &reset
			}
			out = append(out, w)
		}
	}
	return out
}

func clampPercent(fraction float64) int {
	return int(math.Max(0, math.Min(100, math.Round(fraction*100))))
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if strings.Contains(s, "rate_limit") {
		return "rate limited until the limit resets"
	}
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// check is one doctor finding. Level is ok, warn, or fail.
type check struct {
	Name   string `json:"name"`
	Level  string `json:"level"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

func runDoctor(args []string) int {
	asJSON := hasFlag(&args, "json")
	p := relayPaths()
	var checks []check
	add := func(name, level, detail, fix string) {
		checks = append(checks, check{Name: name, Level: level, Detail: detail, Fix: fix})
	}

	cfg, errCfg := loadConfig(p.config)
	if errCfg != nil {
		add("config", "fail", errCfg.Error(), "relay setup")
		return finishDoctor(asJSON, checks)
	}
	add("config", "ok", p.config, "")

	if host := cfg.host(); host == "" || host == "0.0.0.0" || host == "::" {
		add("listen address", "warn", "Relay listens on every network interface", `set server.host to "127.0.0.1" in `+p.config)
	} else {
		add("listen address", "ok", host, "")
	}

	if cfg.clientKey() == "" {
		add("client key", "fail", "access.api-keys is empty", "add a key under access.api-keys in "+p.config)
	} else {
		add("client key", "ok", "", "")
	}

	keyOK := false
	if info, errStat := os.Stat(p.managementKey); errStat != nil {
		add("management key", "fail", p.managementKey+" is missing", "relay setup --reset-management-key")
	} else {
		data, _ := os.ReadFile(p.managementKey)
		key := strings.TrimSpace(string(data))
		secret := cfg.secretKey()
		switch {
		case secret == "":
			add("management key", "fail", "management.secret-key is empty, so the management API is off", "relay setup --reset-management-key")
		case strings.HasPrefix(secret, "$2") && bcrypt.CompareHashAndPassword([]byte(secret), []byte(key)) != nil,
			!strings.HasPrefix(secret, "$2") && secret != key:
			add("management key", "fail", "the key file does not match the config", "relay setup --reset-management-key")
		default:
			keyOK = true
			if info.Mode().Perm()&0o077 != 0 {
				add("management key", "warn", "readable by other users", "chmod 600 "+p.managementKey)
			} else {
				add("management key", "ok", "", "")
			}
		}
	}

	base := baseURL(cfg.port())
	running := waitHealthy(base, cfg.clientKey(), 0)
	if running {
		add("proxy", "ok", "answering at "+base, "")
	} else {
		add("proxy", "fail", "no answer at "+base, "relay setup (or check "+p.log+")")
	}

	switch data, errRead := os.ReadFile(p.plist); {
	case os.Getenv("RELAY_HOME") != "":
		add("login service", "ok", "skipped: RELAY_HOME is set, so this install runs without the login service", "")
	case errRead != nil:
		add("login service", "warn", "not installed, so Relay will not start after a reboot", "relay setup")
	case !strings.Contains(string(data), "<string>"+p.config+"</string>"):
		add("login service", "warn", "the login service runs a different config", "relay setup")
	case !plistBinaryExists(string(data)):
		add("login service", "fail", "the service points to a binary that no longer exists", "relay setup")
	default:
		add("login service", "ok", p.plist, "")
	}

	if running && keyOK {
		c, _ := newClient()
		creds, errList := c.credentials()
		switch {
		case errList != nil:
			add("management API", "fail", errList.Error(), "relay setup --reset-management-key")
		case len(creds) == 0:
			add("accounts", "warn", "no accounts", "relay accounts add claude")
		default:
			for _, cred := range creds {
				a := c.accountStatus(cred)
				name := "account " + firstNonEmpty(a.Account, a.Name)
				switch a.State {
				case "ok":
					add(name, "ok", a.Provider, "")
				case "limited":
					add(name, "warn", a.Message, "wait for the reset; Relay uses other accounts meanwhile")
				case "disabled":
					add(name, "warn", "disabled", "")
				default:
					add(name, "fail", a.Message, "relay accounts remove "+a.Name+" && relay accounts add "+a.Provider)
				}
			}
		}
	}
	return finishDoctor(asJSON, checks)
}

func plistBinaryExists(plist string) bool {
	start := strings.Index(plist, "<array>")
	if start < 0 {
		return false
	}
	rest := plist[start:]
	open := strings.Index(rest, "<string>")
	end := strings.Index(rest, "</string>")
	if open < 0 || end < open {
		return false
	}
	_, errStat := os.Stat(rest[open+len("<string>") : end])
	return errStat == nil
}

func finishDoctor(asJSON bool, checks []check) int {
	failed := 0
	for _, c := range checks {
		if c.Level == "fail" {
			failed++
		}
	}
	output(asJSON, map[string]any{"ok": failed == 0, "checks": checks}, func() {
		marks := map[string]string{"ok": "ok  ", "warn": "warn", "fail": "FAIL"}
		for _, c := range checks {
			line := fmt.Sprintf("%s  %s", marks[c.Level], c.Name)
			if c.Detail != "" {
				line += ": " + c.Detail
			}
			fmt.Println(line)
			if c.Fix != "" && c.Level != "ok" {
				fmt.Println("        fix: " + c.Fix)
			}
		}
	})
	if failed > 0 {
		return 1
	}
	return 0
}
