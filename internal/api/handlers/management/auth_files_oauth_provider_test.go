package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
)

func TestAuthFilesList_ReportsOAuthProvider(t *testing.T) {
	cases := []struct {
		id         string
		attributes map[string]string
		metadata   map[string]any
		want       string
	}{
		{id: "builtin"},
		{id: "attributes-normalized", attributes: map[string]string{"oauth_provider": " Vendor-CN "}, want: "vendor-cn"},
		{id: "metadata-normalized", metadata: map[string]any{"oauth_provider": " Vendor-CN "}, want: "vendor-cn"},
		{id: "attributes-portal-normalized", attributes: map[string]string{"portal": " Vendor-CN "}, want: "vendor-cn"},
		{id: "metadata-portal-normalized", metadata: map[string]any{"portal": " Vendor-CN "}, want: "vendor-cn"},
		{
			id:         "attributes-oauth-over-metadata-oauth",
			attributes: map[string]string{"oauth_provider": " Vendor ", "portal": "ignored-attributes"},
			metadata:   map[string]any{"oauth_provider": "vendor-cn", "portal": "ignored-metadata"},
			want:       "vendor",
		},
		{
			id:         "metadata-oauth-over-attributes-portal",
			attributes: map[string]string{"portal": "ignored-attributes"},
			metadata:   map[string]any{"oauth_provider": " Vendor-CN ", "portal": "ignored-metadata"},
			want:       "vendor-cn",
		},
		{
			id:         "attributes-portal-over-metadata-portal",
			attributes: map[string]string{"portal": " Vendor-CN "},
			metadata:   map[string]any{"portal": "ignored-metadata"},
			want:       "vendor-cn",
		},
		{
			id:         "blank-attributes-oauth",
			attributes: map[string]string{"oauth_provider": "   ", "portal": "ignored"},
			metadata:   map[string]any{"oauth_provider": " Vendor-CN "},
			want:       "vendor-cn",
		},
		{
			id:         "blank-metadata-oauth",
			attributes: map[string]string{"oauth_provider": "   ", "portal": " Vendor-CN "},
			metadata:   map[string]any{"oauth_provider": "   ", "portal": "ignored"},
			want:       "vendor-cn",
		},
		{
			id:         "blank-attributes-portal",
			attributes: map[string]string{"oauth_provider": "   ", "portal": "   "},
			metadata:   map[string]any{"oauth_provider": "   ", "portal": " Vendor-CN "},
			want:       "vendor-cn",
		},
		{id: "numeric-oauth-falls-back", attributes: map[string]string{"portal": "vendor-cn"}, metadata: map[string]any{"oauth_provider": 42}, want: "vendor-cn"},
		{id: "boolean-oauth-falls-back", metadata: map[string]any{"oauth_provider": true, "portal": "vendor-cn"}, want: "vendor-cn"},
		{id: "numeric-portal", metadata: map[string]any{"portal": 42}},
		{id: "boolean-portal", metadata: map[string]any{"portal": true}},
		{id: "non-string-hints", metadata: map[string]any{"oauth_provider": 42, "portal": true}},
		{id: "blank-hints", attributes: map[string]string{"oauth_provider": "   ", "portal": "   "}, metadata: map[string]any{"oauth_provider": "   ", "portal": "   "}},
	}
	manager := coreauth.NewManager(nil, nil, nil)
	for _, tc := range cases {
		attributes := tc.attributes
		if attributes == nil {
			attributes = make(map[string]string)
		}
		// Runtime-only entries do not require files on disk.
		attributes["runtime_only"] = "true"
		if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{
			ID: tc.id, FileName: tc.id + ".json", Provider: "vendor", Attributes: attributes, Metadata: tc.metadata,
		}); errRegister != nil {
			t.Fatalf("register auth %s: %v", tc.id, errRegister)
		}
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	router := oauthProviderTestRouter(h)
	files := listOAuthProviderTestFiles(t, router, "/v8/management/credentials")
	if len(files) != len(cases) {
		t.Fatalf("v8 files count = %d, want %d", len(files), len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			assertOAuthProviderTestFile(t, files, tc.id, "vendor", tc.want)
		})
	}
	legacyFiles := listOAuthProviderTestFiles(t, router, "/v0/management/auth-files")
	if len(legacyFiles) != len(cases) {
		t.Fatalf("v0 files count = %d, want %d", len(legacyFiles), len(cases))
	}
	for id, file := range legacyFiles {
		if value := file.Get("oauth_provider"); value.Exists() {
			t.Errorf("%s: v0 must not expose oauth_provider, got %s", id, value.Raw)
		}
	}
}

func oauthProviderTestRouter(h *Handler) *gin.Engine {
	router := gin.New()
	v8 := router.Group("/v8/management", func(c *gin.Context) {
		c.Set(ConfigV8ContextKey, true)
		c.Next()
	})
	v8.GET("/credentials", h.ListAuthFiles)
	v8.GET("/oauth/auth-url", h.StartOAuthV8)
	v8.GET("/get-auth-status", h.GetAuthStatus)
	router.GET("/v0/management/auth-files", h.ListAuthFiles)
	return router
}

func oauthProviderTestGET(t *testing.T, router http.Handler, path string) ojson.Result {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK || !ojson.ValidBytes(rec.Body.Bytes()) {
		t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
	}
	return ojson.Parse(rec.Body.String())
}

func listOAuthProviderTestFiles(t *testing.T, router http.Handler, path string) map[string]ojson.Result {
	t.Helper()
	resp := oauthProviderTestGET(t, router, path)
	files := resp.Get("files")
	if !files.IsArray() || len(files.Array()) == 0 {
		t.Fatalf("GET %s returned no files: %s", path, resp.Raw)
	}
	out := make(map[string]ojson.Result)
	for _, file := range files.Array() {
		out[file.Get("id").String()] = file
	}
	return out
}

func assertOAuthProviderTestFile(t *testing.T, files map[string]ojson.Result, id, provider, want string) {
	t.Helper()
	file, ok := files[id]
	if !ok {
		t.Fatalf("missing credential %s", id)
	}
	value := file.Get("oauth_provider")
	if value.String() != want || (want == "" && value.Exists()) {
		t.Errorf("%s: oauth_provider = %s, want %q", id, value.Raw, want)
	}
	if file.Get("type").String() != provider || file.Get("provider").String() != provider {
		t.Errorf("%s: credential provider changed: %s", id, file.Raw)
	}
}
