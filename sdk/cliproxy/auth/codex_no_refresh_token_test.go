package auth

import (
	"context"
	"testing"
	"time"
)

type codexRenewalEvaluator struct{}

func (codexRenewalEvaluator) ShouldRefresh(time.Time, *Auth) bool { return true }

func TestCodexNoRefreshTokenRemovedFromAutoRefreshQueue(t *testing.T) {
	now := time.Now()
	lead := CodexOuterRefreshLead
	setRefreshLeadFactory(t, "codex", func() *time.Duration { return &lead })
	m := NewManager(nil, &RoundRobinSelector{}, nil)
	m.RegisterExecutor(&countingRefreshExecutor{id: "codex"})
	a, err := m.Register(context.Background(), &Auth{ID: "access-only", Provider: "codex", Metadata: map[string]any{"access_token": "access", "expired": now.Add(-time.Hour).Format(time.RFC3339)}})
	if err != nil {
		t.Fatal(err)
	}
	loop := newAuthAutoRefreshLoop(m, time.Second, 1)
	loop.rebuild(now)
	if _, ok := loop.peek(); ok {
		t.Fatal("access-only credential queued by rebuild")
	}
	loop.upsert(a.ID, now)
	loop.queueReschedule(a.ID)
	loop.applyDirty(now)
	if _, ok := loop.peek(); ok {
		t.Fatal("access-only credential retained by dirty reschedule")
	}
	loop.upsert(a.ID, now)
	loop.handleDue(context.Background(), now)
	if _, ok := loop.peek(); ok {
		t.Fatal("access-only credential requeued by due handler")
	}
	if len(loop.jobs) != 0 {
		t.Fatal("access-only credential submitted for refresh")
	}

	// A later credential update must restore scheduling without restarting the loop.
	a.Metadata["refresh_token"] = "restored-refresh"
	if _, errUpdate := m.Update(context.Background(), a); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	loop.queueReschedule(a.ID)
	loop.applyDirty(now)
	if next, ok := loop.peek(); !ok || next.After(now) {
		t.Fatal("renewable credential was not scheduled after update")
	}
	loop.handleDue(context.Background(), now)
	if len(loop.jobs) != 1 {
		t.Fatal("renewable credential was not submitted for refresh")
	}
}
