package service

import (
	"context"
	"path/filepath"
	"testing"

	authlogin "github.com/udit19199/relay/internal/auth/login"
	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
)

type cooldownProviderTokenStore struct {
	cooldownStore coreauth.CooldownStateStore
}

func (s *cooldownProviderTokenStore) List(context.Context) ([]*coreauth.Auth, error) {
	return nil, nil
}

func (s *cooldownProviderTokenStore) Save(context.Context, *coreauth.Auth) (string, error) {
	return "", nil
}

func (s *cooldownProviderTokenStore) Delete(context.Context, string) error {
	return nil
}

func (s *cooldownProviderTokenStore) CooldownStateStore() coreauth.CooldownStateStore {
	return s.cooldownStore
}

type serviceCooldownStateStore struct{}

func (*serviceCooldownStateStore) Load(context.Context) ([]coreauth.CooldownStateRecord, error) {
	return nil, nil
}

func (*serviceCooldownStateStore) Save(context.Context, []coreauth.CooldownStateRecord) error {
	return nil
}

func TestResolveCooldownStateStoreUsesCapturedBackendProvider(t *testing.T) {
	originalStore := authlogin.GetTokenStore()
	t.Cleanup(func() {
		authlogin.RegisterTokenStore(originalStore)
	})

	providedStore := &serviceCooldownStateStore{}
	authlogin.RegisterTokenStore(&cooldownProviderTokenStore{cooldownStore: providedStore})
	cfg := &config.Config{
		AuthDir:            t.TempDir(),
		SaveCooldownStatus: true,
	}
	service, errBuild := NewBuilder().
		WithConfig(cfg).
		WithConfigPath(filepath.Join(t.TempDir(), "config.yaml")).
		Build()
	if errBuild != nil {
		t.Fatalf("Build() error = %v", errBuild)
	}

	authlogin.RegisterTokenStore(&cooldownProviderTokenStore{cooldownStore: &serviceCooldownStateStore{}})
	got := service.resolveCooldownStateStore(cfg)
	if got != providedStore {
		t.Fatalf("resolveCooldownStateStore() = %T, want captured backend-provided store", got)
	}
}
