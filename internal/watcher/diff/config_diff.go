package diff

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/udit19199/relay/internal/config"
)

// BuildConfigChangeDetails computes a redacted, human-readable list of config changes.
// Secrets are never printed; only structural or non-sensitive fields are surfaced.
func BuildConfigChangeDetails(oldCfg, newCfg *config.Config) []string {
	changes := make([]string, 0, 16)
	if oldCfg == nil || newCfg == nil {
		return changes
	}

	// Simple scalars
	if oldCfg.Client.Codex.EnableApplyPatch != newCfg.Client.Codex.EnableApplyPatch {
		changes = append(changes, fmt.Sprintf("client.codex.enable-apply-patch: %t -> %t", oldCfg.Client.Codex.EnableApplyPatch, newCfg.Client.Codex.EnableApplyPatch))
	}
	if oldCfg.Port != newCfg.Port {
		changes = append(changes, fmt.Sprintf("port: %d -> %d", oldCfg.Port, newCfg.Port))
	}
	if oldCfg.AuthDir != newCfg.AuthDir {
		changes = append(changes, fmt.Sprintf("auth-dir: %s -> %s", oldCfg.AuthDir, newCfg.AuthDir))
	}
	if oldCfg.Debug != newCfg.Debug {
		changes = append(changes, fmt.Sprintf("debug: %t -> %t", oldCfg.Debug, newCfg.Debug))
	}
	if oldCfg.Pprof.Enable != newCfg.Pprof.Enable {
		changes = append(changes, fmt.Sprintf("pprof.enable: %t -> %t", oldCfg.Pprof.Enable, newCfg.Pprof.Enable))
	}
	if strings.TrimSpace(oldCfg.Pprof.Addr) != strings.TrimSpace(newCfg.Pprof.Addr) {
		changes = append(changes, fmt.Sprintf("pprof.addr: %s -> %s", strings.TrimSpace(oldCfg.Pprof.Addr), strings.TrimSpace(newCfg.Pprof.Addr)))
	}
	if oldCfg.KVStore.Disabled != newCfg.KVStore.Disabled {
		changes = append(changes, fmt.Sprintf("kv-store.disabled: %t -> %t", oldCfg.KVStore.Disabled, newCfg.KVStore.Disabled))
	}
	if oldCfg.EffectiveKVStorePath() != newCfg.EffectiveKVStorePath() {
		changes = append(changes, fmt.Sprintf("kv-store.path: %s -> %s", oldCfg.EffectiveKVStorePath(), newCfg.EffectiveKVStorePath()))
	}
	if oldCfg.LoggingToFile != newCfg.LoggingToFile {
		changes = append(changes, fmt.Sprintf("logging-to-file: %t -> %t", oldCfg.LoggingToFile, newCfg.LoggingToFile))
	}
	if oldCfg.DisableCooling != newCfg.DisableCooling {
		changes = append(changes, fmt.Sprintf("disable-cooling: %t -> %t", oldCfg.DisableCooling, newCfg.DisableCooling))
	}
	if oldCfg.SaveCooldownStatus != newCfg.SaveCooldownStatus {
		changes = append(changes, fmt.Sprintf("save-cooldown-status: %t -> %t", oldCfg.SaveCooldownStatus, newCfg.SaveCooldownStatus))
	}
	if oldCfg.TransientErrorCooldownSeconds != newCfg.TransientErrorCooldownSeconds {
		changes = append(changes, fmt.Sprintf("transient-error-cooldown-seconds: %d -> %d", oldCfg.TransientErrorCooldownSeconds, newCfg.TransientErrorCooldownSeconds))
	}
	if oldCfg.DisableClaudeCloakMode != newCfg.DisableClaudeCloakMode {
		changes = append(changes, fmt.Sprintf("disable-claude-cloak-mode: %t -> %t", oldCfg.DisableClaudeCloakMode, newCfg.DisableClaudeCloakMode))
	}
	if oldCfg.ClaudeCode.DisableCloakingModelList != newCfg.ClaudeCode.DisableCloakingModelList {
		changes = append(changes, fmt.Sprintf("claude-code.disable-cloaking-model-list: %t -> %t", oldCfg.ClaudeCode.DisableCloakingModelList, newCfg.ClaudeCode.DisableCloakingModelList))
	}
	if oldCfg.RequestLog != newCfg.RequestLog {
		changes = append(changes, fmt.Sprintf("request-log: %t -> %t", oldCfg.RequestLog, newCfg.RequestLog))
	}
	if oldCfg.LogsMaxTotalSizeMB != newCfg.LogsMaxTotalSizeMB {
		changes = append(changes, fmt.Sprintf("logs-max-total-size-mb: %d -> %d", oldCfg.LogsMaxTotalSizeMB, newCfg.LogsMaxTotalSizeMB))
	}
	if oldCfg.ErrorLogsMaxFiles != newCfg.ErrorLogsMaxFiles {
		changes = append(changes, fmt.Sprintf("error-logs-max-files: %d -> %d", oldCfg.ErrorLogsMaxFiles, newCfg.ErrorLogsMaxFiles))
	}
	if oldCfg.RequestRetry != newCfg.RequestRetry {
		changes = append(changes, fmt.Sprintf("request-retry: %d -> %d", oldCfg.RequestRetry, newCfg.RequestRetry))
	}
	if oldCfg.MaxRetryCredentials != newCfg.MaxRetryCredentials {
		changes = append(changes, fmt.Sprintf("max-retry-credentials: %d -> %d", oldCfg.MaxRetryCredentials, newCfg.MaxRetryCredentials))
	}
	if oldCfg.MaxRetryInterval != newCfg.MaxRetryInterval {
		changes = append(changes, fmt.Sprintf("max-retry-interval: %d -> %d", oldCfg.MaxRetryInterval, newCfg.MaxRetryInterval))
	}
	if oldCfg.ProxyURL != newCfg.ProxyURL {
		changes = append(changes, fmt.Sprintf("proxy-url: %s -> %s", formatProxyURL(oldCfg.ProxyURL), formatProxyURL(newCfg.ProxyURL)))
	}
	if oldCfg.NonStreamKeepAliveInterval != newCfg.NonStreamKeepAliveInterval {
		changes = append(changes, fmt.Sprintf("nonstream-keepalive-interval: %d -> %d", oldCfg.NonStreamKeepAliveInterval, newCfg.NonStreamKeepAliveInterval))
	}

	// Quota-exceeded behavior
	if !reflect.DeepEqual(oldCfg.Devin.SensitiveWords, newCfg.Devin.SensitiveWords) {
		changes = append(changes, fmt.Sprintf("devin.sensitive-words: %d -> %d", len(oldCfg.Devin.SensitiveWords), len(newCfg.Devin.SensitiveWords)))
	}

	if oldCfg.Codex.DisableCodexCloaking != newCfg.Codex.DisableCodexCloaking {
		changes = append(changes, fmt.Sprintf("codex.disable-codex-cloaking: %t -> %t", oldCfg.Codex.DisableCodexCloaking, newCfg.Codex.DisableCodexCloaking))
	}
	if oldCfg.Codex.StreamBootstrapBuffering != newCfg.Codex.StreamBootstrapBuffering {
		changes = append(changes, fmt.Sprintf("codex.stream-bootstrap-buffering: %t -> %t", oldCfg.Codex.StreamBootstrapBuffering, newCfg.Codex.StreamBootstrapBuffering))
	}
	if strings.TrimSpace(oldCfg.Codex.StreamBootstrapTimeout) != strings.TrimSpace(newCfg.Codex.StreamBootstrapTimeout) {
		changes = append(changes, fmt.Sprintf("codex.stream-bootstrap-timeout: %s -> %s", strings.TrimSpace(oldCfg.Codex.StreamBootstrapTimeout), strings.TrimSpace(newCfg.Codex.StreamBootstrapTimeout)))
	}
	if oldCfg.Client.Codex.OptimizeMultiAgentV2 != newCfg.Client.Codex.OptimizeMultiAgentV2 {
		changes = append(changes, fmt.Sprintf("client.codex.optimize-multi-agent-v2: %t -> %t", oldCfg.Client.Codex.OptimizeMultiAgentV2, newCfg.Client.Codex.OptimizeMultiAgentV2))
	}
	if oldCfg.Client.Codex.WebSearchResultCache.Enabled != newCfg.Client.Codex.WebSearchResultCache.Enabled {
		changes = append(changes, fmt.Sprintf("client.codex.web-search-result-cache.enabled: %t -> %t", oldCfg.Client.Codex.WebSearchResultCache.Enabled, newCfg.Client.Codex.WebSearchResultCache.Enabled))
	}
	if strings.TrimSpace(oldCfg.Client.Codex.WebSearchResultCache.TTL) != strings.TrimSpace(newCfg.Client.Codex.WebSearchResultCache.TTL) {
		changes = append(changes, fmt.Sprintf("client.codex.web-search-result-cache.ttl: %s -> %s", strings.TrimSpace(oldCfg.Client.Codex.WebSearchResultCache.TTL), strings.TrimSpace(newCfg.Client.Codex.WebSearchResultCache.TTL)))
	}
	if oldCfg.Client.Codex.WebSearchResultCache.Limit != newCfg.Client.Codex.WebSearchResultCache.Limit {
		changes = append(changes, fmt.Sprintf("client.codex.web-search-result-cache.limit: %d -> %d", oldCfg.Client.Codex.WebSearchResultCache.Limit, newCfg.Client.Codex.WebSearchResultCache.Limit))
	}
	if oldCfg.Codex.OrphanDelegationCompatibility != newCfg.Codex.OrphanDelegationCompatibility {
		changes = append(changes, fmt.Sprintf("codex.orphan-delegation-compatibility: %t -> %t", oldCfg.Codex.OrphanDelegationCompatibility, newCfg.Codex.OrphanDelegationCompatibility))
	}
	if oldCfg.Claude.ToolSearch != newCfg.Claude.ToolSearch {
		changes = append(changes, fmt.Sprintf("claude.tool-search: %t -> %t", oldCfg.Claude.ToolSearch, newCfg.Claude.ToolSearch))
	}
	if oldCfg.XAI.InjectXSearch != newCfg.XAI.InjectXSearch {
		changes = append(changes, fmt.Sprintf("xai.inject-x-search: %t -> %t", oldCfg.XAI.InjectXSearch, newCfg.XAI.InjectXSearch))
	}
	if !reflect.DeepEqual(oldCfg.Payload, newCfg.Payload) {
		changes = appendPayloadConfigChanges(changes, oldCfg.Payload, newCfg.Payload)
	}

	// API keys (redacted) and counts
	if len(oldCfg.APIKeys) != len(newCfg.APIKeys) {
		changes = append(changes, fmt.Sprintf("api-keys count: %d -> %d", len(oldCfg.APIKeys), len(newCfg.APIKeys)))
	} else if !reflect.DeepEqual(trimStrings(oldCfg.APIKeys), trimStrings(newCfg.APIKeys)) {
		changes = append(changes, "api-keys: values updated (count unchanged, redacted)")
	}
	// Claude keys (do not print key material)
	if len(oldCfg.ClaudeKey) != len(newCfg.ClaudeKey) {
		changes = append(changes, fmt.Sprintf("claude-api-key count: %d -> %d", len(oldCfg.ClaudeKey), len(newCfg.ClaudeKey)))
	} else {
		for i := range oldCfg.ClaudeKey {
			o := oldCfg.ClaudeKey[i]
			n := newCfg.ClaudeKey[i]
			if strings.TrimSpace(o.BaseURL) != strings.TrimSpace(n.BaseURL) {
				changes = append(changes, fmt.Sprintf("claude[%d].base-url: %s -> %s", i, formatURL(o.BaseURL), formatURL(n.BaseURL)))
			}
			if strings.TrimSpace(o.ProxyURL) != strings.TrimSpace(n.ProxyURL) {
				changes = append(changes, fmt.Sprintf("claude[%d].proxy-url: %s -> %s", i, formatProxyURL(o.ProxyURL), formatProxyURL(n.ProxyURL)))
			}
			changes = appendConnectionPoolChanges(changes, fmt.Sprintf("claude[%d].connection-pool", i), o.ConnectionPool, n.ConnectionPool)
			changes = appendOptionalBoolChange(changes, fmt.Sprintf("claude[%d].disable-cooling", i), o.DisableCooling, n.DisableCooling)
			if strings.TrimSpace(o.APIKey) != strings.TrimSpace(n.APIKey) {
				changes = append(changes, fmt.Sprintf("claude[%d].api-key: updated", i))
			}
			if !equalStringMap(o.Headers, n.Headers) {
				changes = append(changes, fmt.Sprintf("claude[%d].headers: updated", i))
			}
			oldModels := SummarizeClaudeModels(o.Models)
			newModels := SummarizeClaudeModels(n.Models)
			if oldModels.hash != newModels.hash {
				changes = append(changes, fmt.Sprintf("claude[%d].models: updated (%d -> %d entries)", i, oldModels.count, newModels.count))
			}
			oldExcluded := SummarizeExcludedModels(o.ExcludedModels)
			newExcluded := SummarizeExcludedModels(n.ExcludedModels)
			if oldExcluded.hash != newExcluded.hash {
				changes = append(changes, fmt.Sprintf("claude[%d].excluded-models: updated (%d -> %d entries)", i, oldExcluded.count, newExcluded.count))
			}
			if o.RebuildMidSystemMessage != n.RebuildMidSystemMessage {
				changes = append(changes, fmt.Sprintf("claude[%d].rebuild-mid-system-message: %t -> %t", i, o.RebuildMidSystemMessage, n.RebuildMidSystemMessage))
			}
			if o.NativeToolSearch != n.NativeToolSearch {
				changes = append(changes, fmt.Sprintf("claude[%d].native-tool-search: %t -> %t", i, o.NativeToolSearch, n.NativeToolSearch))
			}
			if strings.TrimSpace(o.FingerprintProfile) != strings.TrimSpace(n.FingerprintProfile) {
				changes = append(changes, fmt.Sprintf("claude[%d].fingerprint-profile: %s -> %s", i, strings.TrimSpace(o.FingerprintProfile), strings.TrimSpace(n.FingerprintProfile)))
			}
			changes = appendOptionalIntChange(changes, fmt.Sprintf("claude[%d].request-retry", i), o.RequestRetry, n.RequestRetry)
			if o.Cloak != nil && n.Cloak != nil {
				if strings.TrimSpace(o.Cloak.Mode) != strings.TrimSpace(n.Cloak.Mode) {
					changes = append(changes, fmt.Sprintf("claude[%d].cloak.mode: %s -> %s", i, o.Cloak.Mode, n.Cloak.Mode))
				}
				if o.Cloak.StrictMode != n.Cloak.StrictMode {
					changes = append(changes, fmt.Sprintf("claude[%d].cloak.strict-mode: %t -> %t", i, o.Cloak.StrictMode, n.Cloak.StrictMode))
				}
				if len(o.Cloak.SensitiveWords) != len(n.Cloak.SensitiveWords) {
					changes = append(changes, fmt.Sprintf("claude[%d].cloak.sensitive-words: %d -> %d", i, len(o.Cloak.SensitiveWords), len(n.Cloak.SensitiveWords)))
				}
			}
		}
	}

	// Codex keys (do not print key material)
	if len(oldCfg.CodexKey) != len(newCfg.CodexKey) {
		changes = append(changes, fmt.Sprintf("codex-api-key count: %d -> %d", len(oldCfg.CodexKey), len(newCfg.CodexKey)))
	} else {
		for i := range oldCfg.CodexKey {
			o := oldCfg.CodexKey[i]
			n := newCfg.CodexKey[i]
			if strings.TrimSpace(o.BaseURL) != strings.TrimSpace(n.BaseURL) {
				changes = append(changes, fmt.Sprintf("codex[%d].base-url: %s -> %s", i, formatURL(o.BaseURL), formatURL(n.BaseURL)))
			}
			if strings.TrimSpace(o.ProxyURL) != strings.TrimSpace(n.ProxyURL) {
				changes = append(changes, fmt.Sprintf("codex[%d].proxy-url: %s -> %s", i, formatProxyURL(o.ProxyURL), formatProxyURL(n.ProxyURL)))
			}
			changes = appendConnectionPoolChanges(changes, fmt.Sprintf("codex[%d].connection-pool", i), o.ConnectionPool, n.ConnectionPool)
			if o.Websockets != n.Websockets {
				changes = append(changes, fmt.Sprintf("codex[%d].websockets: %t -> %t", i, o.Websockets, n.Websockets))
			}
			if o.AlphaSearch != n.AlphaSearch {
				changes = append(changes, fmt.Sprintf("codex[%d].alpha-search: %t -> %t", i, o.AlphaSearch, n.AlphaSearch))
			}
			changes = appendOptionalBoolChange(changes, fmt.Sprintf("codex[%d].disable-cooling", i), o.DisableCooling, n.DisableCooling)
			changes = appendOptionalBoolChange(changes, fmt.Sprintf("codex[%d].disable-codex-cloaking", i), o.DisableCodexCloaking, n.DisableCodexCloaking)
			if strings.TrimSpace(o.APIKey) != strings.TrimSpace(n.APIKey) {
				changes = append(changes, fmt.Sprintf("codex[%d].api-key: updated", i))
			}
			if !equalStringMap(o.Headers, n.Headers) {
				changes = append(changes, fmt.Sprintf("codex[%d].headers: updated", i))
			}
			oldModels := SummarizeCodexModels(o.Models)
			newModels := SummarizeCodexModels(n.Models)
			if oldModels.hash != newModels.hash {
				changes = append(changes, fmt.Sprintf("codex[%d].models: updated (%d -> %d entries)", i, oldModels.count, newModels.count))
			}
			oldExcluded := SummarizeExcludedModels(o.ExcludedModels)
			newExcluded := SummarizeExcludedModels(n.ExcludedModels)
			if oldExcluded.hash != newExcluded.hash {
				changes = append(changes, fmt.Sprintf("codex[%d].excluded-models: updated (%d -> %d entries)", i, oldExcluded.count, newExcluded.count))
			}
			changes = appendOptionalIntChange(changes, fmt.Sprintf("codex[%d].request-retry", i), o.RequestRetry, n.RequestRetry)
		}
	}

	// xAI keys (do not print key material)
	if len(oldCfg.XAIKey) != len(newCfg.XAIKey) {
		changes = append(changes, fmt.Sprintf("xai-api-key count: %d -> %d", len(oldCfg.XAIKey), len(newCfg.XAIKey)))
	} else {
		for i := range oldCfg.XAIKey {
			o := oldCfg.XAIKey[i]
			n := newCfg.XAIKey[i]
			if strings.TrimSpace(o.BaseURL) != strings.TrimSpace(n.BaseURL) {
				changes = append(changes, fmt.Sprintf("xai[%d].base-url: %s -> %s", i, formatURL(o.BaseURL), formatURL(n.BaseURL)))
			}
			if strings.TrimSpace(o.ProxyURL) != strings.TrimSpace(n.ProxyURL) {
				changes = append(changes, fmt.Sprintf("xai[%d].proxy-url: %s -> %s", i, formatProxyURL(o.ProxyURL), formatProxyURL(n.ProxyURL)))
			}
			changes = appendConnectionPoolChanges(changes, fmt.Sprintf("xai[%d].connection-pool", i), o.ConnectionPool, n.ConnectionPool)
			if o.Priority != n.Priority {
				changes = append(changes, fmt.Sprintf("xai[%d].priority: %d -> %d", i, o.Priority, n.Priority))
			}
			if o.Websockets != n.Websockets {
				changes = append(changes, fmt.Sprintf("xai[%d].websockets: %t -> %t", i, o.Websockets, n.Websockets))
			}
			changes = appendOptionalBoolChange(changes, fmt.Sprintf("xai[%d].disable-cooling", i), o.DisableCooling, n.DisableCooling)
			changes = appendOptionalIntChange(changes, fmt.Sprintf("xai[%d].request-retry", i), o.RequestRetry, n.RequestRetry)
			if strings.TrimSpace(o.APIKey) != strings.TrimSpace(n.APIKey) {
				changes = append(changes, fmt.Sprintf("xai[%d].api-key: updated", i))
			}
			if !equalStringMap(o.Headers, n.Headers) {
				changes = append(changes, fmt.Sprintf("xai[%d].headers: updated", i))
			}
			oldModels := SummarizeCodexModels(o.Models)
			newModels := SummarizeCodexModels(n.Models)
			if oldModels.hash != newModels.hash {
				changes = append(changes, fmt.Sprintf("xai[%d].models: updated (%d -> %d entries)", i, oldModels.count, newModels.count))
			}
			oldExcluded := SummarizeExcludedModels(o.ExcludedModels)
			newExcluded := SummarizeExcludedModels(n.ExcludedModels)
			if oldExcluded.hash != newExcluded.hash {
				changes = append(changes, fmt.Sprintf("xai[%d].excluded-models: updated (%d -> %d entries)", i, oldExcluded.count, newExcluded.count))
			}
		}
	}

	if entries, _ := DiffOAuthExcludedModelChanges(oldCfg.OAuthExcludedModels, newCfg.OAuthExcludedModels); len(entries) > 0 {
		changes = append(changes, entries...)
	}
	if entries, _ := DiffOAuthModelAliasChanges(oldCfg.OAuthModelAlias, newCfg.OAuthModelAlias); len(entries) > 0 {
		changes = append(changes, entries...)
	}
	if entries, _ := DiffOAuthRequestScopedErrorsChanges(oldCfg.OAuthRequestScopedErrors, newCfg.OAuthRequestScopedErrors); len(entries) > 0 {
		changes = append(changes, entries...)
	}
	if entries, _ := DiffOAuthSettingsChanges(oldCfg.OAuthSettings, newCfg.OAuthSettings); len(entries) > 0 {
		changes = append(changes, entries...)
	}

	// Remote management (never print the key)
	if oldCfg.RemoteManagement.AllowRemote != newCfg.RemoteManagement.AllowRemote {
		changes = append(changes, fmt.Sprintf("remote-management.allow-remote: %t -> %t", oldCfg.RemoteManagement.AllowRemote, newCfg.RemoteManagement.AllowRemote))
	}
	if oldCfg.RemoteManagement.SecretKey != newCfg.RemoteManagement.SecretKey {
		switch {
		case oldCfg.RemoteManagement.SecretKey == "" && newCfg.RemoteManagement.SecretKey != "":
			changes = append(changes, "remote-management.secret-key: created")
		case oldCfg.RemoteManagement.SecretKey != "" && newCfg.RemoteManagement.SecretKey == "":
			changes = append(changes, "remote-management.secret-key: deleted")
		default:
			changes = append(changes, "remote-management.secret-key: updated")
		}
	}

	// OpenAI compatibility providers (summarized)
	if compat := DiffOpenAICompatibility(oldCfg.OpenAICompatibility, newCfg.OpenAICompatibility); len(compat) > 0 {
		changes = append(changes, "openai-compatibility:")
		for _, c := range compat {
			changes = append(changes, "  "+c)
		}
	}

	return changes
}

func trimStrings(in []string) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = strings.TrimSpace(in[i])
	}
	return out
}

func appendPayloadConfigChanges(changes []string, oldPayload, newPayload config.PayloadConfig) []string {
	changes = appendPayloadRuleChanges(changes, "default", oldPayload.Default, newPayload.Default)
	changes = appendPayloadRuleChanges(changes, "default-raw", oldPayload.DefaultRaw, newPayload.DefaultRaw)
	changes = appendPayloadRuleChanges(changes, "override", oldPayload.Override, newPayload.Override)
	changes = appendPayloadRuleChanges(changes, "override-raw", oldPayload.OverrideRaw, newPayload.OverrideRaw)
	changes = appendPayloadFilterRuleChanges(changes, "filter", oldPayload.Filter, newPayload.Filter)
	return changes
}

func appendPayloadRuleChanges(changes []string, section string, oldRules, newRules []config.PayloadRule) []string {
	if reflect.DeepEqual(oldRules, newRules) {
		return changes
	}
	return append(changes, fmt.Sprintf("payload.%s: updated (%d -> %d rules)", section, len(oldRules), len(newRules)))
}

func appendPayloadFilterRuleChanges(changes []string, section string, oldRules, newRules []config.PayloadFilterRule) []string {
	if reflect.DeepEqual(oldRules, newRules) {
		return changes
	}
	return append(changes, fmt.Sprintf("payload.%s: updated (%d -> %d rules)", section, len(oldRules), len(newRules)))
}

func appendConnectionPoolChanges(changes []string, prefix string, oldPool, newPool *config.ProxyConnectionPoolConfig) []string {
	if oldPool == nil && newPool == nil {
		return changes
	}
	var oldCfg, newCfg config.ProxyConnectionPoolConfig
	if oldPool != nil {
		oldCfg = *oldPool
	}
	if newPool != nil {
		newCfg = *newPool
	}
	oldEnabled := formatConnectionPoolEnabled(oldPool)
	newEnabled := formatConnectionPoolEnabled(newPool)
	if oldEnabled != newEnabled {
		changes = append(changes, fmt.Sprintf("%s.enabled: %s -> %s", prefix, oldEnabled, newEnabled))
	}
	if strings.TrimSpace(oldCfg.IdleConnTimeout) != strings.TrimSpace(newCfg.IdleConnTimeout) {
		changes = append(changes, fmt.Sprintf("%s.idle-conn-timeout: %q -> %q", prefix, strings.TrimSpace(oldCfg.IdleConnTimeout), strings.TrimSpace(newCfg.IdleConnTimeout)))
	}
	changes = appendOptionalIntChange(changes, prefix+".max-idle-conns-per-host", oldCfg.MaxIdleConnsPerHost, newCfg.MaxIdleConnsPerHost)
	return changes
}

func formatConnectionPoolEnabled(pool *config.ProxyConnectionPoolConfig) string {
	if pool == nil {
		return "<unset>"
	}
	if pool.Enabled == nil {
		return "true"
	}
	return fmt.Sprintf("%t", *pool.Enabled)
}

func appendOptionalIntChange(changes []string, field string, oldVal, newVal *int) []string {
	if optionalIntEqual(oldVal, newVal) {
		return changes
	}
	return append(changes, fmt.Sprintf("%s: %s -> %s", field, formatOptionalInt(oldVal), formatOptionalInt(newVal)))
}

func appendOptionalBoolChange(changes []string, field string, oldVal, newVal *bool) []string {
	if optionalBoolEqual(oldVal, newVal) {
		return changes
	}
	return append(changes, fmt.Sprintf("%s: %s -> %s", field, formatOptionalBool(oldVal), formatOptionalBool(newVal)))
}

func optionalBoolEqual(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func formatOptionalBool(value *bool) string {
	if value == nil {
		return "inherit"
	}
	return fmt.Sprintf("%t", *value)
}

func optionalIntEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func formatOptionalInt(v *int) string {
	if v == nil {
		return "<unset>"
	}
	return strconv.Itoa(*v)
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func formatProxyURL(raw string) string {
	return formatURL(raw)
}

func formatURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "<none>"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "<redacted>"
	}
	host := strings.TrimSpace(parsed.Host)
	scheme := strings.TrimSpace(parsed.Scheme)
	if host == "" {
		// Allow host:port style without scheme.
		parsed2, err2 := url.Parse("http://" + trimmed)
		if err2 == nil {
			host = strings.TrimSpace(parsed2.Host)
		}
		scheme = ""
	}
	if host == "" {
		return "<redacted>"
	}
	if scheme == "" {
		return host
	}
	return scheme + "://" + host
}
