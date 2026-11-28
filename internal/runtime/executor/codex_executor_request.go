package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	neturl "net/url"
	"runtime"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	log "github.com/sirupsen/logrus"
	"github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/misc"
	"github.com/udit19199/relay/internal/registry"
	"github.com/udit19199/relay/internal/runtime/executor/helps"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	"github.com/udit19199/relay/internal/thinking"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
	"github.com/udit19199/relay/internal/util"
)

const (
	codexUserAgent           = "codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.154.0)"
	codexOriginator          = "codex-tui"
	codexResponsesLiteHeader = "X-OpenAI-Internal-Codex-Responses-Lite"
)

var dataTag = []byte("data:")

func translateCodexRequestPair(from, to translatorreg.Format, model string, originalPayload, payload []byte, stream bool, preserveEmptyThinkingBlocks ...bool) ([]byte, []byte) {
	original, body, _ := translateCodexRequestPairContext(context.Background(), from, to, model, originalPayload, payload, stream, preserveEmptyThinkingBlocks...)
	return original, body
}

func translateCodexRequestPairContext(ctx context.Context, from, to translatorreg.Format, model string, originalPayload, payload []byte, stream bool, preserveEmptyThinkingBlocks ...bool) ([]byte, []byte, error) {
	isCompat := len(preserveEmptyThinkingBlocks) > 0 && preserveEmptyThinkingBlocks[0]
	if ctx == nil {
		ctx = context.Background()
	}
	translate := func(raw []byte) ([]byte, error) {
		if isCompat && from == translatorreg.FormatClaude && to == translatorreg.FormatCodex {
			return helps.TranslateRequestReturningError(ctx, nil, nil, from, to, model, raw, stream, true)
		}
		translated := translatorreg.TranslateRequestEnvelope(ctx, from, to, translatorreg.RequestEnvelope{Format: from, Model: model, Stream: stream, Body: raw})
		return translated.Body, translated.Err
	}
	if bytes.Equal(originalPayload, payload) {
		body, err := translate(payload)
		return body, body, err
	}
	originalTranslated, _ := translate(originalPayload)
	body, err := translate(payload)
	return originalTranslated, body, err
}

// codexMinOutputTokens is the smallest integer max_output_tokens value the ChatGPT
// Codex backend accepts; smaller integers are rejected with invalid_request_error.
const codexMinOutputTokens = 16

// normalizeCodexOutputBudgetDoc raises small positive integer output budgets to the
// Codex minimum. It judges the translated payload (max_output_tokens) rather than the
// client payload so every source format and budget field (max_tokens,
// max_completion_tokens, generation_config, ...) is covered uniformly. Budgets that
// are missing, non-numeric, non-integral, or <= 0 are left untouched for the upstream
// to reject. This runs before user payload rules so overrides and filters stay final.
func normalizeCodexOutputBudgetDoc(doc *ojson.Doc) {
	if doc == nil {
		return
	}
	translated := doc.Get("max_output_tokens")
	if translated.Type != ojson.Number {
		return
	}
	tokens, errFloat := strconv.ParseFloat(translated.Raw, 64)
	if errFloat != nil || tokens < 1 || tokens >= codexMinOutputTokens || tokens != math.Trunc(tokens) {
		return
	}
	_ = doc.Set("max_output_tokens", codexMinOutputTokens)
}

// PrepareRequest injects Codex credentials into the outgoing HTTP request.
func (e *CodexExecutor) PrepareRequest(req *http.Request, auth *coreauth.Auth) error {
	if req == nil {
		return nil
	}
	apiKey, _ := codexCredsForRequest(req.Context(), auth, req.Header)
	if codexUseSIWCForRequest(req.Context(), auth, req.Header) && req.URL != nil && req.URL.Scheme == "https" &&
		req.URL.Host == "chatgpt.com" && req.URL.Path == "/backend-api/codex/alpha/search" {
		// ChatGPT Alpha Search requires the original OAuth partition, not SIWC.
		apiKey, _ = auth.Metadata["access_token"].(string)
		if strings.TrimSpace(apiKey) == "" {
			return statusErr{code: http.StatusUnauthorized, msg: "codex executor: original OAuth access_token is required for ChatGPT Alpha Search"}
		}
	} else if errSIWC := ensureCodexSIWCCredentialsForRequest(req.Context(), auth, apiKey, req.Header); errSIWC != nil {
		return errSIWC
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		req.Header.Del("Authorization")
	}
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
	alignCodexContentEncoding(req)
	return nil
}

// HttpRequest injects Codex credentials into the request and executes it.
func (e *CodexExecutor) HttpRequest(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("codex executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := helps.NewUtlsHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

func (e *CodexExecutor) cacheHelper(ctx context.Context, from translatorreg.Format, url string, req coreexecutor.Request, rawJSON []byte, auth *coreauth.Auth, headerSets ...http.Header) (*http.Request, []byte, error) {
	doc := ojson.NewDoc(rawJSON)
	return e.cacheHelperDoc(ctx, from, url, req, doc, auth, headerSets...)
}

func (e *CodexExecutor) cacheHelperDoc(ctx context.Context, from translatorreg.Format, url string, req coreexecutor.Request, doc *ojson.Doc, auth *coreauth.Auth, headerSets ...http.Header) (*http.Request, []byte, error) {
	var headers http.Header
	if len(headerSets) > 0 {
		headers = headerSets[0]
	}
	var cache helps.CodexCache
	if sourceFormatEqual(from, translatorreg.FormatClaude) {
		modelName := ""
		if doc != nil {
			modelName = strings.TrimSpace(doc.Get("model").String())
		}
		if modelName == "" {
			modelName = thinking.ParseSuffix(req.Model).ModelName
		}
		cached, ok, errCache := helps.ClaudeCodePromptCache(ctx, modelName, req.Payload, headers)
		if errCache != nil {
			return nil, nil, errCache
		}
		if ok {
			cache = cached
		}
	} else if sourceFormatEqual(from, translatorreg.FormatOpenAIResponse) {
		promptCacheKey := ojson.GetBytesNoCopy(req.Payload, "prompt_cache_key")
		if promptCacheKey.Exists() {
			cache.ID = strings.Clone(promptCacheKey.String())
		}
	} else if sourceFormatEqual(from, translatorreg.FormatOpenAI) {
		if promptCacheKey := ojson.GetBytesNoCopy(req.Payload, "prompt_cache_key"); promptCacheKey.Exists() {
			cache.ID = strings.Clone(strings.TrimSpace(promptCacheKey.String()))
		}
		if cache.ID == "" {
			cache.ID = helps.ProviderSessionUUID("codex", req.Metadata)
		}
		if cache.ID == "" {
			if apiKey := strings.TrimSpace(helps.APIKeyFromContext(ctx)); apiKey != "" {
				cache.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte("cli-proxy-api:codex:prompt-cache:"+apiKey)).String()
			}
		}
	}
	if cache.ID == "" {
		cache.ID = helps.ProviderSessionUUID("codex", req.Metadata)
	}

	if cache.ID != "" {
		helps.SetStringIfDifferentDoc(doc, "prompt_cache_key", cache.ID)
	}
	helps.SanitizeCodexInputItemIDsDoc(doc)
	helps.FinalizePayloadDoc(ctx, doc)
	var rawJSON []byte
	if doc != nil {
		rawJSON = doc.Bytes()
	}
	wireBody := rawJSON
	if e.cfg != nil && e.cfg.Codex.UpstreamRequestCompression && auth != nil &&
		!codexAuthUsesAPIKey(auth) && !codexUseSIWCForRequest(ctx, auth, headers) &&
		len(rawJSON) >= e.cfg.Codex.RequestCompressionMinBytes() {
		parsedURL, errParse := neturl.Parse(url)
		if errParse == nil && strings.EqualFold(parsedURL.Hostname(), "chatgpt.com") && strings.HasPrefix(parsedURL.Path, "/backend-api/codex/") {
			encoder, errNewWriter := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
			if errNewWriter == nil {
				compressed := encoder.EncodeAll(rawJSON, nil)
				if errClose := encoder.Close(); errClose != nil {
					log.Errorf("codex executor: close request encoder error: %v", errClose)
				} else if len(compressed) < len(rawJSON) {
					wireBody = compressed
				}
			}
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(wireBody))
	if err != nil {
		return nil, nil, err
	}
	if len(wireBody) < len(rawJSON) {
		httpReq.Header.Set("Content-Encoding", "zstd")
	}
	alignCodexContentEncoding(httpReq)
	if httpReq.Header.Get("Accept-Encoding") == "" {
		httpReq.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	}
	if cache.ID != "" {
		httpReq.Header.Set("Session-Id", cache.ID)
	}
	return httpReq, rawJSON, nil
}

// alignCodexContentEncoding inspects a replay copy without consuming the live request body.
func alignCodexContentEncoding(req *http.Request) {
	if req == nil || req.GetBody == nil {
		return
	}
	body, errGetBody := req.GetBody()
	if errGetBody != nil || body == nil {
		return
	}
	defer func() {
		if errClose := body.Close(); errClose != nil {
			log.Errorf("codex executor: close request body copy error: %v", errClose)
		}
	}()
	prefix, errRead := io.ReadAll(io.LimitReader(body, 512))
	if errRead != nil {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	plain := bytes.TrimSpace(prefix)
	jsonPlaintext := len(plain) > 0 && (plain[0] == '{' || plain[0] == '[')
	multipartPlaintext := strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Type"))), "multipart/") && bytes.HasPrefix(plain, []byte("--"))
	if jsonPlaintext || multipartPlaintext {
		req.Header.Del("Content-Encoding")
		return
	}
	encodingCount := 0
	for _, value := range req.Header.Values("Content-Encoding") {
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if token != "" && !strings.EqualFold(token, "identity") {
				encodingCount++
			}
		}
	}
	// Magic identifies only the outermost layer, so trust declared encoding chains.
	if encodingCount > 1 {
		return
	}
	switch {
	case bytes.HasPrefix(prefix, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		req.Header.Set("Content-Encoding", "zstd")
	case bytes.HasPrefix(prefix, []byte{0x1f, 0x8b}):
		req.Header.Set("Content-Encoding", "gzip")
	default:
		// Our zstd encoder always emits its magic; preserve opaque non-zstd encodings.
		var remaining []string
		removedZstd := false
		for _, value := range req.Header.Values("Content-Encoding") {
			for _, token := range strings.Split(value, ",") {
				token = strings.TrimSpace(token)
				if strings.EqualFold(token, "zstd") {
					removedZstd = true
				} else if token != "" {
					remaining = append(remaining, token)
				}
			}
		}
		if removedZstd {
			req.Header.Del("Content-Encoding")
			if len(remaining) > 0 {
				req.Header.Set("Content-Encoding", strings.Join(remaining, ", "))
			}
		}
	}
}

func decodeCodexUpstreamResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	decoded, errDecode := helps.DecodeResponseBody(resp.Body, resp.Header)
	if errDecode != nil {
		return fmt.Errorf("codex executor: decode upstream response: %w", errDecode)
	}
	resp.Body = decoded
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	return nil
}

// doCodexRequest retries a rejected zstd body once without rerunning payload rules.
func doCodexRequest(client *http.Client, req *http.Request, upstreamBody []byte, prepareHeaders func(*http.Request), responseReady func(*http.Response)) (*http.Response, error) {
	alignCodexContentEncoding(req)
	compressed := req.Header.Get("Content-Encoding") == "zstd"
	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, errDo
	}
	if responseReady != nil {
		responseReady(resp)
	}
	if errDecode := decodeCodexUpstreamResponse(resp); errDecode != nil {
		return nil, errDecode
	}
	if !compressed || (resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnsupportedMediaType) {
		return resp, nil
	}
	if errClose := resp.Body.Close(); errClose != nil {
		log.Errorf("codex executor: close rejected response body error: %v", errClose)
	}
	plainReq, errNewRequest := http.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), bytes.NewReader(upstreamBody))
	if errNewRequest != nil {
		return nil, errNewRequest
	}
	plainReq.Header = req.Header.Clone()
	plainReq.Header.Del("Content-Encoding")
	prepareHeaders(plainReq)
	alignCodexContentEncoding(plainReq)
	resp, errDo = client.Do(plainReq)
	if errDo != nil {
		return nil, errDo
	}
	if responseReady != nil {
		responseReady(resp)
	}
	if errDecode := decodeCodexUpstreamResponse(resp); errDecode != nil {
		return nil, errDecode
	}
	return resp, nil
}

func applyCodexHeaders(r *http.Request, auth *coreauth.Auth, token string, stream bool, cfg *config.Config, clientHeaders ...http.Header) {
	var ginHeaders http.Header
	if len(clientHeaders) > 0 && clientHeaders[0] != nil {
		ginHeaders = clientHeaders[0]
	} else if ginCtx, ok := r.Context().Value("gin").(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
		ginHeaders = ginCtx.Request.Header
	}
	applyCodexHeadersWithSIWC(r, auth, codexUseSIWCForRequest(r.Context(), auth, ginHeaders), token, stream, cfg, ginHeaders)
}

// applyModelHeaderOverrides forces models.json config.override_header onto upstream headers.
func applyModelHeaderOverrides(headers http.Header, modelName string) {
	if headers == nil {
		return
	}
	overrides := registry.ModelOverrideHeaders(modelName)
	if len(overrides) == 0 {
		return
	}
	for key, value := range overrides {
		headers.Set(key, value)
	}
	if strings.Contains(headers.Get("User-Agent"), "Mac OS") && codexSessionHeaderValue(headers) == "" {
		headers.Set("Session_id", uuid.NewString())
	}
}

func codexSIWCUserAgent(cfg *config.Config) string {
	agentName := config.DefaultSignInWithChatGPTIdentifier
	if cfg != nil {
		agentName = cfg.Codex.EffectiveSignInWithChatGPTIdentifier()
	}
	return fmt.Sprintf("%s (%s; %s)", agentName, runtime.GOOS, runtime.GOARCH)
}

func applyCodexSIWCRequestBodyRules(body []byte, hasExplicitInstructions bool, opts ...coreexecutor.Options) []byte {
	doc := ojson.NewDoc(body)
	if !applyCodexSIWCRequestBodyRulesDoc(doc, hasExplicitInstructions, opts...) {
		return body
	}
	return doc.Bytes()
}

func applyCodexSIWCRequestBodyRulesDoc(doc *ojson.Doc, hasExplicitInstructions bool, opts ...coreexecutor.Options) bool {
	if doc == nil {
		return false
	}
	changed := helps.SetBoolIfDifferentDoc(doc, "store", false)
	if helps.SetBoolIfDifferentDoc(doc, "stream", true) {
		changed = true
	}
	if stripCodexSIWCUnsupportedFieldsDoc(doc, hasExplicitInstructions, opts...) {
		changed = true
	}
	return changed
}

func applyCodexSIWCWebsocketBodyRulesDoc(doc *ojson.Doc, hasExplicitInstructions bool, opts ...coreexecutor.Options) bool {
	if doc == nil {
		return false
	}
	changed := helps.SetBoolIfDifferentDoc(doc, "store", false)
	if doc.Delete("stream") == nil {
		changed = true
	}
	if doc.Delete("background") == nil {
		changed = true
	}
	if stripCodexSIWCUnsupportedFieldsDoc(doc, hasExplicitInstructions, opts...) {
		changed = true
	}
	return changed
}

func applyCodexSIWCCompactBodyRulesDoc(doc *ojson.Doc, hasExplicitInstructions bool, opts ...coreexecutor.Options) bool {
	if doc == nil {
		return false
	}
	changed := false
	if doc.Delete("stream") == nil {
		changed = true
	}
	if doc.Delete("store") == nil {
		changed = true
	}
	if stripCodexSIWCUnsupportedFieldsDoc(doc, hasExplicitInstructions, opts...) {
		changed = true
	}
	return changed
}

func stripCodexSIWCUnsupportedFields(body []byte, hasExplicitInstructions bool, opts ...coreexecutor.Options) []byte {
	doc := ojson.NewDoc(body)
	if !stripCodexSIWCUnsupportedFieldsDoc(doc, hasExplicitInstructions, opts...) {
		return body
	}
	return doc.Bytes()
}

func stripCodexSIWCUnsupportedFieldsDoc(doc *ojson.Doc, hasExplicitInstructions bool, opts ...coreexecutor.Options) bool {
	if doc == nil {
		return false
	}
	// SIWC subscriptions reject configuration_update input items, so degrade
	// them to the classic top-level reasoning.effort and accept the cache loss.
	// An explicit model thinking suffix is an operator override: the thinking
	// layer has already resolved it into the top-level effort, so only the
	// unsupported items are removed in that case.
	thinking.CollapseConfigurationUpdatesDoc(doc, len(opts) > 0 && opts[0].KeepTopLevelEffort)
	_ = doc.Delete("prompt_cache_retention")
	_ = doc.Delete("prompt_cache_options")
	_ = doc.Delete("max_completion_tokens")
	_ = doc.Delete("temperature")
	if !hasExplicitInstructions {
		if inst := doc.Get("instructions"); inst.Exists() && strings.TrimSpace(inst.String()) == "" {
			_ = doc.Delete("instructions")
		}
	}
	return true
}

func applyCodexHeadersFromSources(r *http.Request, auth *coreauth.Auth, token string, stream bool, cfg *config.Config, ginHeaders http.Header) {
	applyCodexHeadersWithSIWC(r, auth, codexUseSIWCForRequest(r.Context(), auth, ginHeaders), token, stream, cfg, ginHeaders)
}

func applyCodexHeadersWithSIWC(r *http.Request, auth *coreauth.Auth, useSIWC bool, token string, stream bool, cfg *config.Config, ginHeaders http.Header) {
	r.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(token) != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	} else {
		r.Header.Del("Authorization")
	}

	if useSIWC {
		if sessionID := strings.TrimSpace(r.Header.Get("Session-Id")); sessionID != "" {
			r.Header.Del("Session-Id")
			setHeaderCasePreserved(r.Header, "session_id", sessionID)
			r.Header.Set("X-Client-Request-Id", sessionID)
		}
		r.Header.Set("User-Agent", codexSIWCUserAgent(cfg))
		if stream {
			r.Header.Set("Accept", "text/event-stream")
		} else {
			r.Header.Set("Accept", "application/json")
		}
		r.Header.Set("Connection", "Keep-Alive")
		var attrs map[string]string
		if auth != nil {
			attrs = auth.Attributes
		}
		util.ApplyCustomHeadersFromAttrs(r, attrs, ginHeaders)
		return
	}

	if ginHeaders != nil && ginHeaders.Get("X-Codex-Beta-Features") != "" {
		r.Header.Set("X-Codex-Beta-Features", ginHeaders.Get("X-Codex-Beta-Features"))
	}
	misc.EnsureHeader(r.Header, ginHeaders, "Version", "")
	misc.EnsureHeader(r.Header, ginHeaders, "X-Codex-Turn-Metadata", "")
	misc.EnsureHeader(r.Header, ginHeaders, "X-Codex-Turn-State", "")
	misc.EnsureHeader(r.Header, ginHeaders, "X-Client-Request-Id", "")
	misc.EnsureHeader(r.Header, ginHeaders, "X-Codex-Window-Id", "")
	misc.EnsureHeader(r.Header, ginHeaders, "Thread-Id", "")
	misc.EnsureHeader(r.Header, ginHeaders, "Session-Id", "")
	misc.EnsureHeader(r.Header, ginHeaders, "X-Openai-Internal-Codex-Responses-Lite", "")

	cfgUserAgent, _ := codexHeaderDefaults(cfg, auth)
	ensureHeaderWithConfigPrecedence(r.Header, ginHeaders, "User-Agent", cfgUserAgent, codexUserAgent)

	if stream {
		r.Header.Set("Accept", "text/event-stream")
	} else {
		r.Header.Set("Accept", "application/json")
	}
	r.Header.Set("Connection", "Keep-Alive")

	isAPIKey := codexAuthUsesAPIKey(auth)
	if originator := strings.TrimSpace(ginHeaders.Get("Originator")); originator != "" {
		r.Header.Set("Originator", originator)
	} else if !isAPIKey {
		r.Header.Set("Originator", codexOriginator)
	}
	if !isAPIKey {
		if auth != nil && auth.Metadata != nil {
			if accountID, ok := auth.Metadata["account_id"].(string); ok {
				r.Header.Set("Chatgpt-Account-Id", accountID)
			}
		}
	}
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(r, attrs, ginHeaders)
	applyCodexCloakingHeaders(r.Header, cfg, auth)
}

const codexRoutingHintHeader = "X-Codex-Routing-Hint"

// applyCodexRoutingHint sends the routing hint native Codex attaches to every
// ChatGPT-backend Responses request: "model=<slug>" plus ";tier=<service_tier>"
// when the body requests a tier (openai/codex rust-v0.155.0,
// codex-rs/core/src/client.rs build_routing_hint_header). Without it, a
// translated request carries service_tier=priority only in the body. Whether
// the backend needs the header to grant priority is undocumented.
//
// The model is the resolved model written to the upstream body, while the tier
// is read from the final body so payload rules cannot make the hint stale. A
// hint forwarded by a native client names its original model and is replaced.
// Operator configuration keeps precedence: when an auth "header:" rule for the
// hint resolves to a value (static, or a "$Header" reference the request
// carries), that value is sent, and callers apply models.json override_header
// afterwards. A rule that resolves to nothing falls back to the derived hint.
// API-key requests are not touched, matching native Codex, which sends no hint
// to API-key providers.
func applyCodexRoutingHint(ctx context.Context, headers http.Header, auth *coreauth.Auth, baseModel string, upstreamBody []byte, clientHeaders http.Header) {
	if codexAuthUsesAPIKey(auth) || codexUseSIWCForRequest(ctx, auth, clientHeaders) {
		return
	}
	deleteHeaderCaseInsensitive(headers, codexRoutingHintHeader)
	if operatorHint := codexOperatorHeaderValue(ctx, auth, clientHeaders, codexRoutingHintHeader); operatorHint != "" {
		headers.Set(codexRoutingHintHeader, operatorHint)
		return
	}
	model := strings.TrimSpace(baseModel)
	if model == "" {
		return
	}
	hint := "model=" + model
	if tier := ojson.GetBytesNoCopy(upstreamBody, "service_tier"); tier.Type == ojson.String {
		if value := strings.TrimSpace(tier.String()); value != "" {
			hint += ";tier=" + value
		}
	}
	headers.Set(codexRoutingHintHeader, hint)
}

// codexOperatorHeaderValue returns the value the auth's "header:" rules
// resolve to for name, using the same resolver that applied them to the
// request, so dynamic references that resolve to nothing report "".
func codexOperatorHeaderValue(ctx context.Context, auth *coreauth.Auth, clientHeaders http.Header, name string) string {
	if auth == nil || len(auth.Attributes) == 0 {
		return ""
	}
	resolved := (&http.Request{Header: http.Header{}}).WithContext(ctx)
	util.ApplyCustomHeadersFromAttrs(resolved, auth.Attributes, clientHeaders)
	return strings.TrimSpace(resolved.Header.Get(name))
}

func isCodexCloakingDisabled(cfg *config.Config, auth *coreauth.Auth) bool {
	if auth != nil && auth.AuthKind() == coreauth.AuthKindAPIKey {
		cfg = cfg.ForAPIKey()
	}
	if auth != nil && len(auth.Attributes) > 0 {
		if val, ok := auth.Attributes[coreauth.AttributeCodexDisableCloaking]; ok {
			if parsed, errParse := strconv.ParseBool(strings.TrimSpace(val)); errParse == nil {
				return parsed
			}
		}
	}
	if entry := resolveCodexKeyConfig(cfg, auth); entry != nil && entry.DisableCodexCloaking != nil {
		return *entry.DisableCodexCloaking
	}
	if cfg != nil && cfg.Codex.DisableCodexCloaking {
		return true
	}
	return false
}

func applyCodexCloakingHeaders(headers http.Header, cfg *config.Config, auth *coreauth.Auth) {
	if headers == nil || cfg == nil || isCodexCloakingDisabled(cfg, auth) {
		return
	}
	headers.Set("User-Agent", codexUserAgent)
	headers.Set("Originator", codexOriginator)
}

func normalizeCodexInstructions(body []byte, nativeRequest ...bool) []byte {
	if len(nativeRequest) > 0 && nativeRequest[0] {
		return body
	}
	doc := ojson.NewDoc(body)
	if !normalizeCodexInstructionsDoc(doc, nativeRequest...) {
		return body
	}
	return doc.Bytes()
}

func normalizeCodexInstructionsDoc(doc *ojson.Doc, nativeRequest ...bool) bool {
	if doc == nil || (len(nativeRequest) > 0 && nativeRequest[0]) {
		return false
	}
	instructions := doc.Get("instructions")
	if !instructions.Exists() || instructions.Type == ojson.Null {
		return doc.Set("instructions", "") == nil
	}
	return false
}

func isCodexResponsesLiteRequestDoc(doc *ojson.Doc, headers http.Header) bool {
	return helps.CodexResponsesLiteRequestDoc(doc, headers)
}

var webSearchToolJSON = []byte(`{"type":"web_search"}`)
var webSearchToolArrayJSON = []byte(`[{"type":"web_search"}]`)

func isCodexWebSearchToolType(toolType string) bool {
	switch strings.TrimSpace(toolType) {
	case "web_search", "web_search_preview", "web_search_preview_2025_03_11":
		return true
	default:
		return false
	}
}

// ensureWebSearchToolForHistory reconciles replayed web_search_call input items
// with the request's tool declarations so upstream Codex does not fail with
// "response protection is unavailable". Standard Responses requests receive a
// {"type":"web_search"} tool declaration when missing, while Responses Lite
// requests (which omit top-level hosted tools) strip the replayed search items.
func ensureWebSearchToolForHistory(body []byte, headers http.Header) []byte {
	if !bytes.Contains(body, []byte("web_search_call")) {
		return body
	}
	doc := ojson.NewDoc(body)
	if !ensureWebSearchToolForHistoryDoc(doc, headers) {
		return body
	}
	return doc.Bytes()
}

func ensureWebSearchToolForHistoryDoc(doc *ojson.Doc, headers http.Header) bool {
	if doc == nil {
		return false
	}
	input := doc.Get("input")
	if !input.IsArray() || !strings.Contains(input.Raw, "web_search_call") {
		return false
	}
	hasWebSearchCall := false
	input.ForEach(func(_, item ojson.Result) bool {
		if strings.TrimSpace(item.Get("type").String()) == "web_search_call" {
			hasWebSearchCall = true
			return false
		}
		return true
	})
	if !hasWebSearchCall {
		return false
	}

	tools := doc.Get("tools")
	if tools.IsArray() {
		for _, t := range tools.Array() {
			if isCodexWebSearchToolType(t.Get("type").String()) {
				return false
			}
		}
	}

	if isCodexResponsesLiteRequestDoc(doc, headers) {
		items := input.Array()
		rebuilt := make([]string, 0, len(items))
		for _, item := range items {
			if strings.TrimSpace(item.Get("type").String()) == "web_search_call" {
				continue
			}
			rebuilt = append(rebuilt, item.Raw)
		}
		return doc.SetRawBytes("input", []byte("["+strings.Join(rebuilt, ",")+"]")) == nil
	}

	if !tools.Exists() || !tools.IsArray() {
		return doc.SetRawBytes("tools", webSearchToolArrayJSON) == nil
	}
	return doc.SetRawBytes("tools.-1", webSearchToolJSON) == nil
}

func normalizeCodexParallelToolCalls(body []byte, headers http.Header) []byte {
	doc := ojson.NewDoc(body)
	if !normalizeCodexParallelToolCallsDoc(doc, headers) {
		return body
	}
	return doc.Bytes()
}

func normalizeCodexParallelToolCallsDoc(doc *ojson.Doc, headers http.Header) bool {
	if doc == nil {
		return false
	}
	if isCodexResponsesLiteRequestDoc(doc, headers) {
		return helps.SetBoolIfDifferentDoc(doc, "parallel_tool_calls", false)
	}
	return normalizeCodexParallelToolCallsForToolsDoc(doc)
}

func normalizeCodexParallelToolCallsForTools(body []byte) []byte {
	doc := ojson.NewDoc(body)
	if !normalizeCodexParallelToolCallsForToolsDoc(doc) {
		return body
	}
	return doc.Bytes()
}

func normalizeCodexParallelToolCallsForToolsDoc(doc *ojson.Doc) bool {
	if doc == nil || !doc.Get("parallel_tool_calls").Exists() {
		return false
	}

	tools := doc.Get("tools")
	hasTools := tools.Exists() && tools.IsArray() && len(tools.Array()) > 0
	if hasTools {
		return false
	}

	return doc.Delete("parallel_tool_calls") == nil
}
