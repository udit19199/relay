package config

import (
	"sort"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// NormalizeClaudeConfig applies default Claude provider configuration values.
func (cfg *Config) NormalizeClaudeConfig() {
	if cfg == nil {
		return
	}
	if cfg.Claude.SessionDatePinDays == nil {
		days := DefaultClaudeSessionDatePinDays
		cfg.Claude.SessionDatePinDays = &days
	}
}

// SanitizeCodexHeaderDefaults trims surrounding whitespace from the
// configured Codex header fallback values.
func (cfg *Config) SanitizeCodexHeaderDefaults() {
	if cfg == nil {
		return
	}
	cfg.CodexHeaderDefaults.UserAgent = strings.TrimSpace(cfg.CodexHeaderDefaults.UserAgent)
	cfg.CodexHeaderDefaults.BetaFeatures = strings.TrimSpace(cfg.CodexHeaderDefaults.BetaFeatures)
	cfg.Codex.SignInWithChatGPTIdentifier = strings.TrimSpace(cfg.Codex.SignInWithChatGPTIdentifier)
}

// SanitizeClaudeHeaderDefaults trims surrounding whitespace from the
// configured Claude fingerprint baseline values, normalizes the native
// entrypoint and beta override lists, and resets an unknown client-request-id
// mode to the default.
func (cfg *Config) SanitizeClaudeHeaderDefaults() {
	if cfg == nil {
		return
	}
	cfg.ClaudeHeaderDefaults.NativeEntrypoints = normalizeClaudeEntrypointNames(cfg.ClaudeHeaderDefaults.NativeEntrypoints)
	cfg.ClaudeHeaderDefaults.AnthropicBetaStrip = normalizeClaudeBetaNames(cfg.ClaudeHeaderDefaults.AnthropicBetaStrip)
	cfg.ClaudeHeaderDefaults.AnthropicBetaAppend = normalizeClaudeBetaNames(cfg.ClaudeHeaderDefaults.AnthropicBetaAppend)
	mode := strings.ToLower(strings.TrimSpace(cfg.ClaudeHeaderDefaults.ClientRequestID))
	switch mode {
	case "", ClaudeClientRequestIDOn, ClaudeClientRequestIDOff, ClaudeClientRequestIDPassthrough:
	default:
		log.Warnf("unrecognized claude header-defaults client-request-id %q (supported: on, off, passthrough); falling back to on", cfg.ClaudeHeaderDefaults.ClientRequestID)
		mode = ""
	}
	cfg.ClaudeHeaderDefaults.ClientRequestID = mode
	cfg.ClaudeHeaderDefaults.UserAgent = strings.TrimSpace(cfg.ClaudeHeaderDefaults.UserAgent)
	cfg.ClaudeHeaderDefaults.PackageVersion = strings.TrimSpace(cfg.ClaudeHeaderDefaults.PackageVersion)
	cfg.ClaudeHeaderDefaults.RuntimeVersion = strings.TrimSpace(cfg.ClaudeHeaderDefaults.RuntimeVersion)
	cfg.ClaudeHeaderDefaults.OS = strings.TrimSpace(cfg.ClaudeHeaderDefaults.OS)
	cfg.ClaudeHeaderDefaults.Arch = strings.TrimSpace(cfg.ClaudeHeaderDefaults.Arch)
	cfg.ClaudeHeaderDefaults.Timeout = strings.TrimSpace(cfg.ClaudeHeaderDefaults.Timeout)
	cfg.ClaudeHeaderDefaults.Timezone = strings.TrimSpace(cfg.ClaudeHeaderDefaults.Timezone)
}

// normalizeClaudeEntrypointNames trims and lowercases entrypoint names,
// dropping empty and duplicate entries while keeping the configured order.
func normalizeClaudeEntrypointNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeClaudeBetaNames trims beta names and drops empty and duplicate
// entries while keeping the configured order.
func normalizeClaudeBetaNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SanitizeOAuthModelAlias normalizes and deduplicates global OAuth model name aliases.
// It trims whitespace, normalizes channel keys to lower-case, drops empty entries,
// allows multiple aliases per upstream name, and ensures aliases are unique within each channel.
func (cfg *Config) SanitizeOAuthModelAlias() {
	if cfg == nil || len(cfg.OAuthModelAlias) == 0 {
		return
	}
	out := make(map[string][]OAuthModelAlias, len(cfg.OAuthModelAlias))
	for rawChannel, aliases := range cfg.OAuthModelAlias {
		channel := strings.ToLower(strings.TrimSpace(rawChannel))
		if channel == "" || len(aliases) == 0 {
			continue
		}
		seenAlias := make(map[string]struct{}, len(aliases))
		clean := make([]OAuthModelAlias, 0, len(aliases))
		for _, entry := range aliases {
			name := strings.TrimSpace(entry.Name)
			alias := strings.TrimSpace(entry.Alias)
			if name == "" || alias == "" {
				continue
			}
			if strings.EqualFold(name, alias) {
				continue
			}
			aliasKey := strings.ToLower(alias)
			if _, ok := seenAlias[aliasKey]; ok {
				continue
			}
			seenAlias[aliasKey] = struct{}{}
			clean = append(clean, OAuthModelAlias{
				Name:         name,
				Alias:        alias,
				Fork:         entry.Fork,
				DisplayName:  strings.TrimSpace(entry.DisplayName),
				ForceMapping: entry.ForceMapping,
			})
		}
		if len(clean) > 0 {
			out[channel] = clean
		}
	}
	cfg.OAuthModelAlias = out
}

// SanitizeOAuthSettings normalizes and deduplicates global OAuth model settings.
// It trims whitespace, normalizes channel keys to lower-case, drops empty entries,
// and ensures entries are unique within each channel.
func (cfg *Config) SanitizeOAuthSettings() {
	if cfg == nil || len(cfg.OAuthSettings) == 0 {
		return
	}
	out := make(map[string][]OAuthModelSetting, len(cfg.OAuthSettings))
	for rawChannel, settings := range cfg.OAuthSettings {
		channel := strings.ToLower(strings.TrimSpace(rawChannel))
		if channel == "" || len(settings) == 0 {
			continue
		}
		seen := make(map[string]struct{}, len(settings))
		reversed := make([]OAuthModelSetting, 0, len(settings))
		for i := len(settings) - 1; i >= 0; i-- {
			entry := settings[i]
			name := strings.TrimSpace(entry.Name)
			if name == "" {
				continue
			}
			alias := strings.TrimSpace(entry.Alias)
			key := strings.ToLower(name) + "->" + strings.ToLower(alias)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			reversed = append(reversed, OAuthModelSetting{
				Name:             name,
				Alias:            alias,
				MaxContextLength: entry.MaxContextLength,
			})
		}
		if len(reversed) > 0 {
			clean := make([]OAuthModelSetting, len(reversed))
			for i := range reversed {
				clean[len(reversed)-1-i] = reversed[i]
			}
			out[channel] = clean
		}
	}
	cfg.OAuthSettings = out
}

// SanitizeOAuthRequestScopedErrors normalizes and validates global OAuth request-scoped error rules.
// It trims whitespace, normalizes channel keys to lower-case, validates status/action, and drops invalid rules.
func (cfg *Config) SanitizeOAuthRequestScopedErrors() {
	if cfg == nil || len(cfg.OAuthRequestScopedErrors) == 0 {
		return
	}
	out := make(map[string][]RequestScopedErrorRule, len(cfg.OAuthRequestScopedErrors))
	for rawChannel, rules := range cfg.OAuthRequestScopedErrors {
		channel := strings.ToLower(strings.TrimSpace(rawChannel))
		if channel == "" || len(rules) == 0 {
			continue
		}
		clean := make([]RequestScopedErrorRule, 0, len(rules))
		for _, r := range rules {
			action := strings.ToLower(strings.TrimSpace(r.Action))
			match := make([]string, 0, len(r.Match))
			for _, m := range r.Match {
				if tm := strings.TrimSpace(m); tm != "" {
					match = append(match, tm)
				}
			}
			matchRegexr := make([]string, 0, len(r.MatchRegexr))
			for _, re := range r.MatchRegexr {
				if tre := strings.TrimSpace(re); tre != "" {
					matchRegexr = append(matchRegexr, tre)
				}
			}
			if r.Status <= 0 || (len(match) == 0 && len(matchRegexr) == 0) || action == "" {
				continue
			}
			clean = append(clean, RequestScopedErrorRule{
				Status:      r.Status,
				Match:       match,
				MatchRegexr: matchRegexr,
				Action:      action,
			})
		}
		if len(clean) > 0 {
			out[channel] = clean
		}
	}
	if len(out) == 0 {
		cfg.OAuthRequestScopedErrors = nil
		return
	}
	cfg.OAuthRequestScopedErrors = out
}

// SanitizeOpenAICompatibility removes OpenAI-compatibility provider entries that are
// not actionable, specifically those missing a BaseURL. It trims whitespace before
// evaluation and preserves the relative order of remaining entries.
func (cfg *Config) SanitizeOpenAICompatibility() {
	if cfg == nil || len(cfg.OpenAICompatibility) == 0 {
		return
	}
	out := make([]OpenAICompatibility, 0, len(cfg.OpenAICompatibility))
	for i := range cfg.OpenAICompatibility {
		e := cfg.OpenAICompatibility[i]
		e.Name = strings.TrimSpace(e.Name)
		e.Prefix = normalizeModelPrefix(e.Prefix)
		e.BaseURL = strings.TrimSpace(e.BaseURL)
		e.Headers = NormalizeHeaders(e.Headers)
		if e.BaseURL == "" {
			// Skip providers with no base-url; treated as removed
			continue
		}
		out = append(out, e)
	}
	cfg.OpenAICompatibility = out
}

// SanitizeCodexKeys removes Codex API key entries missing a BaseURL.
// It trims whitespace and preserves order for remaining entries.
func (cfg *Config) SanitizeCodexKeys() {
	if cfg == nil {
		return
	}
	cfg.CodexKey = sanitizeCodexKeyEntries(cfg.CodexKey)
}

// SanitizeXAIKeys removes xAI API key entries missing a BaseURL.
// It applies the same normalization rules as codex-api-key.
func (cfg *Config) SanitizeXAIKeys() {
	if cfg == nil {
		return
	}
	cfg.XAIKey = sanitizeCodexKeyEntries(cfg.XAIKey)
	for i := range cfg.XAIKey {
		cfg.XAIKey[i].AlphaSearch = false
	}
}

func sanitizeCodexKeyEntries(entries []CodexKey) []CodexKey {
	if len(entries) == 0 {
		return entries
	}
	out := make([]CodexKey, 0, len(entries))
	for i := range entries {
		e := entries[i]
		e.Prefix = normalizeModelPrefix(e.Prefix)
		e.BaseURL = strings.TrimSpace(e.BaseURL)
		e.Headers = NormalizeHeaders(e.Headers)
		e.ExcludedModels = NormalizeExcludedModels(e.ExcludedModels)
		if e.BaseURL == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// NormalizeCloakConfig trims strings and removes blank sensitive words.
func NormalizeCloakConfig(cloak *CloakConfig) *CloakConfig {
	if cloak == nil {
		return nil
	}
	cloak.Mode = strings.TrimSpace(cloak.Mode)
	if len(cloak.SensitiveWords) > 0 {
		normalizedWords := make([]string, 0, len(cloak.SensitiveWords))
		for _, w := range cloak.SensitiveWords {
			if trimmed := strings.TrimSpace(w); trimmed != "" {
				normalizedWords = append(normalizedWords, trimmed)
			}
		}
		if len(normalizedWords) > 0 {
			cloak.SensitiveWords = normalizedWords
		} else {
			cloak.SensitiveWords = nil
		}
	} else {
		cloak.SensitiveWords = nil
	}
	return cloak
}

// SanitizeClaudeKeys normalizes headers for Claude credentials.
func (cfg *Config) SanitizeClaudeKeys() {
	if cfg == nil || len(cfg.ClaudeKey) == 0 {
		return
	}
	for i := range cfg.ClaudeKey {
		entry := &cfg.ClaudeKey[i]
		entry.Prefix = normalizeModelPrefix(entry.Prefix)
		entry.Headers = NormalizeHeaders(entry.Headers)
		entry.ExcludedModels = NormalizeExcludedModels(entry.ExcludedModels)
		entry.Cloak = NormalizeCloakConfig(entry.Cloak)
		// Only a recognized value is rewritten. An unrecognized one is preserved as
		// written so sanitizing a config file never destroys operator input; the
		// request path falls back to the default profile and reports it once.
		if normalized, ok := NormalizeClaudeFingerprintProfile(entry.FingerprintProfile); ok {
			entry.FingerprintProfile = normalized
		} else {
			entry.FingerprintProfile = strings.TrimSpace(entry.FingerprintProfile)
		}
	}
}

// FormatConnectionPoolDedupID serializes a per-credential connection-pool override
// deterministically for credential deduplication and auth-index mapping.
func FormatConnectionPoolDedupID(pool *ProxyConnectionPoolConfig) string {
	if pool == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("pool:")
	if pool.Enabled != nil {
		b.WriteString(strconv.FormatBool(*pool.Enabled))
	}
	b.WriteByte(0)
	b.WriteString(strings.TrimSpace(pool.IdleConnTimeout))
	b.WriteByte(0)
	if pool.MaxIdleConnsPerHost != nil {
		b.WriteString(strconv.Itoa(*pool.MaxIdleConnsPerHost))
	}
	return b.String()
}

// FormatSortedHeaders serializes headers deterministically with null byte separators.
func FormatSortedHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte(0)
		b.WriteString(headers[k])
		b.WriteByte(0)
	}
	return b.String()
}

func normalizeModelPrefix(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "/") {
		return ""
	}
	return trimmed
}

// NormalizeHeaders trims header keys and values and removes empty pairs.
func NormalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	clean := make(map[string]string, len(headers))
	for k, v := range headers {
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if key == "" || val == "" {
			continue
		}
		clean[key] = val
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

// NormalizeExcludedModels trims, lowercases, and deduplicates model exclusion patterns.
// It preserves the order of first occurrences and drops empty entries.
func NormalizeExcludedModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, raw := range models {
		trimmed := strings.ToLower(strings.TrimSpace(raw))
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeOAuthExcludedModels cleans provider -> excluded models mappings by normalizing provider keys
// and applying model exclusion normalization to each entry.
func NormalizeOAuthExcludedModels(entries map[string][]string) map[string][]string {
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string][]string, len(entries))
	for provider, models := range entries {
		key := strings.ToLower(strings.TrimSpace(provider))
		if key == "" {
			continue
		}
		normalized := NormalizeExcludedModels(models)
		if len(normalized) == 0 {
			continue
		}
		out[key] = normalized
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
