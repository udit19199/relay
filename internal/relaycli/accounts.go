package relaycli

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
)

// oauthProviders are the browser sign-ins Relay offers.
var oauthProviders = []string{"claude", "codex", "kimi", "xai", "devin", "meta"}

func runAccounts(args []string) int {
	asJSON := hasFlag(&args, "json")
	if hasFlag(&args, "help") || hasFlag(&args, "h") {
		return runHelp(nil)
	}
	if len(args) >= 2 && args[0] == "add" {
		noBrowser := hasFlag(&args, "no-browser")
		keyStdin := hasFlag(&args, "key-stdin")
		provider := strings.ToLower(args[1])
		if provider == "opencode-go" || provider == "opencode" {
			return addOpenCodeGo(asJSON, keyStdin)
		}
		return addOAuth(asJSON, provider, noBrowser)
	}
	if len(args) >= 2 && args[0] == "remove" {
		return removeAccount(asJSON, args[1])
	}
	return fail(asJSON, errors.New("usage: relay accounts add <provider> | relay accounts remove <name>"))
}

// addOAuth starts a browser sign-in, prints the link, and waits until the new
// credential appears. The person signs in; the agent only hands over the link.
func addOAuth(asJSON bool, provider string, noBrowser bool) int {
	if provider == "anthropic" {
		provider = "claude"
	}
	known := false
	for _, name := range oauthProviders {
		known = known || name == provider
	}
	if !known {
		return fail(asJSON, fmt.Errorf("unknown provider %q; use one of: %s, opencode-go", provider, strings.Join(oauthProviders, ", ")))
	}
	c, errClient := newClient()
	if errClient != nil {
		return fail(asJSON, errClient)
	}
	before := map[string]bool{}
	if creds, errList := c.credentials(); errList == nil {
		for _, cred := range creds {
			before[cred.Name] = true
		}
	}

	var start struct {
		URL   string `json:"url"`
		State string `json:"state"`
	}
	// is_webui opens the local callback listener; without it the browser
	// fails on the final redirect.
	query := url.Values{"provider": {provider}, "is_webui": {"true"}}
	if errStart := c.do(http.MethodGet, "/v8/management/oauth/auth-url?"+query.Encode(), nil, &start); errStart != nil {
		return fail(asJSON, errStart)
	}
	if start.URL == "" || start.State == "" {
		return fail(asJSON, fmt.Errorf("Relay did not return a sign-in link for %q", provider))
	}

	if asJSON {
		output(true, map[string]any{"event": "sign_in_link", "provider": provider, "url": start.URL, "message": "Open this link and finish signing in. Waiting up to 5 minutes."}, nil)
	} else {
		fmt.Println("Open this link and finish signing in:")
		fmt.Println("  " + start.URL)
		fmt.Println("Waiting up to 5 minutes...")
	}
	if !noBrowser {
		_ = exec.Command("open", start.URL).Start()
	}

	cancel := func() {
		_ = c.do(http.MethodDelete, "/v8/management/oauth/session?state="+url.QueryEscape(start.State), nil, nil)
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-interrupt:
			cancel()
			return fail(asJSON, errors.New("sign-in cancelled"))
		case <-time.After(2 * time.Second):
		}
		var status struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if errStatus := c.do(http.MethodGet, "/v8/management/oauth/status?state="+url.QueryEscape(start.State), nil, &status); errStatus != nil {
			continue
		}
		switch status.Status {
		case "wait":
			continue
		case "ok":
			added := c.newCredentials(before)
			output(asJSON, map[string]any{"ok": true, "event": "added", "provider": provider, "accounts": added}, func() {
				if len(added) == 0 {
					fmt.Println("Signed in. The account is updating; check: relay status")
					return
				}
				for _, a := range added {
					fmt.Printf("Added %s account %s\n", a.Provider, firstNonEmpty(a.Account, a.Name))
				}
			})
			return 0
		default:
			return fail(asJSON, fmt.Errorf("sign-in failed: %s", firstNonEmpty(status.Error, status.Status)))
		}
	}
	cancel()
	return fail(asJSON, errors.New("sign-in timed out after 5 minutes"))
}

// newCredentials waits briefly for credentials that were not in before.
func (c *client) newCredentials(before map[string]bool) []accountStatus {
	for i := 0; i < 10; i++ {
		if creds, errList := c.credentials(); errList == nil {
			var added []accountStatus
			for _, cred := range creds {
				if !before[cred.Name] {
					added = append(added, c.accountStatus(cred))
				}
			}
			if len(added) > 0 {
				return added
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

// addOpenCodeGo saves an OpenCode Go API key and checks it against OpenCode.
// The key comes from OPENCODE_API_KEY or stdin, never from an argument, so it
// stays out of shell history and agent transcripts.
func addOpenCodeGo(asJSON, keyStdin bool) int {
	key := strings.TrimSpace(os.Getenv("OPENCODE_API_KEY"))
	if keyStdin {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		key = strings.TrimSpace(line)
	}
	if key == "" {
		return fail(asJSON, errors.New("set OPENCODE_API_KEY or pipe the key with --key-stdin"))
	}
	c, errClient := newClient()
	if errClient != nil {
		return fail(asJSON, errClient)
	}
	suffix, _ := randomKey()
	name := "opencode-go-" + suffix[len(suffix)-8:] + ".json"
	body := map[string]any{"type": "opencode-go", "label": "OpenCode Go", "api_key": key}
	if errUpload := c.do(http.MethodPost, "/v8/management/credentials?name="+url.QueryEscape(name), body, nil); errUpload != nil {
		return fail(asJSON, errUpload)
	}

	var added *credential
	for i := 0; i < 10 && added == nil; i++ {
		if creds, errList := c.credentials(); errList == nil {
			for _, cred := range creds {
				if cred.Name == name {
					found := cred
					added = &found
				}
			}
		}
		if added == nil {
			time.Sleep(300 * time.Millisecond)
		}
	}
	if added == nil {
		return fail(asJSON, errors.New("Relay saved the key but did not load it; check: relay doctor"))
	}

	errQuota := c.do(http.MethodPost, "/v0/management/quota/fetch", map[string]string{"auth_index": added.AuthIndex}, nil)
	var httpErr *httpError
	if errors.As(errQuota, &httpErr) && httpErr.status == http.StatusUnprocessableEntity {
		_ = c.do(http.MethodDelete, "/v8/management/credentials", map[string]string{"name": name}, nil)
		return fail(asJSON, errors.New("OpenCode Go rejected this key; nothing was saved"))
	}
	status := c.accountStatus(*added)
	output(asJSON, map[string]any{"ok": true, "event": "added", "provider": "opencode-go", "accounts": []accountStatus{status}, "verified": errQuota == nil}, func() {
		fmt.Println("Added OpenCode Go account", name)
		if errQuota != nil {
			fmt.Println("Could not check its limits yet:", errQuota)
		}
	})
	return 0
}

func removeAccount(asJSON bool, name string) int {
	c, errClient := newClient()
	if errClient != nil {
		return fail(asJSON, errClient)
	}
	if errDelete := c.do(http.MethodDelete, "/v8/management/credentials", map[string]string{"name": name}, nil); errDelete != nil {
		return fail(asJSON, errDelete)
	}
	output(asJSON, map[string]any{"ok": true, "removed": name}, func() { fmt.Println("Removed", name) })
	return 0
}
