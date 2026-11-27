package auth

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"testing"

	"github.com/udit19199/relay/internal/registry"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

type retryRoundCallExecutor struct {
	identifier string
	mu         sync.Mutex
	executeIDs []string
	streamIDs  []string
	countIDs   []string
}

func (e *retryRoundCallExecutor) Identifier() string { return e.identifier }

func (e *retryRoundCallExecutor) Execute(_ context.Context, auth *Auth, _ coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	e.mu.Lock()
	e.executeIDs = append(e.executeIDs, auth.ID)
	e.mu.Unlock()
	return coreexecutor.Response{}, &Error{HTTPStatus: http.StatusInternalServerError, Message: "retry-round test failure"}
}

func (e *retryRoundCallExecutor) ExecuteStream(_ context.Context, auth *Auth, _ coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	e.mu.Lock()
	e.streamIDs = append(e.streamIDs, auth.ID)
	e.mu.Unlock()
	return nil, &Error{HTTPStatus: http.StatusInternalServerError, Message: "retry-round test failure"}
}

func (*retryRoundCallExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *retryRoundCallExecutor) CountTokens(_ context.Context, auth *Auth, _ coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	e.mu.Lock()
	e.countIDs = append(e.countIDs, auth.ID)
	e.mu.Unlock()
	return coreexecutor.Response{}, &Error{HTTPStatus: http.StatusInternalServerError, Message: "retry-round test failure"}
}

func (*retryRoundCallExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func (e *retryRoundCallExecutor) ids(kind string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var source []string
	switch kind {
	case "execute":
		source = e.executeIDs
	case "stream":
		source = e.streamIDs
	case "count":
		source = e.countIDs
	}
	return append([]string(nil), source...)
}

func registerRetryRoundLocalAuths(t *testing.T, manager *Manager, provider, model string, limits map[string]int) []string {
	t.Helper()
	ids := make([]string, 0, len(limits))
	for id := range limits {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	reg := registry.GetGlobalRegistry()
	for _, id := range ids {
		reg.RegisterClient(id, provider, []*registry.ModelInfo{{ID: model}})
		t.Cleanup(func() { reg.UnregisterClient(id) })
		if _, errRegister := manager.Register(context.Background(), &Auth{
			ID:       id,
			Provider: provider,
			Metadata: map[string]any{"request_retry": limits[id], "disable_cooling": true},
		}); errRegister != nil {
			t.Fatalf("register %s: %v", id, errRegister)
		}
	}
	return ids
}

func countRetryRoundIDs(ids []string) map[string]int {
	counts := make(map[string]int, len(ids))
	for _, id := range ids {
		counts[id]++
	}
	return counts
}

func TestExecuteRetryRoundCredentialWindows(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*Manager, coreexecutor.Request) error
		kind   string
	}{
		{
			name: "non-stream",
			invoke: func(manager *Manager, req coreexecutor.Request) error {
				_, errExecute := manager.Execute(context.Background(), []string{"retry-round-test"}, req, coreexecutor.Options{})
				return errExecute
			},
			kind: "execute",
		},
		{
			name: "count-tokens",
			invoke: func(manager *Manager, req coreexecutor.Request) error {
				_, errExecute := manager.ExecuteCount(context.Background(), []string{"retry-round-test"}, req, coreexecutor.Options{})
				return errExecute
			},
			kind: "count",
		},
		{
			name: "stream",
			invoke: func(manager *Manager, req coreexecutor.Request) error {
				_, errExecute := manager.ExecuteStream(context.Background(), []string{"retry-round-test"}, req, coreexecutor.Options{Stream: true})
				return errExecute
			},
			kind: "stream",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := NewManager(nil, nil, nil)
			manager.SetRetryConfig(3, 0, 0)
			executor := &retryRoundCallExecutor{identifier: "retry-round-test"}
			manager.RegisterExecutor(executor)
			registerRetryRoundLocalAuths(t, manager, "retry-round-test", "retry-round-model", map[string]int{
				"retry-round-a": 3,
				"retry-round-b": 2,
				"retry-round-c": 2,
			})

			if errExecute := test.invoke(manager, coreexecutor.Request{Model: "retry-round-model"}); errExecute == nil {
				t.Fatal("execution error = nil, want terminal retry error")
			}
			counts := countRetryRoundIDs(executor.ids(test.kind))
			if counts["retry-round-a"] != 4 || counts["retry-round-b"] != 3 || counts["retry-round-c"] != 3 {
				t.Fatalf("credential call counts = %#v, want A=4 B=3 C=3; calls=%v", counts, executor.ids(test.kind))
			}
		})
	}
}

func TestExecuteRetryRoundMaxCredentialsAgesSkippedAuths(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	manager.SetRetryConfig(2, 0, 3)
	executor := &retryRoundCallExecutor{identifier: "retry-round-test"}
	manager.RegisterExecutor(executor)
	registerRetryRoundLocalAuths(t, manager, "retry-round-test", "retry-round-model-cap", map[string]int{
		"retry-cap-a": 1,
		"retry-cap-b": 1,
		"retry-cap-c": 1,
		"retry-cap-d": 2,
	})

	if _, errExecute := manager.Execute(context.Background(), []string{"retry-round-test"}, coreexecutor.Request{Model: "retry-round-model-cap"}, coreexecutor.Options{}); errExecute == nil {
		t.Fatal("execution error = nil, want terminal retry error")
	}
	calls := executor.ids("execute")
	if len(calls) != 7 {
		t.Fatalf("credential calls = %v, want three initial, three round-1, and one round-2 call", calls)
	}
	counts := countRetryRoundIDs(calls)
	if counts["retry-cap-a"] > 2 || counts["retry-cap-b"] > 2 || counts["retry-cap-c"] > 2 || counts["retry-cap-d"] > 3 {
		t.Fatalf("credential call counts exceed their round windows: %#v", counts)
	}
	if calls[len(calls)-1] != "retry-cap-d" {
		t.Fatalf("last retry call = %q, want retry-cap-d; calls=%v", calls[len(calls)-1], calls)
	}
}

type retryConfigMutationExecutor struct {
	identifier  string
	manager     *Manager
	nextDefault int

	mu    sync.Mutex
	calls int
}

func (e *retryConfigMutationExecutor) Identifier() string { return e.identifier }

func (e *retryConfigMutationExecutor) Execute(context.Context, *Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	if e.recordCall() == 1 {
		return coreexecutor.Response{}, &Error{HTTPStatus: http.StatusInternalServerError, Message: "retry config changed"}
	}
	return coreexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *retryConfigMutationExecutor) ExecuteStream(context.Context, *Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	if e.recordCall() == 1 {
		return nil, &Error{HTTPStatus: http.StatusInternalServerError, Message: "retry config changed"}
	}
	chunks := make(chan coreexecutor.StreamChunk)
	close(chunks)
	return &coreexecutor.StreamResult{Chunks: chunks}, nil
}

func (*retryConfigMutationExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *retryConfigMutationExecutor) CountTokens(context.Context, *Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	if e.recordCall() == 1 {
		return coreexecutor.Response{}, &Error{HTTPStatus: http.StatusInternalServerError, Message: "retry config changed"}
	}
	return coreexecutor.Response{Payload: []byte("ok")}, nil
}

func (*retryConfigMutationExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func (e *retryConfigMutationExecutor) recordCall() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	if e.calls == 1 {
		e.manager.SetRetryConfig(e.nextDefault, 0, 0)
	}
	return e.calls
}

func (e *retryConfigMutationExecutor) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestExecuteSnapshotsDefaultRequestRetry(t *testing.T) {
	paths := []struct {
		name   string
		invoke func(*Manager) error
	}{
		{
			name: "non-stream",
			invoke: func(manager *Manager) error {
				_, errExecute := manager.Execute(context.Background(), []string{"retry-config-snapshot"}, coreexecutor.Request{Model: "retry-config-snapshot-model"}, coreexecutor.Options{})
				return errExecute
			},
		},
		{
			name: "count-tokens",
			invoke: func(manager *Manager) error {
				_, errExecute := manager.ExecuteCount(context.Background(), []string{"retry-config-snapshot"}, coreexecutor.Request{Model: "retry-config-snapshot-model"}, coreexecutor.Options{})
				return errExecute
			},
		},
		{
			name: "stream",
			invoke: func(manager *Manager) error {
				_, errExecute := manager.ExecuteStream(context.Background(), []string{"retry-config-snapshot"}, coreexecutor.Request{Model: "retry-config-snapshot-model"}, coreexecutor.Options{Stream: true})
				return errExecute
			},
		},
	}
	scenarios := []struct {
		name        string
		initial     int
		next        int
		wantCalls   int
		wantSuccess bool
	}{
		{name: "decrease after request starts", initial: 1, next: 0, wantCalls: 2, wantSuccess: true},
		{name: "increase after request starts", initial: 0, next: 1, wantCalls: 1, wantSuccess: false},
	}

	for _, path := range paths {
		for _, scenario := range scenarios {
			t.Run(path.name+"/"+scenario.name, func(t *testing.T) {
				manager := NewManager(nil, nil, nil)
				manager.SetRetryConfig(scenario.initial, 0, 0)
				executor := &retryConfigMutationExecutor{
					identifier:  "retry-config-snapshot",
					manager:     manager,
					nextDefault: scenario.next,
				}
				manager.RegisterExecutor(executor)

				const authID = "retry-config-snapshot-auth"
				reg := registry.GetGlobalRegistry()
				reg.RegisterClient(authID, "retry-config-snapshot", []*registry.ModelInfo{{ID: "retry-config-snapshot-model"}})
				t.Cleanup(func() { reg.UnregisterClient(authID) })
				if _, errRegister := manager.Register(context.Background(), &Auth{
					ID:       authID,
					Provider: "retry-config-snapshot",
					Metadata: map[string]any{"disable_cooling": true},
				}); errRegister != nil {
					t.Fatalf("register auth: %v", errRegister)
				}

				errExecute := path.invoke(manager)
				if scenario.wantSuccess && errExecute != nil {
					t.Fatalf("execution error = %v, want success", errExecute)
				}
				if !scenario.wantSuccess && statusCodeFromError(errExecute) != http.StatusInternalServerError {
					t.Fatalf("execution error = %v, want HTTP 500", errExecute)
				}
				if calls := executor.callCount(); calls != scenario.wantCalls {
					t.Fatalf("executor calls = %d, want %d", calls, scenario.wantCalls)
				}
			})
		}
	}
}
