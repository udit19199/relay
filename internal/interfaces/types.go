// Package interfaces provides type aliases for backwards compatibility with translator functions.
// It defines common interface types used throughout the CLI Proxy API for request and response
// transformation operations, maintaining compatibility with the SDK translator package.
package interfaces

import translatorreg "github.com/udit19199/relay/internal/translator/registry"

// Backwards compatible aliases for translator function types.
type TranslateRequestFunc = translatorreg.RequestTransform

type TranslateResponseFunc = translatorreg.ResponseStreamTransform

type TranslateResponseNonStreamFunc = translatorreg.ResponseNonStreamTransform

type TranslateResponse = translatorreg.ResponseTransform
