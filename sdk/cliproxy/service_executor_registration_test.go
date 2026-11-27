package cliproxy

import (
	"context"
	"net/http"
	"testing"

	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

type serviceTestCustomExecutor struct{}
type serviceTestSDKExecutor struct{ serviceTestCustomExecutor }

func (serviceTestSDKExecutor) Identifier() string { return "sdk-provider" }

func (serviceTestCustomExecutor) Identifier() string {
	return "custom-provider"
}

func (serviceTestCustomExecutor) Execute(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (serviceTestCustomExecutor) ExecuteStream(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (serviceTestCustomExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (serviceTestCustomExecutor) CountTokens(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (serviceTestCustomExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestRegisterAvailableExecutors(t *testing.T) {
	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}
	service.registerAvailableExecutors(nil, executorRegistrationOptions{
		includeBaseline: true,
	})

	providers := []string{
		"codex",
		"claude",
		"xai",
		"devin",
		"meta",
		"openai-compatibility",
	}
	for _, provider := range providers {
		resolved, ok := service.coreManager.Executor(provider)
		if !ok || resolved == nil {
			t.Fatalf("expected executor for provider %s after registration", provider)
		}
	}

	// Fork: Google families and Antigravity must not register executors.
	for _, provider := range []string{
		"gemini",
		"gemini-interactions",
		"vertex",
		"aistudio",
		"antigravity",
	} {
		if _, ok := service.coreManager.Executor(provider); ok {
			t.Fatalf("fork: executor for stripped provider %s must not be registered", provider)
		}
	}
}

func TestRegisterExecutorForAuth_OpenAICompatUsesNamespacedProviderKey(t *testing.T) {
	testCases := []struct {
		name  string
		auths []*coreauth.Auth
	}{
		{
			name: "native first",
			auths: []*coreauth.Auth{
				{ID: "native-meta", Provider: "meta"},
				openAICompatMetaAuth(),
			},
		},
		{
			name: "compat first",
			auths: []*coreauth.Auth{
				openAICompatMetaAuth(),
				{ID: "native-meta", Provider: "meta"},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				cfg:         &config.Config{},
				coreManager: coreauth.NewManager(nil, nil, nil),
			}

			service.registerExecutorsForAuths(tt.auths, true)

			nativeExecutor, okNative := service.coreManager.Executor("meta")
			if !okNative {
				t.Fatal("expected native meta executor")
			}
			if _, okKimi := nativeExecutor.(*runtimeexecutor.MetaExecutor); !okKimi {
				t.Fatalf("native executor type = %T, want *executor.MetaExecutor", nativeExecutor)
			}

			compatExecutor, okCompat := service.coreManager.Executor("openai-compatible-meta")
			if !okCompat {
				t.Fatal("expected namespaced OpenAI-compatible executor")
			}
			if _, okOpenAICompat := compatExecutor.(*runtimeexecutor.OpenAICompatExecutor); !okOpenAICompat {
				t.Fatalf("compat executor type = %T, want *executor.OpenAICompatExecutor", compatExecutor)
			}
		})
	}
}

func openAICompatMetaAuth() *coreauth.Auth {
	return &coreauth.Auth{
		ID:       "compat-meta",
		Provider: "openai-compatibility",
		Label:    "meta",
		Attributes: map[string]string{
			"compat_name":  "meta",
			"provider_key": "meta",
		},
	}
}
