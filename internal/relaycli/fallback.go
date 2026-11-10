package relaycli

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// A fallback chain serves one model name from several providers in order. It
// uses two things the proxy already has: credential priority (the router tries
// the highest available priority first and moves down when an account is rate
// limited) and per-credential model aliases (a provider can serve the chain's
// model name with one of its own models). Each credential also records its
// place in the chain under "relay_fallback" so status can show the chains.

type fallbackStep struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

type fallbackChain struct {
	Model string         `json:"model"`
	Steps []fallbackStep `json:"steps"`
}

type modelAlias struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
	Fork  bool   `json:"fork,omitempty"`
}

// credentialFile is the part of an auth file the fallback command edits.
type credentialFile struct {
	Aliases  []modelAlias   `json:"model_aliases"`
	Fallback map[string]int `json:"relay_fallback"`
}

func (c *client) credentialFile(name string) (credentialFile, error) {
	var file credentialFile
	errDo := c.do(http.MethodGet, "/v8/management/credentials/download?name="+url.QueryEscape(name), nil, &file)
	return file, errDo
}

func runFallback(args []string) int {
	asJSON := hasFlag(&args, "json")
	if hasFlag(&args, "help") || hasFlag(&args, "h") {
		return runHelp(nil)
	}
	c, errClient := newClient()
	if errClient != nil {
		return fail(asJSON, errClient)
	}
	creds, errList := c.credentials()
	if errList != nil {
		return fail(asJSON, errList)
	}
	switch {
	case len(args) == 0:
		chains, errChains := c.fallbackChains(creds)
		if errChains != nil {
			return fail(asJSON, errChains)
		}
		output(asJSON, map[string]any{"ok": true, "fallback": chains}, func() { printFallback(chains) })
		return 0
	case args[0] == "set" && len(args) >= 3:
		steps := make([]fallbackStep, 0, len(args)-2)
		for _, raw := range args[2:] {
			provider, model, _ := strings.Cut(raw, ":")
			steps = append(steps, fallbackStep{Provider: strings.ToLower(strings.TrimSpace(provider)), Model: strings.TrimSpace(model)})
		}
		return c.applyFallback(asJSON, creds, args[1], steps)
	case args[0] == "clear" && len(args) == 2:
		return c.applyFallback(asJSON, creds, args[1], nil)
	}
	return fail(asJSON, errors.New("usage: relay fallback [set <model> <provider>[:<model>] ... | clear <model>]"))
}

// applyFallback writes the chain for model into every credential. A nil steps
// list removes the chain.
func (c *client) applyFallback(asJSON bool, creds []credential, model string, steps []fallbackStep) int {
	position := map[string]int{}
	for i, step := range steps {
		position[step.Provider] = i
		found := false
		for _, cred := range creds {
			if cred.provider() == step.Provider {
				found = true
			}
		}
		if !found {
			return fail(asJSON, fmt.Errorf("no %s account; add one first: relay accounts add %s", step.Provider, step.Provider))
		}
	}

	for _, cred := range creds {
		file, errFile := c.credentialFile(cred.Name)
		if errFile != nil {
			return fail(asJSON, fmt.Errorf("read %s: %w", cred.Name, errFile))
		}
		pos, inChain := position[cred.provider()]
		_, wasInChain := file.Fallback[model]
		if !inChain && !wasInChain {
			continue
		}
		aliases := make([]modelAlias, 0, len(file.Aliases)+1)
		for _, a := range file.Aliases {
			if !strings.EqualFold(a.Alias, model) {
				aliases = append(aliases, a)
			}
		}
		chains := map[string]int{}
		for k, v := range file.Fallback {
			if k != model {
				chains[k] = v
			}
		}
		patch := map[string]any{"name": cred.Name}
		if inChain {
			step := steps[pos]
			if step.Model != "" && !strings.EqualFold(step.Model, model) {
				aliases = append(aliases, modelAlias{Name: step.Model, Alias: model, Fork: true})
			}
			chains[model] = pos
			patch["priority"] = 100 - 10*pos
		} else if len(chains) == 0 {
			patch["priority"] = 0
		}
		patch["model_aliases"] = aliases
		patch["relay_fallback"] = chains
		if errPatch := c.do(http.MethodPatch, "/v8/management/credentials/fields", patch, nil); errPatch != nil {
			return fail(asJSON, fmt.Errorf("update %s: %w", cred.Name, errPatch))
		}
	}

	updated, _ := c.credentials()
	chains, errChains := c.fallbackChains(updated)
	if errChains != nil {
		return fail(asJSON, errChains)
	}
	output(asJSON, map[string]any{"ok": true, "fallback": chains}, func() {
		if steps == nil {
			fmt.Println("Removed the fallback chain for", model)
		}
		printFallback(chains)
	})
	return 0
}

// fallbackChains rebuilds the chains from the relay_fallback field of each
// credential.
func (c *client) fallbackChains(creds []credential) ([]fallbackChain, error) {
	type entry struct {
		pos  int
		step fallbackStep
	}
	byModel := map[string]map[string]entry{}
	for _, cred := range creds {
		file, errFile := c.credentialFile(cred.Name)
		if errFile != nil {
			return nil, errFile
		}
		for model, pos := range file.Fallback {
			step := fallbackStep{Provider: cred.provider()}
			for _, a := range file.Aliases {
				if strings.EqualFold(a.Alias, model) {
					step.Model = a.Name
				}
			}
			if byModel[model] == nil {
				byModel[model] = map[string]entry{}
			}
			byModel[model][step.Provider] = entry{pos: pos, step: step}
		}
	}
	chains := make([]fallbackChain, 0, len(byModel)) // [] not null in JSON
	for model, entries := range byModel {
		list := make([]entry, 0, len(entries))
		for _, e := range entries {
			list = append(list, e)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].pos < list[j].pos })
		chain := fallbackChain{Model: model}
		for _, e := range list {
			chain.Steps = append(chain.Steps, e.step)
		}
		chains = append(chains, chain)
	}
	sort.Slice(chains, func(i, j int) bool { return chains[i].Model < chains[j].Model })
	return chains, nil
}

func printFallback(chains []fallbackChain) {
	if len(chains) == 0 {
		fmt.Println("\nNo fallback chains. Example:\n  relay fallback set claude-opus-5-5 claude codex:gpt-6-sol opencode-go:deepseek-v4.1-flash")
		return
	}
	fmt.Println("\nFallback")
	for _, chain := range chains {
		parts := make([]string, 0, len(chain.Steps))
		for _, step := range chain.Steps {
			if step.Model != "" {
				parts = append(parts, step.Provider+" ("+step.Model+")")
			} else {
				parts = append(parts, step.Provider)
			}
		}
		fmt.Printf("  %s: %s\n", chain.Model, strings.Join(parts, " -> "))
	}
}
