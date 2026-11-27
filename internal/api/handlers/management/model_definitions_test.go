package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/registry"
)

func invokeGetStaticModelDefinitions(t *testing.T, h *Handler, channel string) (*httptest.ResponseRecorder, struct {
	Channel string               `json:"channel"`
	Error   string               `json:"error"`
	Models  []registry.ModelInfo `json:"models"`
}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Params = gin.Params{{Key: "channel", Value: channel}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v8/management/routing/model-definitions/"+channel, nil)
	h.GetStaticModelDefinitions(ctx)

	var body struct {
		Channel string               `json:"channel"`
		Error   string               `json:"error"`
		Models  []registry.ModelInfo `json:"models"`
	}
	if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &body); errUnmarshal != nil {
		t.Fatalf("unmarshal response body %q: %v", rec.Body.String(), errUnmarshal)
	}
	return rec, body
}

func TestGetStaticModelDefinitions_BuiltInChannel(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, nil)

	rec, body := invokeGetStaticModelDefinitions(t, h, "claude")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if body.Channel != "claude" {
		t.Fatalf("channel = %q, want %q", body.Channel, "claude")
	}
	if len(body.Models) == 0 {
		t.Fatal("expected non-empty models for built-in channel claude")
	}
}

func TestGetStaticModelDefinitions_RejectsUnknownChannel(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, nil)
	rec, body := invokeGetStaticModelDefinitions(t, h, "nonexistent-provider")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if body.Error != "unknown channel" || body.Channel != "nonexistent-provider" {
		t.Fatalf("body = %#v, want unknown channel error for nonexistent-provider", body)
	}
}

func TestRefreshModelDefinitions_UpdatesCatalogAndReportsChangedProviders(t *testing.T) {
	modelsDir := filepath.Join("..", "..", "..", "registry", "models")
	baseModels, err := os.ReadFile(filepath.Join(modelsDir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseCodex, err := os.ReadFile(filepath.Join(modelsDir, "codex_client_models.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseDevin, err := os.ReadFile(filepath.Join(modelsDir, "devin_models.json"))
	if err != nil {
		t.Fatal(err)
	}

	var rawCatalog map[string]json.RawMessage
	if err := json.Unmarshal(baseModels, &rawCatalog); err != nil {
		t.Fatal(err)
	}
	var claudeModels []registry.ModelInfo
	if err := json.Unmarshal(rawCatalog["claude"], &claudeModels); err != nil {
		t.Fatal(err)
	}
	claudeModels = append(claudeModels, registry.ModelInfo{
		ID:          "claude-mgmt-refresh-7039",
		DisplayName: "Claude Mgmt Refresh 7039",
	})
	updatedClaude, err := json.Marshal(claudeModels)
	if err != nil {
		t.Fatal(err)
	}
	rawCatalog["claude"] = updatedClaude
	updatedModelsJSON, err := json.Marshal(rawCatalog)
	if err != nil {
		t.Fatal(err)
	}

	var generalPayload atomic.Value
	generalPayload.Store(updatedModelsJSON)
	generalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(generalPayload.Load().([]byte))
	}))
	t.Cleanup(generalSrv.Close)

	codexSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(baseCodex)
	}))
	t.Cleanup(codexSrv.Close)

	devinSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(baseDevin)
	}))
	t.Cleanup(devinSrv.Close)

	cfg := &config.Config{
		AuthDir: t.TempDir(),
		Models: config.ModelCatalogs{
			Catalog:      generalSrv.URL,
			CodexCatalog: codexSrv.URL,
			DevinCatalog: devinSrv.URL,
		},
	}
	h := NewHandlerWithoutConfigFilePath(cfg, nil)
	t.Cleanup(func() {
		generalPayload.Store(baseModels)
		if _, errRestore := registry.RefreshModelCatalogsWithPolicy(context.Background(), cfg.Models); errRestore != nil {
			t.Errorf("restore model catalogs failed: %v", errRestore)
		}
	})

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v8/management/routing/model-definitions/refresh", nil)
	h.RefreshModelDefinitions(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		OK               bool     `json:"ok"`
		Catalog          string   `json:"catalog"`
		CodexCatalog     string   `json:"codex_catalog"`
		DevinCatalog     string   `json:"devin_catalog"`
		ChangedProviders []string `json:"changed_providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", rec.Body.String(), err)
	}
	if !resp.OK || resp.Catalog != generalSrv.URL || resp.CodexCatalog != codexSrv.URL || resp.DevinCatalog != devinSrv.URL {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if !slices.Contains(resp.ChangedProviders, "claude") {
		t.Fatalf("changed_providers = %v, want claude", resp.ChangedProviders)
	}

	_, defs := invokeGetStaticModelDefinitions(t, h, "claude")
	found := false
	for _, m := range defs.Models {
		if m.ID == "claude-mgmt-refresh-7039" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected claude-mgmt-refresh-7039 in GetStaticModelDefinitions after refresh")
	}

	// Invalid remote payload must return 502 Bad Gateway and keep last valid catalog.
	generalPayload.Store([]byte("invalid-json"))
	recFail := httptest.NewRecorder()
	ctxFail, _ := gin.CreateTestContext(recFail)
	ctxFail.Request = httptest.NewRequest(http.MethodPost, "/v8/management/routing/model-definitions/refresh", nil)
	h.RefreshModelDefinitions(ctxFail)
	if recFail.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body = %s", recFail.Code, http.StatusBadGateway, recFail.Body.String())
	}
}
