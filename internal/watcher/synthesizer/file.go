package synthesizer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/udit19199/relay/internal/auth/codex"
	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
)

// FileSynthesizer generates Auth entries from OAuth JSON files.
// It handles file-based authentication.
type FileSynthesizer struct{}

// NewFileSynthesizer creates a new FileSynthesizer instance.
func NewFileSynthesizer() *FileSynthesizer {
	return &FileSynthesizer{}
}

// Synthesize generates Auth entries from auth files in the auth directory.
func (s *FileSynthesizer) Synthesize(ctx *SynthesisContext) ([]*coreauth.Auth, error) {
	out := make([]*coreauth.Auth, 0, 16)
	if ctx == nil || ctx.AuthDir == "" {
		return out, nil
	}

	entries, err := os.ReadDir(ctx.AuthDir)
	if err != nil {
		// A missing directory holds no auths; any other read failure keeps the loaded ones.
		if !errors.Is(err, fs.ErrNotExist) {
			ctx.RecordIncompleteFile(ctx.AuthDir)
		}
		return out, nil
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		full := filepath.Join(ctx.AuthDir, name)
		data, errRead := os.ReadFile(full)
		if errRead != nil {
			if !errors.Is(errRead, fs.ErrNotExist) {
				ctx.RecordIncompleteFile(full)
			}
			continue
		}
		if !json.Valid(data) {
			ctx.RecordIncompleteFile(full)
			continue
		}
		auths, errSynthesize := synthesizeFileAuths(ctx, full, data)
		if errSynthesize != nil {
			log.WithError(errSynthesize).Warnf("skipping auth file %s", name)
			continue
		}
		if len(auths) == 0 {
			continue
		}
		out = append(out, auths...)
	}
	return out, nil
}

// SynthesizeAuthFile generates Auth entries for one auth JSON file payload.
// It shares exactly the same mapping behavior as FileSynthesizer.Synthesize.
func SynthesizeAuthFile(ctx *SynthesisContext, fullPath string, data []byte) ([]*coreauth.Auth, error) {
	return synthesizeFileAuths(ctx, fullPath, data)
}

func synthesizeFileAuths(ctx *SynthesisContext, fullPath string, data []byte) ([]*coreauth.Auth, error) {
	if ctx == nil || len(data) == 0 {
		return nil, nil
	}
	now := ctx.Now
	var metadata map[string]any
	if errUnmarshal := json.Unmarshal(data, &metadata); errUnmarshal != nil {
		if ctx.StrictFileParsing {
			return nil, fmt.Errorf("parse auth file: %w", errUnmarshal)
		}
		return nil, nil
	}
	coreauth.NormalizeCredentialMetadata(metadata)
	if errWeight := coreauth.ValidateAuthWeight(&coreauth.Auth{Metadata: metadata}); errWeight != nil {
		return nil, fmt.Errorf("invalid weight in %s: %w", filepath.Base(fullPath), errWeight)
	}
	// Invalid per-account rules must not drop an existing credential: keep the valid
	// entries and warn. Write entry points (upload and PATCH) reject them before persisting.
	perAccountExcluded, errExcluded := ExtractExcludedModelsFromMetadata(metadata)
	if errExcluded != nil {
		log.WithError(errExcluded).Warnf("ignoring invalid excluded models in auth file %s", filepath.Base(fullPath))
	}
	t, _ := metadata["type"].(string)
	provider := strings.ToLower(strings.TrimSpace(t))
	if provider == "" || removedGoogleAuthProvider(provider) {
		return nil, nil
	}
	label := provider
	if email, _ := metadata["email"].(string); email != "" {
		label = email
	}
	// Use relative path under authDir as ID to stay consistent with the file-based token store.
	id := fullPath
	if strings.TrimSpace(ctx.AuthDir) != "" {
		if rel, errRel := filepath.Rel(ctx.AuthDir, fullPath); errRel == nil && rel != "" {
			id = rel
		}
	}
	if runtime.GOOS == "windows" {
		id = strings.ToLower(id)
	}

	proxyURL := ""
	if p, ok := metadata["proxy_url"].(string); ok {
		proxyURL = p
	}

	prefix := ""
	if rawPrefix, ok := metadata["prefix"].(string); ok {
		trimmed := strings.TrimSpace(rawPrefix)
		trimmed = strings.Trim(trimmed, "/")
		if trimmed != "" && !strings.Contains(trimmed, "/") {
			prefix = trimmed
		}
	}

	disabled, _ := metadata["disabled"].(bool)
	status := coreauth.StatusActive
	if disabled {
		status = coreauth.StatusDisabled
	}

	perAccountModelAliases := extractOAuthModelAliasesFromMetadata(metadata)

	a := &coreauth.Auth{
		ID:       id,
		FileName: filepath.Base(fullPath),
		Provider: provider,
		Label:    label,
		Prefix:   prefix,
		Status:   status,
		Disabled: disabled,
		Attributes: map[string]string{
			coreauth.AttributeSource:        fullPath,
			coreauth.AttributePath:          fullPath,
			coreauth.AttributeSourceBackend: coreauth.AuthSourceFile,
		},
		ProxyURL:  proxyURL,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Read priority from auth file.
	coreauth.ApplyAuthPriorityMetadata(a, metadata)
	if errWeight := coreauth.ApplyAuthWeightMetadata(a, metadata); errWeight != nil {
		return nil, fmt.Errorf("invalid auth weight in %s: %w", filepath.Base(fullPath), errWeight)
	}
	// Read note from auth file.
	if rawNote, ok := metadata["note"]; ok {
		if note, isStr := rawNote.(string); isStr {
			if trimmed := strings.TrimSpace(note); trimmed != "" {
				a.Attributes["note"] = trimmed
			}
		}
	}
	coreauth.ApplyCustomHeadersFromMetadata(a)
	coreauth.SetOAuthModelAliasesAttribute(a, perAccountModelAliases)
	ApplyAuthExcludedModelsMeta(a, perAccountExcluded, "oauth")
	applyFingerprintProfileAttribute(a, metadata)
	if provider == openCodeGoProvider {
		applyOpenCodeGoAttributes(a, metadata)
	}
	// For codex auth files, extract plan_type from metadata or JWT id_token.
	if provider == "codex" {
		if ptRaw, ok := metadata["plan_type"].(string); ok && strings.TrimSpace(ptRaw) != "" {
			a.Attributes["plan_type"] = strings.TrimSpace(ptRaw)
		} else if idTokenRaw, ok := metadata["id_token"].(string); ok && strings.TrimSpace(idTokenRaw) != "" {
			if claims, errParse := codex.ParseJWTToken(idTokenRaw); errParse == nil && claims != nil {
				a.Attributes["plan_type"] = claims.GetPlanType()
			} else {
				a.Attributes["plan_type"] = codex.DefaultPlanType
			}
		}
	}
	a.BindFileSnapshot(fullPath, data)
	return []*coreauth.Auth{a}, nil
}

const (
	openCodeGoProvider = "opencode-go"
	openCodeGoBaseURL  = "https://opencode.ai/inference/go/v1"
)

// applyOpenCodeGoAttributes lets an OpenCode Go auth file serve inference through
// the OpenAI-compatible executor bound to provider "opencode-go". Its models are
// exposed only as "opencode-go/<model>" so they never shadow other providers.
func applyOpenCodeGoAttributes(a *coreauth.Auth, metadata map[string]any) {
	if key, ok := metadata["api_key"].(string); ok && strings.TrimSpace(key) != "" {
		a.Attributes["api_key"] = strings.TrimSpace(key)
	}
	a.Attributes["base_url"] = openCodeGoBaseURL
	if bu, ok := metadata["base_url"].(string); ok && strings.TrimSpace(bu) != "" {
		a.Attributes["base_url"] = strings.TrimSpace(bu)
	}
	// OpenCode Go asks clients to identify themselves and rejects requests without
	// a session header; "$CPA-SESSION-ID" expands to Relay's per-conversation ID.
	// A "headers" entry in the file wins over these defaults.
	defaults := map[string]string{"User-Agent": "relay", "X-Opencode-Session": "$CPA-SESSION-ID"}
	for name := range a.Attributes {
		for header := range defaults {
			if strings.EqualFold(name, "header:"+header) {
				delete(defaults, header)
			}
		}
	}
	for header, value := range defaults {
		a.Attributes["header:"+header] = value
	}
	if a.Prefix == "" {
		a.Prefix = openCodeGoProvider
	}
}

func extractOAuthModelAliasesFromMetadata(metadata map[string]any) []config.OAuthModelAlias {
	if metadata == nil {
		return nil
	}
	var combined []config.OAuthModelAlias
	for _, key := range []string{"model_aliases", "model-aliases", "models"} {
		raw, ok := metadata[key]
		if !ok || raw == nil {
			continue
		}
		data, errMarshal := json.Marshal(raw)
		if errMarshal != nil {
			continue
		}
		var items []json.RawMessage
		if errUnmarshal := json.Unmarshal(data, &items); errUnmarshal != nil {
			continue
		}
		for _, item := range items {
			var alias config.OAuthModelAlias
			if errUnmarshal := json.Unmarshal(item, &alias); errUnmarshal != nil {
				continue
			}
			combined = append(combined, alias)
		}
	}
	if len(combined) == 0 {
		return nil
	}
	cfg := config.Config{
		OAuthModelAlias: map[string][]config.OAuthModelAlias{
			"auth": combined,
		},
	}
	cfg.SanitizeOAuthModelAlias()
	return cfg.OAuthModelAlias["auth"]
}

// ExtractExcludedModelsFromMetadata reads per-account excluded models from the OAuth JSON metadata.
// "excluded_models" is canonical; "excluded-models" remains a legacy alias. The value must be a
// string array; a single string is also accepted for older hand-written files and is split on
// commas, because model IDs never contain commas. Any other type, or a non-string array element,
// is an error. For non-string elements the valid string entries are still returned with the
// error, so lenient readers can keep the usable rules while strict writers reject the value.
func ExtractExcludedModelsFromMetadata(metadata map[string]any) ([]string, error) {
	if metadata == nil {
		return nil, nil
	}
	key := "excluded_models"
	raw, ok := metadata[key]
	if !ok {
		key = "excluded-models"
		raw, ok = metadata[key]
	}
	if !ok || raw == nil {
		return nil, nil
	}
	var stringSlice []string
	var errInvalid error
	switch v := raw.(type) {
	case string:
		stringSlice = strings.Split(v, ",")
	case []string:
		stringSlice = v
	case []interface{}:
		stringSlice = make([]string, 0, len(v))
		for index, item := range v {
			s, okString := item.(string)
			if !okString {
				if errInvalid == nil {
					errInvalid = fmt.Errorf("metadata %s[%d] must be a string, got %T", key, index, item)
				}
				continue
			}
			stringSlice = append(stringSlice, s)
		}
	default:
		return nil, fmt.Errorf("metadata %s must be a string array, got %T", key, raw)
	}
	result := make([]string, 0, len(stringSlice))
	for _, s := range stringSlice {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result, errInvalid
}

func removedGoogleAuthProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gemini", "gemini-cli", "gemini-interactions", "vertex", "aistudio", "antigravity", "google":
		return true
	default:
		return false
	}
}
