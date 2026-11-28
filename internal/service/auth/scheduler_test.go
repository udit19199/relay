package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"time"

	"github.com/udit19199/relay/internal/registry"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

type schedulerTestExecutor struct {
	provider string
}

type schedulerLoadStore struct {
	auths []*Auth
}

func (s *schedulerLoadStore) List(context.Context) ([]*Auth, error) {
	return s.auths, nil
}

func (s *schedulerLoadStore) Save(context.Context, *Auth) (string, error) {
	return "", nil
}

func (s *schedulerLoadStore) Delete(context.Context, string) error {
	return nil
}

func (e schedulerTestExecutor) Identifier() string {
	if e.provider != "" {
		return e.provider
	}
	return "test"
}

func (schedulerTestExecutor) Execute(ctx context.Context, auth *Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, nil
}

func (schedulerTestExecutor) ExecuteStream(ctx context.Context, auth *Auth, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	return nil, nil
}

func (schedulerTestExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (schedulerTestExecutor) CountTokens(ctx context.Context, auth *Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, nil
}

func (schedulerTestExecutor) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	return nil, nil
}

type trackingSelector struct {
	calls      int
	lastAuthID []string
}

func (s *trackingSelector) Pick(ctx context.Context, provider, model string, opts coreexecutor.Options, auths []*Auth) (*Auth, error) {
	s.calls++
	s.lastAuthID = s.lastAuthID[:0]
	for _, auth := range auths {
		s.lastAuthID = append(s.lastAuthID, auth.ID)
	}
	if len(auths) == 0 {
		return nil, nil
	}
	return auths[len(auths)-1], nil
}

func newSchedulerForTest(auths ...*Auth) *authScheduler {
	scheduler := newAuthScheduler()
	scheduler.rebuild(auths)
	return scheduler
}

func registerSchedulerModels(t *testing.T, provider string, model string, authIDs ...string) {
	t.Helper()
	reg := registry.GetGlobalRegistry()
	for _, authID := range authIDs {
		reg.RegisterClient(authID, provider, []*registry.ModelInfo{{ID: model}})
	}
	t.Cleanup(func() {
		for _, authID := range authIDs {
			reg.UnregisterClient(authID)
		}
	})
}

func TestSchedulerPick_RoundRobinHighestPriority(t *testing.T) {
	t.Parallel()

	scheduler := newSchedulerForTest(
		&Auth{ID: "low", Provider: "gemini", Attributes: map[string]string{"priority": "0"}},
		&Auth{ID: "high-b", Provider: "gemini", Attributes: map[string]string{"priority": "10"}},
		&Auth{ID: "high-a", Provider: "gemini", Attributes: map[string]string{"priority": "10"}},
	)

	want := []string{"high-a", "high-b", "high-a"}
	for index, wantID := range want {
		got, errPick := scheduler.pickSingle(context.Background(), "gemini", "", coreexecutor.Options{}, nil)
		if errPick != nil {
			t.Fatalf("pickSingle() #%d error = %v", index, errPick)
		}
		if got == nil {
			t.Fatalf("pickSingle() #%d auth = nil", index)
		}
		if got.ID != wantID {
			t.Fatalf("pickSingle() #%d auth.ID = %q, want %q", index, got.ID, wantID)
		}
	}
}

func TestManagerLegacyRoundRobinKeepsIndependentAliasPrefixedModelState(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.executors["gemini"] = schedulerTestExecutor{}

	auths := []*Auth{
		{ID: "a-first", Provider: "gemini"},
		{ID: "a-second", Provider: "gemini"},
		{ID: "b-first", Provider: "gemini"},
		{ID: "b-second", Provider: "gemini"},
	}
	for _, auth := range auths {
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", auth.ID, errRegister)
		}
	}
	registerSchedulerModels(t, "gemini", "team-a/shared", "a-first", "a-second")
	registerSchedulerModels(t, "gemini", "team-b/shared", "b-first", "b-second")

	counts := make(map[string]int)
	for index := 0; index < 40; index++ {
		for _, model := range []string{"team-a/shared", "team-b/shared"} {
			got, _, errPick := manager.pickNext(context.Background(), "gemini", model, coreexecutor.Options{}, nil)
			if errPick != nil {
				t.Fatalf("pickNext(%q) #%d error = %v", model, index, errPick)
			}
			counts[got.ID]++
		}
	}
	want := map[string]int{"a-first": 20, "a-second": 20, "b-first": 20, "b-second": 20}
	for authID, wantCount := range want {
		if counts[authID] != wantCount {
			t.Fatalf("auth %q picks = %d, want %d; all=%#v", authID, counts[authID], wantCount, counts)
		}
	}
}

func TestSchedulerPick_PromotesExpiredCooldownBeforePick(t *testing.T) {
	t.Parallel()

	model := "gemini-2.5-pro"
	registerSchedulerModels(t, "gemini", model, "cooldown-expired")
	scheduler := newSchedulerForTest(
		&Auth{
			ID:       "cooldown-expired",
			Provider: "gemini",
			ModelStates: map[string]*ModelState{
				model: {
					Status:         StatusError,
					Unavailable:    true,
					NextRetryAfter: time.Now().Add(-1 * time.Second),
				},
			},
		},
	)

	got, errPick := scheduler.pickSingle(context.Background(), "gemini", model, coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickSingle() error = %v", errPick)
	}
	if got == nil {
		t.Fatalf("pickSingle() auth = nil")
	}
	if got.ID != "cooldown-expired" {
		t.Fatalf("pickSingle() auth.ID = %q, want %q", got.ID, "cooldown-expired")
	}
}

func TestSchedulerPick_CodexWebsocketPrefersWebsocketEnabledSubset(t *testing.T) {
	t.Parallel()

	scheduler := newSchedulerForTest(
		&Auth{ID: "codex-http", Provider: "codex"},
		&Auth{ID: "codex-ws-a", Provider: "codex", Attributes: map[string]string{"websockets": "true"}},
		&Auth{ID: "codex-ws-b", Provider: "codex", Attributes: map[string]string{"websockets": "true"}},
	)

	ctx := coreexecutor.WithDownstreamWebsocket(context.Background())
	want := []string{"codex-ws-a", "codex-ws-b", "codex-ws-a"}
	for index, wantID := range want {
		got, errPick := scheduler.pickSingle(ctx, "codex", "", coreexecutor.Options{}, nil)
		if errPick != nil {
			t.Fatalf("pickSingle() #%d error = %v", index, errPick)
		}
		if got == nil {
			t.Fatalf("pickSingle() #%d auth = nil", index)
		}
		if got.ID != wantID {
			t.Fatalf("pickSingle() #%d auth.ID = %q, want %q", index, got.ID, wantID)
		}
	}
}

func TestSchedulerPick_XAIWebsocketPrefersWebsocketEnabledSubset(t *testing.T) {
	t.Parallel()

	scheduler := newSchedulerForTest(
		&Auth{ID: "xai-http", Provider: "xai"},
		&Auth{ID: "xai-ws-a", Provider: "xai", Attributes: map[string]string{"websockets": "true"}},
		&Auth{ID: "xai-ws-b", Provider: "xai", Attributes: map[string]string{"websockets": "true"}},
	)

	ctx := coreexecutor.WithDownstreamWebsocket(context.Background())
	want := []string{"xai-ws-a", "xai-ws-b", "xai-ws-a"}
	for index, wantID := range want {
		got, errPick := scheduler.pickSingle(ctx, "xai", "", coreexecutor.Options{}, nil)
		if errPick != nil {
			t.Fatalf("pickSingle() #%d error = %v", index, errPick)
		}
		if got == nil {
			t.Fatalf("pickSingle() #%d auth = nil", index)
		}
		if got.ID != wantID {
			t.Fatalf("pickSingle() #%d auth.ID = %q, want %q", index, got.ID, wantID)
		}
	}
}

func TestSchedulerPick_CodexWebsocketPrefersWebsocketEnabledAcrossPriorities(t *testing.T) {
	t.Parallel()

	scheduler := newSchedulerForTest(
		&Auth{ID: "codex-http", Provider: "codex", Attributes: map[string]string{"priority": "10"}},
		&Auth{ID: "codex-ws-a", Provider: "codex", Attributes: map[string]string{"priority": "0", "websockets": "true"}},
		&Auth{ID: "codex-ws-b", Provider: "codex", Attributes: map[string]string{"priority": "0", "websockets": "true"}},
	)

	ctx := coreexecutor.WithDownstreamWebsocket(context.Background())
	want := []string{"codex-ws-a", "codex-ws-b", "codex-ws-a"}
	for index, wantID := range want {
		got, errPick := scheduler.pickSingle(ctx, "codex", "", coreexecutor.Options{}, nil)
		if errPick != nil {
			t.Fatalf("pickSingle() #%d error = %v", index, errPick)
		}
		if got == nil {
			t.Fatalf("pickSingle() #%d auth = nil", index)
		}
		if got.ID != wantID {
			t.Fatalf("pickSingle() #%d auth.ID = %q, want %q", index, got.ID, wantID)
		}
	}
}

func TestSchedulerPick_MixedProvidersUsesWeightedProviderRotationOverReadyCandidates(t *testing.T) {
	t.Parallel()

	scheduler := newSchedulerForTest(
		&Auth{ID: "gemini-a", Provider: "gemini"},
		&Auth{ID: "gemini-b", Provider: "gemini"},
		&Auth{ID: "claude-a", Provider: "claude"},
	)

	wantProviders := []string{"gemini", "gemini", "claude", "gemini"}
	wantIDs := []string{"gemini-a", "gemini-b", "claude-a", "gemini-a"}
	for index := range wantProviders {
		got, provider, errPick := scheduler.pickMixed(context.Background(), []string{"gemini", "claude"}, "", coreexecutor.Options{}, nil)
		if errPick != nil {
			t.Fatalf("pickMixed() #%d error = %v", index, errPick)
		}
		if got == nil {
			t.Fatalf("pickMixed() #%d auth = nil", index)
		}
		if provider != wantProviders[index] {
			t.Fatalf("pickMixed() #%d provider = %q, want %q", index, provider, wantProviders[index])
		}
		if got.ID != wantIDs[index] {
			t.Fatalf("pickMixed() #%d auth.ID = %q, want %q", index, got.ID, wantIDs[index])
		}
	}
}

func TestReadyViewRoundRobinPreservesSuccessorAcrossRebuild(t *testing.T) {
	t.Parallel()

	entry := func(id string) *scheduledAuth {
		return &scheduledAuth{auth: &Auth{ID: id}}
	}

	t.Run("a cooling resumes at b", func(t *testing.T) {
		original := readyView{
			flat: []*scheduledAuth{entry("A"), entry("B"), entry("C")},
		}
		if got := original.pickRoundRobin(nil); got == nil || got.auth.ID != "A" {
			t.Fatalf("first pick = %v, want A", got)
		}

		state := snapshotReadyViewCursors(original)
		rebuilt := readyView{
			flat: []*scheduledAuth{entry("B"), entry("C")},
		}
		restoreReadyViewCursors(&rebuilt, state)

		got := rebuilt.pickRoundRobin(nil)
		if got == nil || got.auth.ID != "B" {
			t.Fatalf("pick after A cooldown = %v, want B", got)
		}
	})

	t.Run("b cooling resumes at c", func(t *testing.T) {
		original := readyView{
			flat: []*scheduledAuth{entry("A"), entry("B"), entry("C")},
		}
		// Pick A, then B
		if got := original.pickRoundRobin(nil); got == nil || got.auth.ID != "A" {
			t.Fatalf("first pick = %v, want A", got)
		}
		if got := original.pickRoundRobin(nil); got == nil || got.auth.ID != "B" {
			t.Fatalf("second pick = %v, want B", got)
		}

		state := snapshotReadyViewCursors(original)
		rebuilt := readyView{
			flat: []*scheduledAuth{entry("A"), entry("C")},
		}
		restoreReadyViewCursors(&rebuilt, state)

		got := rebuilt.pickRoundRobin(nil)
		if got == nil || got.auth.ID != "C" {
			t.Fatalf("pick after B cooldown = %v, want C", got)
		}
	})

	t.Run("c cooling wraps to a", func(t *testing.T) {
		original := readyView{
			flat: []*scheduledAuth{entry("A"), entry("B"), entry("C")},
		}
		// Pick A, B, C
		for _, want := range []string{"A", "B", "C"} {
			if got := original.pickRoundRobin(nil); got == nil || got.auth.ID != want {
				t.Fatalf("pick = %v, want %s", got, want)
			}
		}

		state := snapshotReadyViewCursors(original)
		rebuilt := readyView{
			flat: []*scheduledAuth{entry("A"), entry("B")},
		}
		restoreReadyViewCursors(&rebuilt, state)

		got := rebuilt.pickRoundRobin(nil)
		if got == nil || got.auth.ID != "A" {
			t.Fatalf("pick after C cooldown = %v, want A", got)
		}
	})

	t.Run("recovery preserves successor", func(t *testing.T) {
		original := readyView{
			flat: []*scheduledAuth{entry("B"), entry("C")},
		}
		if got := original.pickRoundRobin(nil); got == nil || got.auth.ID != "B" {
			t.Fatalf("first pick = %v, want B", got)
		}

		state := snapshotReadyViewCursors(original)
		// A recovered and is prepended back
		rebuilt := readyView{
			flat: []*scheduledAuth{entry("A"), entry("B"), entry("C")},
		}
		restoreReadyViewCursors(&rebuilt, state)

		got := rebuilt.pickRoundRobin(nil)
		if got == nil || got.auth.ID != "C" {
			t.Fatalf("pick after A recovery = %v, want C", got)
		}
	})

	t.Run("retry exclusion resumes without rebuild", func(t *testing.T) {
		view := readyView{
			flat: []*scheduledAuth{entry("A"), entry("B"), entry("C")},
		}
		if got := view.pickRoundRobin(nil); got == nil || got.auth.ID != "A" {
			t.Fatalf("first pick = %v, want A", got)
		}
		got := view.pickRoundRobin(func(candidate *scheduledAuth) bool {
			return candidate.auth.ID != "B"
		})
		if got == nil || got.auth.ID != "C" {
			t.Fatalf("pick after excluding B = %v, want C", got)
		}
	})

	t.Run("multiple cooldown skips to first surviving successor", func(t *testing.T) {
		original := readyView{
			flat: []*scheduledAuth{entry("A"), entry("B"), entry("C"), entry("D")},
		}
		if got := original.pickRoundRobin(nil); got == nil || got.auth.ID != "A" {
			t.Fatalf("first pick = %v, want A", got)
		}

		state := snapshotReadyViewCursors(original)
		rebuilt := readyView{
			flat: []*scheduledAuth{entry("C"), entry("D")},
		}
		restoreReadyViewCursors(&rebuilt, state)

		got := rebuilt.pickRoundRobin(nil)
		if got == nil || got.auth.ID != "C" {
			t.Fatalf("pick after A and B cooldown = %v, want C", got)
		}
	})
}

func TestScheduledSuccessorIndex_WrapsAndSkipsFilteredCandidates(t *testing.T) {
	t.Parallel()

	entries := []*scheduledAuth{
		{auth: &Auth{ID: "aaa"}},
		{auth: &Auth{ID: "ccc"}},
		{auth: &Auth{ID: "eee"}},
	}
	tests := []struct {
		name   string
		lastID string
		want   int
	}{
		{name: "no previous pick starts at head", lastID: "", want: 0},
		{name: "resumes after previous pick", lastID: "aaa", want: 1},
		{name: "resumes after filtered-out pick", lastID: "bbb", want: 1},
		{name: "wraps at the end of the ring", lastID: "eee", want: 0},
		{name: "wraps for removed trailing pick", lastID: "zzz", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scheduledSuccessorIndex(entries, tt.lastID); got != tt.want {
				t.Fatalf("scheduledSuccessorIndex(%q) = %d, want %d", tt.lastID, got, tt.want)
			}
		})
	}
	if got := scheduledSuccessorIndex(nil, "aaa"); got != 0 {
		t.Fatalf("scheduledSuccessorIndex(nil, aaa) = %d, want 0", got)
	}
}

func TestManagerRoundRobinPreservesSuccessorAcrossCooldown(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	model := "test-successor-model"
	authIDs := []string{"successor-auth-a", "successor-auth-b", "successor-auth-c"}
	registerSchedulerModels(t, "gemini", model, authIDs...)

	for _, id := range authIDs {
		if _, errRegister := manager.Register(context.Background(), &Auth{ID: id, Provider: "gemini"}); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", id, errRegister)
		}
	}

	got, errPick := manager.scheduler.pickSingle(context.Background(), "gemini", model, coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickSingle #1 error = %v", errPick)
	}
	if got == nil || got.ID != "successor-auth-a" {
		t.Fatalf("pickSingle #1 = %v, want successor-auth-a", got)
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   "successor-auth-a",
		Provider: "gemini",
		Model:    model,
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "rate limit"},
	})

	got, errPick = manager.scheduler.pickSingle(context.Background(), "gemini", model, coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickSingle #2 after successor-auth-a cooldown error = %v", errPick)
	}
	if got == nil || got.ID != "successor-auth-b" {
		t.Fatalf("pickSingle #2 after successor-auth-a cooldown = %v, want successor-auth-b", got)
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   "successor-auth-b",
		Provider: "gemini",
		Model:    model,
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "rate limit"},
	})

	got, errPick = manager.scheduler.pickSingle(context.Background(), "gemini", model, coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickSingle #3 after successor-auth-b cooldown error = %v", errPick)
	}
	if got == nil || got.ID != "successor-auth-c" {
		t.Fatalf("pickSingle #3 after successor-auth-b cooldown = %v, want successor-auth-c", got)
	}
}

func TestSchedulerPick_RoundRobinPreservesWebsocketSuccessorAcrossCooldown(t *testing.T) {
	t.Parallel()

	wsA := &Auth{ID: "codex-ws-a", Provider: "codex", Attributes: map[string]string{"websockets": "true"}}
	wsB := &Auth{ID: "codex-ws-b", Provider: "codex", Attributes: map[string]string{"websockets": "true"}}
	wsC := &Auth{ID: "codex-ws-c", Provider: "codex", Attributes: map[string]string{"websockets": "true"}}
	httpOnly := &Auth{ID: "codex-http", Provider: "codex"}
	scheduler := newSchedulerForTest(httpOnly, wsA, wsB, wsC)

	ctx := coreexecutor.WithDownstreamWebsocket(context.Background())
	got, errPick := scheduler.pickSingle(ctx, "codex", "", coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickSingle() first error = %v", errPick)
	}
	if got == nil || got.ID != "codex-ws-a" {
		t.Fatalf("pickSingle() first = %v, want codex-ws-a", got)
	}

	wsA.Unavailable = true
	wsA.NextRetryAfter = time.Now().Add(time.Hour)
	scheduler.upsertAuth(wsA)

	got, errPick = scheduler.pickSingle(ctx, "codex", "", coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickSingle() after ws-a cooldown error = %v", errPick)
	}
	if got == nil || got.ID != "codex-ws-b" {
		t.Fatalf("pickSingle() after ws-a cooldown = %v, want codex-ws-b", got)
	}
}

func TestSchedulerPick_MixedProvidersPrefersHighestPriorityTier(t *testing.T) {
	t.Parallel()

	model := "gpt-default"
	registerSchedulerModels(t, "provider-low", model, "low")
	registerSchedulerModels(t, "provider-high-a", model, "high-a")
	registerSchedulerModels(t, "provider-high-b", model, "high-b")

	scheduler := newSchedulerForTest(
		&Auth{ID: "low", Provider: "provider-low", Attributes: map[string]string{"priority": "4"}},
		&Auth{ID: "high-a", Provider: "provider-high-a", Attributes: map[string]string{"priority": "7"}},
		&Auth{ID: "high-b", Provider: "provider-high-b", Attributes: map[string]string{"priority": "7"}},
	)

	providers := []string{"provider-low", "provider-high-a", "provider-high-b"}
	wantProviders := []string{"provider-high-a", "provider-high-b", "provider-high-a", "provider-high-b"}
	wantIDs := []string{"high-a", "high-b", "high-a", "high-b"}
	for index := range wantProviders {
		got, provider, errPick := scheduler.pickMixed(context.Background(), providers, model, coreexecutor.Options{}, nil)
		if errPick != nil {
			t.Fatalf("pickMixed() #%d error = %v", index, errPick)
		}
		if got == nil {
			t.Fatalf("pickMixed() #%d auth = nil", index)
		}
		if provider != wantProviders[index] {
			t.Fatalf("pickMixed() #%d provider = %q, want %q", index, provider, wantProviders[index])
		}
		if got.ID != wantIDs[index] {
			t.Fatalf("pickMixed() #%d auth.ID = %q, want %q", index, got.ID, wantIDs[index])
		}
	}
}

func TestManager_PickNextMixed_UsesWeightedProviderRotationBeforeCredentialRotation(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.executors["gemini"] = schedulerTestExecutor{}
	manager.executors["claude"] = schedulerTestExecutor{}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "gemini-a", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(gemini-a) error = %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "gemini-b", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(gemini-b) error = %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "claude-a", Provider: "claude"}); errRegister != nil {
		t.Fatalf("Register(claude-a) error = %v", errRegister)
	}

	wantProviders := []string{"gemini", "gemini", "claude", "gemini"}
	wantIDs := []string{"gemini-a", "gemini-b", "claude-a", "gemini-a"}
	for index := range wantProviders {
		got, _, provider, errPick := manager.pickNextMixed(context.Background(), []string{"gemini", "claude"}, "", coreexecutor.Options{}, map[string]struct{}{})
		if errPick != nil {
			t.Fatalf("pickNextMixed() #%d error = %v", index, errPick)
		}
		if got == nil {
			t.Fatalf("pickNextMixed() #%d auth = nil", index)
		}
		if provider != wantProviders[index] {
			t.Fatalf("pickNextMixed() #%d provider = %q, want %q", index, provider, wantProviders[index])
		}
		if got.ID != wantIDs[index] {
			t.Fatalf("pickNextMixed() #%d auth.ID = %q, want %q", index, got.ID, wantIDs[index])
		}
	}
}

func TestManager_PickNextMixed_DisallowFreeAuthSkipsCodexFreePlan(t *testing.T) {
	t.Parallel()

	model := "gpt-5.4-mini"
	registerSchedulerModels(t, "codex", model, "codex-a-free", "codex-b-plus")

	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.executors["codex"] = schedulerTestExecutor{}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "codex-a-free", Provider: "codex", Attributes: map[string]string{"plan_type": "free"}}); errRegister != nil {
		t.Fatalf("Register(codex-a-free) error = %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "codex-b-plus", Provider: "codex", Attributes: map[string]string{"plan_type": "plus"}}); errRegister != nil {
		t.Fatalf("Register(codex-b-plus) error = %v", errRegister)
	}

	opts := coreexecutor.Options{
		Metadata: map[string]any{coreexecutor.DisallowFreeAuthMetadataKey: true},
	}
	got, _, provider, errPick := manager.pickNextMixed(context.Background(), []string{"codex"}, model, opts, map[string]struct{}{})
	if errPick != nil {
		t.Fatalf("pickNextMixed() error = %v", errPick)
	}
	if got == nil {
		t.Fatalf("pickNextMixed() auth = nil")
	}
	if provider != "codex" {
		t.Fatalf("pickNextMixed() provider = %q, want %q", provider, "codex")
	}
	if got.ID != "codex-b-plus" {
		t.Fatalf("pickNextMixed() auth.ID = %q, want %q", got.ID, "codex-b-plus")
	}
}

func TestManagerCodexAlphaSearchPolicyRejectsOrdinaryAPIKey(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.executors["codex"] = schedulerTestExecutor{}
	if _, errRegister := manager.Register(context.Background(), &Auth{
		ID:         "ordinary-api-key",
		Provider:   "codex",
		Attributes: map[string]string{AttributeAPIKey: "ordinary"},
	}); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	selected, errSelect := manager.SelectAuthWithCredentialPolicy(context.Background(), "codex", "", CredentialPolicyCodexAlphaSearchV1, coreexecutor.Options{})
	if selected != nil {
		t.Fatalf("SelectAuthWithCredentialPolicy() auth = %#v, want nil", selected)
	}
	var authErr *Error
	if !errors.As(errSelect, &authErr) || authErr.Code != "auth_not_found" {
		t.Fatalf("SelectAuthWithCredentialPolicy() error = %#v, want auth_not_found", errSelect)
	}
}

func TestManagerRoundRobinDisallowFreeAuthSkipsFreePlan(t *testing.T) {
	tests := []struct {
		name  string
		mixed bool
	}{
		{name: "single provider"},
		{name: "mixed providers", mixed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(nil, &RoundRobinSelector{}, nil)
			manager.executors["codex"] = schedulerTestExecutor{}
			lightProvider := "codex"
			if tt.mixed {
				lightProvider = "gemini"
				manager.executors["gemini"] = schedulerTestExecutor{provider: "gemini"}
			}
			for _, candidate := range []*Auth{
				{ID: "free-high", Provider: "codex", Attributes: map[string]string{"plan_type": "free"}, Metadata: map[string]any{"access_token": "free-token"}},
				{ID: "paid-heavy", Provider: "codex", Attributes: map[string]string{"plan_type": "plus"}, Metadata: map[string]any{"access_token": "heavy-token"}},
				{ID: "paid-light", Provider: lightProvider, Attributes: map[string]string{"plan_type": "plus"}, Metadata: map[string]any{"access_token": "light-token"}},
			} {
				if _, errRegister := manager.Register(context.Background(), candidate); errRegister != nil {
					t.Fatalf("Register(%s) error = %v", candidate.ID, errRegister)
				}
			}

			opts := coreexecutor.Options{Metadata: map[string]any{coreexecutor.DisallowFreeAuthMetadataKey: true}}
			counts := make(map[string]int)
			for index := 0; index < 600; index++ {
				var selected *Auth
				var errPick error
				if tt.mixed {
					selected, _, _, errPick = manager.pickNextMixed(context.Background(), []string{"codex", "gemini"}, "", opts, nil)
				} else {
					selected, _, errPick = manager.pickNext(context.Background(), "codex", "", opts, nil)
				}
				if errPick != nil {
					t.Fatalf("pick #%d error = %v", index, errPick)
				}
				counts[selected.ID]++
			}
			if counts["paid-heavy"]+counts["paid-light"] != 600 || counts["free-high"] != 0 {
				t.Fatalf("non-free picks = %#v, want only paid auths and no free auth", counts)
			}
		})
	}
}

func TestManagerSelectAuthByKindRoundRobinKeepsEligibleRotation(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.executors["codex"] = schedulerTestExecutor{}
	for _, candidate := range []*Auth{
		{ID: "api-key", Provider: "codex", Attributes: map[string]string{AttributeAPIKey: "test-key"}},
		{ID: "oauth-a", Provider: "codex", Metadata: map[string]any{"access_token": "token-a"}},
		{ID: "oauth-b", Provider: "codex", Metadata: map[string]any{"access_token": "token-b"}},
	} {
		if _, errRegister := manager.Register(context.Background(), candidate); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", candidate.ID, errRegister)
		}
	}

	counts := make(map[string]int)
	for index := 0; index < 6; index++ {
		selected, errSelect := manager.SelectAuthByKind(context.Background(), "codex", "", AuthKindOAuth, coreexecutor.Options{})
		if errSelect != nil {
			t.Fatalf("SelectAuthByKind() #%d error = %v", index, errSelect)
		}
		counts[selected.ID]++
	}
	if counts["oauth-a"] != 3 || counts["oauth-b"] != 3 || counts["api-key"] != 0 {
		t.Fatalf("round-robin OAuth picks = %#v, want three picks per OAuth auth and no API key", counts)
	}
}

func TestManagerSelectAuthByKindReturnsErrorWhenUnavailable(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.executors["codex"] = schedulerTestExecutor{}
	if _, errRegister := manager.Register(context.Background(), &Auth{
		ID:         "codex-api-key",
		Provider:   "codex",
		Attributes: map[string]string{AttributeAPIKey: "test-key"},
	}); errRegister != nil {
		t.Fatalf("Register(codex-api-key) error = %v", errRegister)
	}

	selected, errSelect := manager.SelectAuthByKind(context.Background(), "codex", "", AuthKindOAuth, coreexecutor.Options{})
	if selected != nil {
		t.Fatalf("SelectAuthByKind() auth = %#v, want nil", selected)
	}
	var authErr *Error
	if !errors.As(errSelect, &authErr) || authErr.Code != "auth_not_found" {
		t.Fatalf("SelectAuthByKind() error = %#v, want auth_not_found", errSelect)
	}
}

func TestManagerSelectAuthByKindRejectsInvalidKind(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	selected, errSelect := manager.SelectAuthByKind(context.Background(), "codex", "", "certificate", coreexecutor.Options{})
	if selected != nil {
		t.Fatalf("SelectAuthByKind() auth = %#v, want nil", selected)
	}
	var authErr *Error
	if !errors.As(errSelect, &authErr) || authErr.Code != "invalid_auth_kind" || authErr.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("SelectAuthByKind() error = %#v, want invalid_auth_kind", errSelect)
	}
}

func TestManagerCustomSelector_FallsBackToLegacyPath(t *testing.T) {
	t.Parallel()

	selector := &trackingSelector{}
	manager := NewManager(nil, selector, nil)
	manager.executors["gemini"] = schedulerTestExecutor{}
	manager.auths["auth-a"] = &Auth{ID: "auth-a", Provider: "gemini"}
	manager.auths["auth-b"] = &Auth{ID: "auth-b", Provider: "gemini"}

	got, _, errPick := manager.pickNext(context.Background(), "gemini", "", coreexecutor.Options{}, map[string]struct{}{})
	if errPick != nil {
		t.Fatalf("pickNext() error = %v", errPick)
	}
	if got == nil {
		t.Fatalf("pickNext() auth = nil")
	}
	if selector.calls != 1 {
		t.Fatalf("selector.calls = %d, want %d", selector.calls, 1)
	}
	if len(selector.lastAuthID) != 2 {
		t.Fatalf("len(selector.lastAuthID) = %d, want %d", len(selector.lastAuthID), 2)
	}
	if got.ID != selector.lastAuthID[len(selector.lastAuthID)-1] {
		t.Fatalf("pickNext() auth.ID = %q, want selector-picked %q", got.ID, selector.lastAuthID[len(selector.lastAuthID)-1])
	}
}

func TestManager_InitializesSchedulerForBuiltInSelector(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	if manager.scheduler == nil {
		t.Fatalf("manager.scheduler = nil")
	}
}

func TestManager_SchedulerTracksRegisterAndUpdate(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "auth-b", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(auth-b) error = %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "auth-a", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(auth-a) error = %v", errRegister)
	}

	got, errPick := manager.scheduler.pickSingle(context.Background(), "gemini", "", coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("scheduler.pickSingle() error = %v", errPick)
	}
	if got == nil || got.ID != "auth-a" {
		t.Fatalf("scheduler.pickSingle() auth = %v, want auth-a", got)
	}

	if _, errUpdate := manager.Update(context.Background(), &Auth{ID: "auth-a", Provider: "gemini", Disabled: true}); errUpdate != nil {
		t.Fatalf("Update(auth-a) error = %v", errUpdate)
	}

	got, errPick = manager.scheduler.pickSingle(context.Background(), "gemini", "", coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("scheduler.pickSingle() after update error = %v", errPick)
	}
	if got == nil || got.ID != "auth-b" {
		t.Fatalf("scheduler.pickSingle() after update auth = %v, want auth-b", got)
	}
}

func TestManager_PickNextMixed_UsesSchedulerRotation(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.executors["gemini"] = schedulerTestExecutor{}
	manager.executors["claude"] = schedulerTestExecutor{}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "gemini-a", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(gemini-a) error = %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "gemini-b", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(gemini-b) error = %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "claude-a", Provider: "claude"}); errRegister != nil {
		t.Fatalf("Register(claude-a) error = %v", errRegister)
	}

	wantProviders := []string{"gemini", "gemini", "claude", "gemini"}
	wantIDs := []string{"gemini-a", "gemini-b", "claude-a", "gemini-a"}
	for index := range wantProviders {
		got, _, provider, errPick := manager.pickNextMixed(context.Background(), []string{"gemini", "claude"}, "", coreexecutor.Options{}, nil)
		if errPick != nil {
			t.Fatalf("pickNextMixed() #%d error = %v", index, errPick)
		}
		if got == nil {
			t.Fatalf("pickNextMixed() #%d auth = nil", index)
		}
		if provider != wantProviders[index] {
			t.Fatalf("pickNextMixed() #%d provider = %q, want %q", index, provider, wantProviders[index])
		}
		if got.ID != wantIDs[index] {
			t.Fatalf("pickNextMixed() #%d auth.ID = %q, want %q", index, got.ID, wantIDs[index])
		}
	}
}

func TestManager_SchedulerSharesThinkingSuffixCooldownAndRegistryState(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	baseModel := "scheduler-thinking-model"
	reg.RegisterClient("thinking-auth-a", "gemini", []*registry.ModelInfo{{ID: baseModel}})
	reg.RegisterClient("thinking-auth-b", "gemini", []*registry.ModelInfo{{ID: baseModel}})
	t.Cleanup(func() {
		reg.UnregisterClient("thinking-auth-a")
		reg.UnregisterClient("thinking-auth-b")
	})
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "thinking-auth-a", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(thinking-auth-a) error = %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "thinking-auth-b", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(thinking-auth-b) error = %v", errRegister)
	}

	retryAfter := time.Hour
	manager.MarkResult(context.Background(), Result{
		AuthID:     "thinking-auth-a",
		Provider:   "gemini",
		Model:      baseModel + "(high)",
		Success:    false,
		Error:      &Error{HTTPStatus: 429, Message: "quota"},
		RetryAfter: &retryAfter,
	})

	auth, ok := manager.GetByID("thinking-auth-a")
	if !ok || auth == nil {
		t.Fatal("thinking-auth-a was not found")
	}
	if len(auth.ModelStates) != 1 || auth.ModelStates[baseModel] == nil {
		t.Fatalf("ModelStates = %+v, want only canonical key %q", auth.ModelStates, baseModel)
	}
	// Only thinking-auth-a is cooling down; thinking-auth-b remains available.
	if count := reg.GetModelCount(baseModel); count != 1 {
		t.Fatalf("registry model count during cooldown = %d, want 1", count)
	}
	for _, model := range []string{baseModel, baseModel + "(medium)", baseModel + "(low)"} {
		got, errPick := manager.scheduler.pickSingle(context.Background(), "gemini", model, coreexecutor.Options{}, nil)
		if errPick != nil {
			t.Fatalf("scheduler.pickSingle(%q) error = %v", model, errPick)
		}
		if got == nil || got.ID != "thinking-auth-b" {
			t.Fatalf("scheduler.pickSingle(%q) auth = %v, want thinking-auth-b", model, got)
		}
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   "thinking-auth-a",
		Provider: "gemini",
		Model:    baseModel + "(low)",
		Success:  true,
	})

	auth, ok = manager.GetByID("thinking-auth-a")
	if !ok || auth == nil || auth.ModelStates[baseModel] == nil {
		t.Fatal("canonical model state was not retained after success")
	}
	state := auth.ModelStates[baseModel]
	if state.Unavailable || state.Quota.Exceeded || !state.NextRetryAfter.IsZero() {
		t.Fatalf("canonical model state after success = %+v, want cleared", state)
	}
	if count := reg.GetModelCount(baseModel); count != 2 {
		t.Fatalf("registry model count after recovery = %d, want 2", count)
	}
}

func TestManager_PickNextMixed_SkipsProvidersWithoutExecutors(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.executors["claude"] = schedulerTestExecutor{}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "gemini-a", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(gemini-a) error = %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "claude-a", Provider: "claude"}); errRegister != nil {
		t.Fatalf("Register(claude-a) error = %v", errRegister)
	}

	got, _, provider, errPick := manager.pickNextMixed(context.Background(), []string{"gemini", "claude"}, "", coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickNextMixed() error = %v", errPick)
	}
	if got == nil {
		t.Fatalf("pickNextMixed() auth = nil")
	}
	if provider != "claude" {
		t.Fatalf("pickNextMixed() provider = %q, want %q", provider, "claude")
	}
	if got.ID != "claude-a" {
		t.Fatalf("pickNextMixed() auth.ID = %q, want %q", got.ID, "claude-a")
	}
}

func TestManager_SchedulerTracksMarkResultCooldownAndRecovery(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient("auth-a", "gemini", []*registry.ModelInfo{{ID: "test-model"}})
	reg.RegisterClient("auth-b", "gemini", []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		reg.UnregisterClient("auth-a")
		reg.UnregisterClient("auth-b")
	})
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "auth-a", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(auth-a) error = %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "auth-b", Provider: "gemini"}); errRegister != nil {
		t.Fatalf("Register(auth-b) error = %v", errRegister)
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   "auth-a",
		Provider: "gemini",
		Model:    "test-model",
		Success:  false,
		Error:    &Error{HTTPStatus: 429, Message: "quota"},
	})

	got, errPick := manager.scheduler.pickSingle(context.Background(), "gemini", "test-model", coreexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("scheduler.pickSingle() after cooldown error = %v", errPick)
	}
	if got == nil || got.ID != "auth-b" {
		t.Fatalf("scheduler.pickSingle() after cooldown auth = %v, want auth-b", got)
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   "auth-a",
		Provider: "gemini",
		Model:    "test-model",
		Success:  true,
	})

	seen := make(map[string]struct{}, 2)
	for index := 0; index < 2; index++ {
		got, errPick = manager.scheduler.pickSingle(context.Background(), "gemini", "test-model", coreexecutor.Options{}, nil)
		if errPick != nil {
			t.Fatalf("scheduler.pickSingle() after recovery #%d error = %v", index, errPick)
		}
		if got == nil {
			t.Fatalf("scheduler.pickSingle() after recovery #%d auth = nil", index)
		}
		seen[got.ID] = struct{}{}
	}
	if len(seen) != 2 {
		t.Fatalf("len(seen) = %d, want %d", len(seen), 2)
	}
}
