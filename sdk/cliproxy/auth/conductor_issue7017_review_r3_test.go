package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// Review round 3 regressions: explicit-zero hints must retry immediately,
// and the Home non-stream request-scoped stop path must keep the trusted
// Retry-After.

type issue7017SequenceExecutor struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (e *issue7017SequenceExecutor) Identifier() string { return "codex" }

func (e *issue7017SequenceExecutor) Execute(ctx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	cliproxyexecutor.MarkUpstreamAttempt(ctx)
	e.mu.Lock()
	e.calls++
	calls := e.calls
	e.mu.Unlock()
	if calls == 1 {
		return cliproxyexecutor.Response{}, e.err
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *issue7017SequenceExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, e.err
}

func (e *issue7017SequenceExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, e.err
}

func (*issue7017SequenceExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (*issue7017SequenceExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestIssue7017ExplicitZeroHintRetriesImmediately(t *testing.T) {
	withQuotaCooldownEnabled(t)
	zero := time.Duration(0)
	manager := NewManager(nil, nil, nil)
	manager.SetRetryConfig(1, 0, 0)
	executor := &issue7017SequenceExecutor{err: issue7017HintError{msg: issue7017TurnBody(), hint: &zero}}
	manager.RegisterExecutor(executor)
	registerRetryRoundLocalAuthsSkipCoolingOverride(t, manager, "turn-zero-retry")

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: "gpt-6.1-sol"}, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("explicit zero hint must allow an immediate retry round: %v", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("payload = %q, want ok", resp.Payload)
	}
	executor.mu.Lock()
	calls := executor.calls
	executor.mu.Unlock()
	if calls != 2 {
		t.Fatalf("calls = %d, want initial attempt plus one immediate retry", calls)
	}
}
