package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/udit19199/relay/internal/config"
	internalconfig "github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

type blockingServiceCooldownStore struct {
	started chan struct{}
}

func (s *blockingServiceCooldownStore) Load(context.Context) ([]coreauth.CooldownStateRecord, error) {
	return nil, nil
}

func (s *blockingServiceCooldownStore) Save(ctx context.Context, _ []coreauth.CooldownStateRecord) error {
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestConfigCommitDoesNotHoldCommitMutexDuringCooldownPersistence(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{ID: "auth-1", Provider: "xai", Status: coreauth.StatusActive}
	if _, errRegister := manager.Register(coreauth.WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}
	manager.MarkResult(context.Background(), coreauth.Result{
		AuthID: auth.ID, Provider: auth.Provider, Model: "grok-4", Success: false,
		Error: &coreauth.Error{Message: "rate limited", HTTPStatus: http.StatusTooManyRequests},
	})
	store := &blockingServiceCooldownStore{started: make(chan struct{})}
	manager.SetCooldownStateStore(store)
	service := &Service{cfg: &config.Config{}, coreManager: manager}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	applyDone := make(chan bool, 1)
	go func() {
		applyDone <- service.applyConfigUpdateWithAuthSynthesis(ctx, &config.Config{DisableCooling: true}, false)
	}()
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("old cooldown store persistence did not start")
	}

	commitDone := make(chan struct{})
	go func() {
		service.commitConfigUpdate(&config.Config{})
		close(commitDone)
	}()
	select {
	case <-commitDone:
	case <-time.After(time.Second):
		t.Fatal("config commit mutex remained locked during cooldown persistence")
	}

	cancel()
	select {
	case applied := <-applyDone:
		if applied {
			t.Fatal("config runtime apply succeeded after cooldown persistence cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("config runtime apply did not honor cooldown persistence cancellation")
	}
}

func TestServiceSkipsStaleLocalConfigRuntimeApply(t *testing.T) {
	service := &Service{cfg: &config.Config{}}
	var applied []string
	service.applyPprofConfigContextFn = func(_ context.Context, cfg *config.Config) bool {
		applied = append(applied, cfg.Routing.Strategy)
		return true
	}
	first := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{Strategy: "fill-first"}})
	second := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{Strategy: "round-robin"}})
	if !service.applyConfigRuntime(context.Background(), second, false) {
		t.Fatal("newest config runtime apply failed")
	}
	if service.applyConfigRuntime(context.Background(), first, false) {
		t.Fatal("stale config runtime apply succeeded")
	}
	if got, want := strings.Join(applied, ","), "round-robin"; got != want {
		t.Fatalf("runtime apply order = %q, want %q", got, want)
	}
}

func TestServiceAppliesSameValueNewestSelectorCommit(t *testing.T) {
	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	manager.RegisterExecutor(serviceTestCustomExecutor{})
	for _, id := range []string{"auth-b", "auth-a"} {
		if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{ID: id, Provider: "consumer-provider", Status: coreauth.StatusActive}); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", id, errRegister)
		}
	}

	service := &Service{cfg: &config.Config{}, coreManager: manager}
	older := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{Strategy: "fill-first"}})
	newer := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{Strategy: "fill-first"}})
	if !service.applyConfigRuntime(context.Background(), newer, false) {
		t.Fatal("newest same-value config runtime apply failed")
	}
	if service.applyConfigRuntime(context.Background(), older, false) {
		t.Fatal("stale same-value config runtime apply succeeded")
	}

	for range 2 {
		selected, errSelect := manager.SelectAuth(context.Background(), "consumer-provider", "", coreexecutor.Options{})
		if errSelect != nil {
			t.Fatalf("SelectAuth() error = %v", errSelect)
		}
		if selected == nil || selected.ID != "auth-a" {
			t.Fatalf("selector picked = %+v, want auth-a from fill-first", selected)
		}
	}
}

func TestBuilderPreservesInitialSelectorForSameRouting(t *testing.T) {
	cfg := &config.Config{
		AuthDir: t.TempDir(),
		Routing: internalconfig.RoutingConfig{
			Strategy:           "fill-first",
			SessionAffinity:    true,
			SessionAffinityTTL: "1h",
		},
	}
	service, errBuild := NewBuilder().
		WithConfig(cfg).
		WithConfigPath(t.TempDir() + "/config.yaml").
		Build()
	if errBuild != nil {
		t.Fatalf("Build() error = %v", errBuild)
	}

	initialSelector := service.coreManager.Selector()
	initialAffinity, ok := initialSelector.(*coreauth.SessionAffinitySelector)
	if !ok {
		t.Fatalf("initial selector = %T, want *SessionAffinitySelector", initialSelector)
	}
	defer initialAffinity.Stop()
	commit := service.commitConfigUpdate(cfg)
	if !service.applyConfigRuntime(context.Background(), commit, false) {
		t.Fatal("same-routing config runtime apply failed")
	}
	if got := service.coreManager.Selector(); got != initialSelector {
		t.Fatalf("same-routing selector = %p, want initial selector %p", got, initialSelector)
	}
}

func TestServiceApplyConfigRuntimePreservesSelectorForUnchangedRouting(t *testing.T) {
	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	initial := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:           "fill-first",
		SessionAffinity:    true,
		SessionAffinityTTL: "1h",
	}})
	if !service.applyConfigRuntime(context.Background(), initial, false) {
		t.Fatal("initial config runtime apply failed")
	}
	initialSelector := manager.Selector()
	initialAffinity, ok := initialSelector.(*coreauth.SessionAffinitySelector)
	if !ok {
		t.Fatalf("initial selector = %T, want *SessionAffinitySelector", initialSelector)
	}
	defer initialAffinity.Stop()

	older := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:           " FILLFIRST ",
		SessionAffinity:    true,
		SessionAffinityTTL: "60m",
	}})
	newer := service.commitConfigUpdate(&config.Config{
		Routing: internalconfig.RoutingConfig{
			Strategy:           "fill-first",
			SessionAffinity:    true,
			SessionAffinityTTL: "1h",
		},
	})
	if !service.applyConfigRuntime(context.Background(), newer, false) {
		t.Fatal("newest same-routing config runtime apply failed")
	}
	if got := manager.Selector(); got != initialSelector {
		t.Fatalf("same-routing selector = %p, want original %p", got, initialSelector)
	}
	if service.applyConfigRuntime(context.Background(), older, false) {
		t.Fatal("stale same-routing config runtime apply succeeded")
	}
	if got := manager.Selector(); got != initialSelector {
		t.Fatalf("stale same-routing selector = %p, want original %p", got, initialSelector)
	}

	changed := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:           "round-robin",
		SessionAffinity:    true,
		SessionAffinityTTL: "1h",
	}})
	if !service.applyConfigRuntime(context.Background(), changed, false) {
		t.Fatal("changed-routing config runtime apply failed")
	}
	changedSelector := manager.Selector()
	if changedSelector == initialSelector {
		t.Fatal("changed-routing selector retained original identity")
	}
	changedAffinity, ok := changedSelector.(*coreauth.SessionAffinitySelector)
	if !ok {
		t.Fatalf("changed selector = %T, want *SessionAffinitySelector", changedSelector)
	}
	defer changedAffinity.Stop()

	unrelated := service.commitConfigUpdate(&config.Config{
		Routing: internalconfig.RoutingConfig{
			Strategy:           "round-robin",
			SessionAffinity:    true,
			SessionAffinityTTL: "1h",
		},
	})
	if !service.applyConfigRuntime(context.Background(), unrelated, false) {
		t.Fatal("unrelated config runtime apply failed")
	}
	if got := manager.Selector(); got != changedSelector {
		t.Fatalf("unrelated-update selector = %p, want changed selector %p", got, changedSelector)
	}
}

func TestServiceApplyConfigRuntimeSessionAffinitySubagentsChangeRecreatesSelector(t *testing.T) {
	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	subagentsEnabled := true
	initial := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                 "round-robin",
		SessionAffinity:          true,
		SessionAffinityTTL:       "1h",
		SessionAffinitySubagents: &subagentsEnabled,
	}})
	if !service.applyConfigRuntime(context.Background(), initial, false) {
		t.Fatal("initial config runtime apply failed")
	}
	initialSelector := manager.Selector()

	subagentsDisabled := false
	changed := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                 "round-robin",
		SessionAffinity:          true,
		SessionAffinityTTL:       "1h",
		SessionAffinitySubagents: &subagentsDisabled,
	}})
	if !service.applyConfigRuntime(context.Background(), changed, false) {
		t.Fatal("changed config runtime apply failed")
	}
	changedSelector := manager.Selector()
	if changedSelector == initialSelector {
		t.Fatal("changing SessionAffinitySubagents should recreate selector")
	}
}

func TestServiceApplyConfigRuntimeSessionAffinityDisabledSubagentsChangeIsNoOp(t *testing.T) {
	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	subagentsEnabled := true
	initial := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                 "round-robin",
		SessionAffinity:          false,
		SessionAffinityTTL:       "1h",
		SessionAffinitySubagents: &subagentsEnabled,
	}})
	if !service.applyConfigRuntime(context.Background(), initial, false) {
		t.Fatal("initial config runtime apply failed")
	}
	initialSelector := manager.Selector()

	subagentsDisabled := false
	changed := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                 "round-robin",
		SessionAffinity:          false,
		SessionAffinityTTL:       "1h",
		SessionAffinitySubagents: &subagentsDisabled,
	}})
	if !service.applyConfigRuntime(context.Background(), changed, false) {
		t.Fatal("changed config runtime apply failed")
	}
	changedSelector := manager.Selector()
	if changedSelector != initialSelector {
		t.Fatal("toggling SessionAffinitySubagents when SessionAffinity is false must be a no-op")
	}
}

func TestServiceApplyConfigRuntimeSessionAffinityRebindOnChangeRecreatesSelector(t *testing.T) {
	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	initial := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                "round-robin",
		SessionAffinity:         true,
		SessionAffinityTTL:      "1h",
		SessionAffinityRebindOn: "any",
	}})
	if !service.applyConfigRuntime(context.Background(), initial, false) {
		t.Fatal("initial config runtime apply failed")
	}
	initialSelector := manager.Selector()

	same := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                "round-robin",
		SessionAffinity:         true,
		SessionAffinityTTL:      "1h",
		SessionAffinityRebindOn: "",
	}})
	if !service.applyConfigRuntime(context.Background(), same, false) {
		t.Fatal("same config runtime apply failed")
	}
	if manager.Selector() != initialSelector {
		t.Fatal("equivalent default SessionAffinityRebindOn should preserve selector")
	}

	changed := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                "round-robin",
		SessionAffinity:         true,
		SessionAffinityTTL:      "1h",
		SessionAffinityRebindOn: "quota",
	}})
	if !service.applyConfigRuntime(context.Background(), changed, false) {
		t.Fatal("changed config runtime apply failed")
	}
	changedSelector := manager.Selector()
	if changedSelector == initialSelector {
		t.Fatal("changing SessionAffinityRebindOn should recreate selector")
	}

	neverCfg := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                "round-robin",
		SessionAffinity:         true,
		SessionAffinityTTL:      "1h",
		SessionAffinityRebindOn: "never",
	}})
	if !service.applyConfigRuntime(context.Background(), neverCfg, false) {
		t.Fatal("never config runtime apply failed")
	}
	neverSelector := manager.Selector()
	if neverSelector == changedSelector {
		t.Fatal("changing SessionAffinityRebindOn to never should recreate selector")
	}

	sameNeverCfg := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                "round-robin",
		SessionAffinity:         true,
		SessionAffinityTTL:      "1h",
		SessionAffinityRebindOn: " NEVER ",
	}})
	if !service.applyConfigRuntime(context.Background(), sameNeverCfg, false) {
		t.Fatal("same never config runtime apply failed")
	}
	if manager.Selector() != neverSelector {
		t.Fatal("equivalent never SessionAffinityRebindOn should preserve selector")
	}

	backToAnyCfg := service.commitConfigUpdate(&config.Config{Routing: internalconfig.RoutingConfig{
		Strategy:                "round-robin",
		SessionAffinity:         true,
		SessionAffinityTTL:      "1h",
		SessionAffinityRebindOn: "any",
	}})
	if !service.applyConfigRuntime(context.Background(), backToAnyCfg, false) {
		t.Fatal("back to any config runtime apply failed")
	}
	if manager.Selector() == neverSelector {
		t.Fatal("changing SessionAffinityRebindOn from never to any should recreate selector")
	}
}
