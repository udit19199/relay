package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/udit19199/relay/internal/registry"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

func withQuotaProbeInterval(t *testing.T, seconds int) {
	t.Helper()
	previous := credentialQuotaProbeIntervalSeconds.Load()
	SetCredentialQuotaProbeIntervalSeconds(seconds)
	t.Cleanup(func() { credentialQuotaProbeIntervalSeconds.Store(previous) })
}

// quotaProbeFixture registers a Claude credential blocked by a credential-wide
// quota and returns a picker for the requested selection path.
func quotaProbeFixture(t *testing.T, path string, probeAfter time.Time) (*Manager, *Auth, string) {
	t.Helper()
	withQuotaCooldownEnabled(t)
	const model = "claude-sonnet-4-6"
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(&mockCustomErrorExecutor{identifier: "claude"})
	auth := &Auth{
		ID:       "claude-probe-" + path,
		Provider: "claude",
		Status:   StatusActive,
		Quota: QuotaState{
			Exceeded:      true,
			Reason:        "credential_quota",
			NextRecoverAt: time.Now().Add(70 * time.Hour),
			ProbeAfter:    probeAfter,
		},
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, "claude", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register: %v", errRegister)
	}
	return manager, auth, model
}

func pickThroughPath(t *testing.T, manager *Manager, path, model string) (*Auth, error) {
	t.Helper()
	if path == "scheduler" {
		selected, _, _, errPick := manager.pickNextMixed(context.Background(), []string{"claude"}, model, coreexecutor.Options{}, nil)
		return selected, errPick
	}
	return manager.SelectAuth(context.Background(), "claude", model, coreexecutor.Options{})
}

func TestQuotaProbeWaitsForInterval(t *testing.T) {
	for _, path := range []string{"selector", "scheduler"} {
		t.Run(path, func(t *testing.T) {
			withQuotaProbeInterval(t, 60)
			manager, blocked, model := quotaProbeFixture(t, path, time.Now().Add(time.Minute))
			healthy := &Auth{ID: "claude-healthy-" + path, Provider: "claude", Status: StatusActive}
			registry.GetGlobalRegistry().RegisterClient(healthy.ID, "claude", []*registry.ModelInfo{{ID: model}})
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(healthy.ID) })
			if _, errRegister := manager.Register(WithSkipPersist(context.Background()), healthy); errRegister != nil {
				t.Fatalf("Register healthy: %v", errRegister)
			}

			for i := 0; i < 6; i++ {
				selected, errPick := pickThroughPath(t, manager, path, model)
				if errPick != nil {
					t.Fatalf("pick %d: %v", i, errPick)
				}
				if selected.ID != healthy.ID {
					t.Fatalf("pick %d = %s, want the healthy credential before the probe interval", i, selected.ID)
				}
			}
			updated, _ := manager.GetByID(blocked.ID)
			if updated == nil || !updated.Quota.ProbeLeaseUntil.IsZero() {
				t.Fatalf("blocked credential was probed before the interval: %+v", updated)
			}
		})
	}
}

func TestQuotaProbeAdmitsExactlyOneRequest(t *testing.T) {
	for _, path := range []string{"selector", "scheduler"} {
		t.Run(path, func(t *testing.T) {
			withQuotaProbeInterval(t, 60)
			manager, blocked, model := quotaProbeFixture(t, path, time.Now().Add(-time.Second))

			selected, errPick := pickThroughPath(t, manager, path, model)
			if errPick != nil {
				t.Fatalf("probe pick: %v", errPick)
			}
			if selected.ID != blocked.ID {
				t.Fatalf("probe pick = %s, want %s", selected.ID, blocked.ID)
			}
			updated, _ := manager.GetByID(blocked.ID)
			if updated == nil || !updated.Quota.ProbeLeaseUntil.After(time.Now()) {
				t.Fatalf("probe was not marked in flight: %+v", updated)
			}

			if second, errSecond := pickThroughPath(t, manager, path, model); errSecond == nil {
				t.Fatalf("second pick = %s, want no credential while the probe is in flight", second.ID)
			}
		})
	}
}

func TestQuotaProbeSuccessClearsBlock(t *testing.T) {
	for _, path := range []string{"selector", "scheduler"} {
		t.Run(path, func(t *testing.T) {
			withQuotaProbeInterval(t, 60)
			manager, blocked, model := quotaProbeFixture(t, path, time.Now().Add(-time.Second))
			if _, errPick := pickThroughPath(t, manager, path, model); errPick != nil {
				t.Fatalf("probe pick: %v", errPick)
			}

			manager.MarkResult(context.Background(), Result{
				AuthID:   blocked.ID,
				Provider: "claude",
				Model:    model,
				Success:  true,
			})

			updated, _ := manager.GetByID(blocked.ID)
			if updated == nil {
				t.Fatal("auth missing after probe result")
			}
			if updated.Quota.Exceeded || updated.Quota.Reason == "credential_quota" {
				t.Fatalf("successful probe kept the block: %+v", updated.Quota)
			}
			if !updated.Quota.ProbeAfter.IsZero() || !updated.Quota.ProbeLeaseUntil.IsZero() {
				t.Fatalf("probe state survived recovery: %+v", updated.Quota)
			}
			if blockedNow, _, _ := isAuthBlockedForModel(updated, model, time.Now()); blockedNow {
				t.Fatal("credential is still blocked after a successful probe")
			}
			selected, errPick := pickThroughPath(t, manager, path, model)
			if errPick != nil || selected.ID != blocked.ID {
				t.Fatalf("pick after recovery = %v, %v; want %s", selected, errPick, blocked.ID)
			}
		})
	}
}

func TestQuotaProbeRateLimitKeepsBlockAndReschedules(t *testing.T) {
	for _, path := range []string{"selector", "scheduler"} {
		t.Run(path, func(t *testing.T) {
			withQuotaProbeInterval(t, 60)
			manager, blocked, model := quotaProbeFixture(t, path, time.Now().Add(-time.Second))
			if _, errPick := pickThroughPath(t, manager, path, model); errPick != nil {
				t.Fatalf("probe pick: %v", errPick)
			}

			retryAfter := 70 * time.Hour
			manager.MarkResult(context.Background(), Result{
				AuthID:          blocked.ID,
				Provider:        "claude",
				Model:           model,
				CredentialScope: true,
				Error:           &Error{HTTPStatus: http.StatusTooManyRequests, Message: "rate limit"},
				RetryAfter:      &retryAfter,
			})

			updated, _ := manager.GetByID(blocked.ID)
			if updated == nil || !updated.Quota.Exceeded || updated.Quota.Reason != "credential_quota" {
				t.Fatalf("failed probe cleared the block: %+v", updated)
			}
			if !updated.Quota.ProbeAfter.After(time.Now()) {
				t.Fatalf("next probe was not rescheduled: %+v", updated.Quota)
			}
			if !updated.Quota.ProbeLeaseUntil.IsZero() {
				t.Fatalf("failed probe stayed in flight: %+v", updated.Quota)
			}
			if blockedNow, _, _ := isAuthBlockedForModel(updated, model, time.Now()); !blockedNow {
				t.Fatal("credential became selectable after a 429 probe")
			}
			if selected, errPick := pickThroughPath(t, manager, path, model); errPick == nil {
				t.Fatalf("pick after 429 probe = %s, want the block to hold", selected.ID)
			}
		})
	}
}
