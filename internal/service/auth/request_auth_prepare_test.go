package auth

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/udit19199/relay/internal/registry"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

type requestPrepareStore struct {
	saveCount atomic.Int32
	mu        sync.Mutex
	last      *Auth
}

func (s *requestPrepareStore) List(context.Context) ([]*Auth, error) { return nil, nil }

func (s *requestPrepareStore) Save(_ context.Context, auth *Auth) (string, error) {
	s.saveCount.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = auth.Clone()
	return "", nil
}

func (s *requestPrepareStore) Delete(context.Context, string) error { return nil }

func (s *requestPrepareStore) lastAuth() *Auth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last.Clone()
}

type requestPrepareExecutor struct {
	prepareCalls atomic.Int32
	executeCalls atomic.Int32
	prepareErr   error
	executeErr   error
	mu           sync.Mutex
	observed     []*Auth
}

func (e *requestPrepareExecutor) Identifier() string { return "antigravity" }

func (e *requestPrepareExecutor) ShouldPrepareRequestAuth(auth *Auth) bool {
	return auth == nil || auth.Metadata == nil || testStringValue(auth.Metadata["project_id"]) == ""
}

func (e *requestPrepareExecutor) PrepareRequestAuth(_ context.Context, auth *Auth) (*Auth, error) {
	e.prepareCalls.Add(1)
	if e.prepareErr != nil {
		return nil, e.prepareErr
	}
	updated := auth.Clone()
	if updated.Metadata == nil {
		updated.Metadata = make(map[string]any)
	}
	updated.Metadata["project_id"] = "prepared-project"
	return updated, nil
}

func (e *requestPrepareExecutor) recordPreparedAuth(auth *Auth) error {
	e.executeCalls.Add(1)
	if got := testStringValue(auth.Metadata["project_id"]); got != "prepared-project" {
		return &Error{HTTPStatus: http.StatusBadRequest, Message: "missing prepared project"}
	}
	e.mu.Lock()
	e.observed = append(e.observed, auth.Clone())
	e.mu.Unlock()
	return nil
}

func (e *requestPrepareExecutor) Execute(ctx context.Context, auth *Auth, _ coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	if errPrepared := e.recordPreparedAuth(auth); errPrepared != nil {
		return coreexecutor.Response{}, errPrepared
	}
	if e.executeErr != nil {
		coreexecutor.MarkUpstreamAttempt(ctx)
		return coreexecutor.Response{}, e.executeErr
	}
	return coreexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *requestPrepareExecutor) ExecuteStream(ctx context.Context, auth *Auth, _ coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	if errPrepared := e.recordPreparedAuth(auth); errPrepared != nil {
		return nil, errPrepared
	}
	if e.executeErr != nil {
		coreexecutor.MarkUpstreamAttempt(ctx)
		return nil, e.executeErr
	}
	chunks := make(chan coreexecutor.StreamChunk, 1)
	chunks <- coreexecutor.StreamChunk{Payload: []byte(`{"type":"response.completed"}`)}
	close(chunks)
	return &coreexecutor.StreamResult{Chunks: chunks}, nil
}

func (e *requestPrepareExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *requestPrepareExecutor) CountTokens(ctx context.Context, auth *Auth, _ coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	if errPrepared := e.recordPreparedAuth(auth); errPrepared != nil {
		return coreexecutor.Response{}, errPrepared
	}
	if e.executeErr != nil {
		coreexecutor.MarkUpstreamAttempt(ctx)
		return coreexecutor.Response{}, e.executeErr
	}
	return coreexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *requestPrepareExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{HTTPStatus: http.StatusNotImplemented, Message: "http not implemented"}
}

func TestManagerExecute_PreparesAndPersistsMissingRequestAuthMetadata(t *testing.T) {
	const model = "gemini-3.1-pro"
	store := &requestPrepareStore{}
	executor := &requestPrepareExecutor{}
	manager := NewManager(store, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &Auth{
		ID:       "auth-request-prepare",
		Provider: "antigravity",
		Metadata: map[string]any{"access_token": "token"},
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, "antigravity", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })

	resp, errExecute := manager.Execute(context.Background(), []string{"antigravity"}, coreexecutor.Request{Model: model}, coreexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("Execute error: %v", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("payload = %q, want ok", string(resp.Payload))
	}
	if got := executor.prepareCalls.Load(); got != 1 {
		t.Fatalf("prepare calls = %d, want 1", got)
	}
	if got := store.saveCount.Load(); got < 1 {
		t.Fatalf("save count = %d, want at least 1", got)
	}
	if got := testStringValue(store.lastAuth().Metadata["project_id"]); got != "prepared-project" {
		t.Fatalf("persisted project_id = %q, want prepared-project", got)
	}
	current, ok := manager.GetByID(auth.ID)
	if !ok {
		t.Fatal("expected auth in manager")
	}
	if got := testStringValue(current.Metadata["project_id"]); got != "prepared-project" {
		t.Fatalf("manager project_id = %q, want prepared-project", got)
	}

	if _, errExecute = manager.Execute(context.Background(), []string{"antigravity"}, coreexecutor.Request{Model: model}, coreexecutor.Options{}); errExecute != nil {
		t.Fatalf("second Execute error: %v", errExecute)
	}
	if got := executor.prepareCalls.Load(); got != 1 {
		t.Fatalf("prepare calls after second execute = %d, want 1", got)
	}
}

func TestManagerExecute_PrepareAuth403TriggersCooldown(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	const model = "gemini-3.1-pro"
	store := &requestPrepareStore{}
	executor := &requestPrepareExecutor{
		prepareErr: customStatusError{code: http.StatusForbidden, msg: "forbidden"},
	}
	manager := NewManager(store, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &Auth{
		ID:       "auth-prepare-403",
		Provider: "antigravity",
		Metadata: map[string]any{"access_token": "token"},
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, "antigravity", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })

	_, errExecute := manager.Execute(context.Background(), []string{"antigravity"}, coreexecutor.Request{Model: model}, coreexecutor.Options{})
	if errExecute == nil {
		t.Fatal("expected Execute error on 403 prepare failure")
	}

	current, ok := manager.GetByID(auth.ID)
	if !ok {
		t.Fatal("expected auth in manager")
	}
	if !current.Unavailable {
		t.Fatal("expected auth to be marked unavailable after 403 prepare failure")
	}
	if current.Quota.NextRecoverAt.IsZero() && current.NextRetryAfter.IsZero() {
		t.Fatal("expected auth cooldown to be scheduled after 403 prepare failure")
	}
}

func testStringValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []byte:
		return strings.TrimSpace(string(typed))
	default:
		return ""
	}
}
