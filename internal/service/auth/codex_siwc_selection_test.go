package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/udit19199/relay/internal/registry"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

type recordingSIWCExecutor struct {
	id        string
	onExecute func(ctx context.Context, auth *Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error)
	onStream  func(ctx context.Context, auth *Auth, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error)
}

func (e *recordingSIWCExecutor) Identifier() string { return e.id }

func (e *recordingSIWCExecutor) Execute(ctx context.Context, auth *Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	if e.onExecute != nil {
		return e.onExecute(ctx, auth, req, opts)
	}
	return coreexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
}

func (e *recordingSIWCExecutor) ExecuteStream(ctx context.Context, auth *Auth, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	if e.onStream != nil {
		return e.onStream(ctx, auth, req, opts)
	}
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{Payload: []byte("data: {}\n\n")}
	close(ch)
	return &coreexecutor.StreamResult{Chunks: ch}, nil
}

func (e *recordingSIWCExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *recordingSIWCExecutor) CountTokens(ctx context.Context, auth *Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, nil
}

func (e *recordingSIWCExecutor) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	return nil, nil
}

func registerTestCodexModels(t *testing.T, authID string) {
	t.Helper()
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(authID, "codex", []*registry.ModelInfo{
		{ID: "gpt-5.4"},
		{ID: "gpt-image-2.6"},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})
}
