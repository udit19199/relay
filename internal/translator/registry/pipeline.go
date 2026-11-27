package registry

import (
	"context"

	"github.com/udit19199/relay/internal/registry"
)

// RequestEnvelope represents a request in the translation pipeline.
type RequestEnvelope struct {
	Format    Format
	Model     string
	Stream    bool
	Body      []byte
	ModelInfo *registry.ModelInfo
	// Err is a request-scoped translation failure. Callers must not send Body upstream when it is set.
	Err error
}

// ResponseEnvelope represents a response in the translation pipeline.
type ResponseEnvelope struct {
	Format Format
	Model  string
	Stream bool
	Body   []byte
	Chunks [][]byte
}

// RequestMiddleware decorates request translation.
type RequestMiddleware func(ctx context.Context, req RequestEnvelope, next RequestHandler) (RequestEnvelope, error)

// ResponseMiddleware decorates response translation.
type ResponseMiddleware func(ctx context.Context, resp ResponseEnvelope, next ResponseHandler) (ResponseEnvelope, error)

// RequestHandler performs request translation between formats.
type RequestHandler func(ctx context.Context, req RequestEnvelope) (RequestEnvelope, error)

// ResponseHandler performs response translation between formats.
type ResponseHandler func(ctx context.Context, resp ResponseEnvelope) (ResponseEnvelope, error)

// Pipeline orchestrates request/response transformation with middleware support.
type Pipeline struct {
	registry          *Registry
	requestMiddleware []RequestMiddleware
}

// NewPipeline constructs a pipeline bound to the provided registry.
func NewPipeline(registry *Registry) *Pipeline {
	if registry == nil {
		registry = Default()
	}
	return &Pipeline{registry: registry}
}

// TranslateRequest applies middleware and registry transformations.
func (p *Pipeline) TranslateRequest(ctx context.Context, from, to Format, req RequestEnvelope) (RequestEnvelope, error) {
	terminal := func(ctx context.Context, input RequestEnvelope) (RequestEnvelope, error) {
		return p.registry.TranslateRequestEnvelope(ctx, from, to, input), nil
	}

	handler := terminal
	for i := len(p.requestMiddleware) - 1; i >= 0; i-- {
		mw := p.requestMiddleware[i]
		next := handler
		handler = func(ctx context.Context, r RequestEnvelope) (RequestEnvelope, error) {
			return mw(ctx, r, next)
		}
	}

	return handler(ctx, req)
}
