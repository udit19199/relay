package service

import (
	"context"
	"testing"

	internalconfig "github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
)

func TestBuilder_WithResultPolicy(t *testing.T) {
	cfg := &internalconfig.Config{}
	policy := coreauth.ResultPolicyFunc(func(ctx context.Context, result coreauth.Result) coreauth.Result {
		return result
	})

	service, errBuild := NewBuilder().
		WithConfig(cfg).
		WithConfigPath(t.TempDir() + "/config.yaml").
		WithResultPolicy(policy).
		Build()
	if errBuild != nil {
		t.Fatalf("Build() failed: %v", errBuild)
	}
	if service == nil {
		t.Fatal("expected service to be non-nil")
	}

	if got := service.ResultPolicy(); got == nil {
		t.Fatal("expected service.ResultPolicy() to be non-nil")
	}

	// Test updating policy on Service
	policy2 := coreauth.ResultPolicyFunc(func(ctx context.Context, result coreauth.Result) coreauth.Result {
		result.CredentialScope = false
		return result
	})
	service.SetResultPolicy(policy2)
	if service.ResultPolicy() == nil {
		t.Fatal("expected updated policy on service")
	}

	// Test clearing policy on Service
	service.SetResultPolicy(nil)
	if service.ResultPolicy() != nil {
		t.Fatal("expected service.ResultPolicy() to be nil after clearing")
	}
}
