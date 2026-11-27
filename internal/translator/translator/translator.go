// Package translator provides request and response translation functionality
// between different AI API formats. It acts as a wrapper around the SDK translator
// registry, providing convenient functions for translating requests and responses
// between OpenAI, Claude, Gemini, and other API formats.
package translator

import (
	"context"
	"github.com/udit19199/relay/internal/interfaces"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

// registry holds the default translator registry instance.
var registry = translatorreg.Default()

// Register registers a new translator for converting between two API formats.
//
// Parameters:
//   - from: The source API format identifier
//   - to: The target API format identifier
//   - request: The request translation function
//   - response: The response translation function
func Register(from, to string, request interfaces.TranslateRequestFunc, response interfaces.TranslateResponse) {
	registry.Register(translatorreg.FromString(from), translatorreg.FromString(to), request, response)
}

// Request translates a request from one API format to another.
//
// Parameters:
//   - from: The source API format identifier
//   - to: The target API format identifier
//   - modelName: The model name for the request
//   - rawJSON: The raw JSON request data
//   - stream: Whether this is a streaming request
//
// Returns:
//   - []byte: The translated request JSON
func Request(from, to, modelName string, rawJSON []byte, stream bool) []byte {
	return registry.TranslateRequest(translatorreg.FromString(from), translatorreg.FromString(to), modelName, rawJSON, stream)
}

// ResponseNonStream translates a non-streaming response from one API format to another.
//
// Parameters:
//   - from: The source API format identifier
//   - to: The target API format identifier
//   - ctx: The context for the translation
//   - modelName: The model name for the response
//   - originalRequestRawJSON: The original request JSON
//   - requestRawJSON: The translated request JSON
//   - rawJSON: The raw response JSON
//   - param: Additional parameters for translation
//
// Returns:
//   - []byte: The translated response JSON
func ResponseNonStream(from, to string, ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	return registry.TranslateNonStream(ctx, translatorreg.FromString(from), translatorreg.FromString(to), modelName, originalRequestRawJSON, requestRawJSON, rawJSON, param)
}
