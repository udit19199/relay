package helps

import (
	"context"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/udit19199/relay/internal/registry"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	"github.com/udit19199/relay/internal/thinking"
	"github.com/udit19199/relay/internal/util"
)

const codexResponsesLiteHeaderName = "X-OpenAI-Internal-Codex-Responses-Lite"

// CodexModelUsesResponsesLite identifies models whose native Codex route uses Responses Lite.
func CodexModelUsesResponsesLite(model string) bool {
	slug := strings.ToLower(strings.TrimSpace(thinking.ParseSuffix(model).ModelName))
	if idx := strings.LastIndex(slug, "/"); idx >= 0 {
		slug = strings.TrimSpace(slug[idx+1:])
	}
	return slug != "" && (strings.HasPrefix(slug, "gpt-6") || registry.CodexClientModelUsesResponsesLite(slug))
}

// CodexResponsesLiteRequestDoc reads the route hints without changing the payload.
func CodexResponsesLiteRequestDoc(doc *ojson.Doc, headers http.Header) bool {
	if strings.EqualFold(strings.TrimSpace(headers.Get(codexResponsesLiteHeaderName)), "true") {
		return true
	}
	if doc == nil {
		return false
	}
	value := doc.Get("client_metadata.ws_request_header_x_openai_internal_codex_responses_lite")
	return value.Type == ojson.True || (value.Type == ojson.String && strings.EqualFold(strings.TrimSpace(value.String()), "true"))
}

// CodexResponsesLiteRequestForAuthDoc reports whether the request uses the
// Codex Responses-Lite protocol after resolving client_metadata, client
// headers, credential header: attributes, and models.json override_header.
func CodexResponsesLiteRequestForAuthDoc(doc *ojson.Doc, modelName string, auth *coreauth.Auth, headers http.Header) bool {
	if CodexResponsesLiteRequestDoc(doc, nil) {
		return true
	}
	var effective string
	if headers != nil {
		effective = strings.TrimSpace(headers.Get(codexResponsesLiteHeaderName))
	}
	if auth != nil && len(auth.Attributes) > 0 {
		resolved := (&http.Request{Header: http.Header{}}).WithContext(context.Background())
		util.ApplyCustomHeadersFromAttrs(resolved, auth.Attributes, headers)
		if opLite := strings.TrimSpace(resolved.Header.Get(codexResponsesLiteHeaderName)); opLite != "" {
			effective = opLite
		}
	}
	if overrides := registry.ModelOverrideHeaders(modelName); len(overrides) > 0 {
		for key, value := range overrides {
			if strings.EqualFold(strings.TrimSpace(key), codexResponsesLiteHeaderName) {
				effective = strings.TrimSpace(value)
			}
		}
	}
	return strings.EqualFold(effective, "true")
}

// This file intentionally retains only Responses-Lite detection helpers.
