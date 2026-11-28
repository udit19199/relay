package login

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"encoding/json"

	"github.com/udit19199/relay/internal/auth/codex"
	coreauth "github.com/udit19199/relay/internal/service/auth"
)

func TestExtractAccessToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]any
		expected string
	}{
		{
			"antigravity top-level access_token",
			map[string]any{"access_token": "tok-abc"},
			"tok-abc",
		},
		{
			"gemini nested token.access_token",
			map[string]any{
				"token": map[string]any{"access_token": "tok-nested"},
			},
			"tok-nested",
		},
		{
			"top-level takes precedence over nested",
			map[string]any{
				"access_token": "tok-top",
				"token":        map[string]any{"access_token": "tok-nested"},
			},
			"tok-top",
		},
		{
			"empty metadata",
			map[string]any{},
			"",
		},
		{
			"whitespace-only access_token",
			map[string]any{"access_token": "   "},
			"",
		},
		{
			"wrong type access_token",
			map[string]any{"access_token": 12345},
			"",
		},
		{
			"token is not a map",
			map[string]any{"token": "not-a-map"},
			"",
		},
		{
			"nested whitespace-only",
			map[string]any{
				"token": map[string]any{"access_token": "  "},
			},
			"",
		},
		{
			"fallback to nested when top-level empty",
			map[string]any{
				"access_token": "",
				"token":        map[string]any{"access_token": "tok-fallback"},
			},
			"tok-fallback",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractAccessToken(tt.metadata)
			if got != tt.expected {
				t.Errorf("extractAccessToken() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFileTokenStoreSaveExistingMetadataSetsFileAttributes(t *testing.T) {
	tests := []struct {
		name          string
		existingToken string
		savedToken    string
	}{
		{name: "unchanged content", existingToken: "token", savedToken: "token"},
		{name: "overwritten content", existingToken: "old-token", savedToken: "new-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			fileName := "antigravity-user.json"
			path := filepath.Join(baseDir, fileName)
			existing := []byte(`{"type":"antigravity","access_token":"` + tt.existingToken + `","disabled":false}`)
			if errWrite := os.WriteFile(path, existing, 0o600); errWrite != nil {
				t.Fatalf("write existing auth file: %v", errWrite)
			}

			store := NewFileTokenStore()
			store.SetBaseDir(baseDir)
			auth := &coreauth.Auth{
				ID:       fileName,
				FileName: fileName,
				Metadata: map[string]any{
					"type":         "antigravity",
					"access_token": tt.savedToken,
				},
			}

			savedPath, errSave := store.Save(context.Background(), auth)
			if errSave != nil {
				t.Fatalf("Save() error = %v", errSave)
			}
			if savedPath != path {
				t.Fatalf("Save() path = %q, want %q", savedPath, path)
			}
			if got := auth.Attributes[coreauth.AttributePath]; got != path {
				t.Errorf("path attribute = %q, want %q", got, path)
			}
			if got := auth.Attributes[coreauth.AttributeSource]; got != path {
				t.Errorf("source attribute = %q, want %q", got, path)
			}
			if got := auth.Attributes[coreauth.AttributeSourceBackend]; got != coreauth.AuthSourceFile {
				t.Errorf("source backend attribute = %q, want %q", got, coreauth.AuthSourceFile)
			}
			persisted, errRead := os.ReadFile(path)
			if errRead != nil {
				t.Fatalf("read saved auth file: %v", errRead)
			}
			expected := []byte(`{"type":"antigravity","access_token":"` + tt.savedToken + `","disabled":false}`)
			if !jsonEqual(persisted, expected) {
				t.Errorf("saved auth file = %s, want JSON equal to %s", persisted, expected)
			}
		})
	}
}

func TestFileTokenStoreNormalizesLegacyCredentialMetadata(t *testing.T) {
	t.Run("save", func(t *testing.T) {
		baseDir := t.TempDir()
		store := NewFileTokenStore()
		store.SetBaseDir(baseDir)
		auth := &coreauth.Auth{
			ID:       "legacy-save.json",
			FileName: "legacy-save.json",
			Metadata: map[string]any{
				"type":            "codex",
				"request-retry":   2,
				"request_retry":   0,
				"disable-cooling": true,
			},
		}

		path, errSave := store.Save(context.Background(), auth)
		if errSave != nil {
			t.Fatalf("Save() error = %v", errSave)
		}
		persisted, errRead := os.ReadFile(path)
		if errRead != nil {
			t.Fatalf("read saved auth file: %v", errRead)
		}
		want := []byte(`{"type":"codex","request_retry":0,"disable_cooling":true,"disabled":false}`)
		if !jsonEqual(persisted, want) {
			t.Fatalf("saved auth file = %s, want JSON equal to %s", persisted, want)
		}
	})

	t.Run("list", func(t *testing.T) {
		baseDir := t.TempDir()
		path := filepath.Join(baseDir, "legacy-list.json")
		if errWrite := os.WriteFile(path, []byte(`{"type":"codex","request-retry":2,"disable-cooling":true}`), 0o600); errWrite != nil {
			t.Fatalf("write legacy auth file: %v", errWrite)
		}
		store := NewFileTokenStore()
		store.SetBaseDir(baseDir)

		auths, errList := store.List(context.Background())
		if errList != nil {
			t.Fatalf("List() error = %v", errList)
		}
		if len(auths) != 1 {
			t.Fatalf("List() len = %d, want 1", len(auths))
		}
		if got := auths[0].Metadata["request_retry"]; got != float64(2) {
			t.Fatalf("listed request_retry = %#v, want 2", got)
		}
		if got := auths[0].Metadata["disable_cooling"]; got != true {
			t.Fatalf("listed disable_cooling = %#v, want true", got)
		}
		for _, legacy := range []string{"request-retry", "disable-cooling"} {
			if _, exists := auths[0].Metadata[legacy]; exists {
				t.Fatalf("listed metadata retained %q: %#v", legacy, auths[0].Metadata)
			}
		}
	})
}

func TestFileTokenStoreSaveRejectsInvalidWeight(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)
	auth := &coreauth.Auth{
		ID:       "invalid.json",
		FileName: "invalid.json",
		Metadata: map[string]any{
			"type":                   "test",
			coreauth.AttributeWeight: 1.5,
		},
	}

	if _, errSave := store.Save(context.Background(), auth); errSave == nil {
		t.Fatal("Save() accepted an invalid weight")
	}
	if _, errStat := os.Stat(filepath.Join(baseDir, auth.FileName)); !os.IsNotExist(errStat) {
		t.Fatalf("invalid auth file was persisted: %v", errStat)
	}
}

func TestFileTokenStoreSaveReadModifyWritePreservesSIWC(t *testing.T) {
	baseDir := t.TempDir()
	fileName := "codex-siwc.json"
	path := filepath.Join(baseDir, fileName)
	onDisk := []byte(`{"type":"codex","access_token":"outer-disk","refresh_token":"outer-ref-disk","use_siwc":true,"siwc":{"access_token":"siwc-disk","refresh_token":"siwc-ref-disk","client_id":"oaiapp_disk","device_id":"e61bbe28-07ef-466d-8e5d-a344f94ab305"}}`)
	if errWrite := os.WriteFile(path, onDisk, 0o600); errWrite != nil {
		t.Fatalf("write initial file: %v", errWrite)
	}

	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)

	// Save an Auth whose in-memory Metadata only updated outer tokens and forgot siwc/use_siwc
	authOuter := &coreauth.Auth{
		ID:       fileName,
		FileName: fileName,
		Provider: "codex",
		Metadata: map[string]any{
			"type":          "codex",
			"access_token":  "outer-new",
			"refresh_token": "outer-ref-new",
		},
	}
	var original map[string]any
	if errUnmarshal := json.Unmarshal(onDisk, &original); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	authOuter.CodexRefreshPatch = codex.NewCredentialRefreshPatch(original, authOuter.Metadata, true, false)
	if _, errSave := store.Save(context.Background(), authOuter); errSave != nil {
		t.Fatalf("Save outer error: %v", errSave)
	}
	if authOuter.Metadata["use_siwc"] != true {
		t.Fatalf("in-memory Metadata should be synced with disk use_siwc, got %v", authOuter.Metadata["use_siwc"])
	}
	siwcMem, ok := authOuter.Metadata["siwc"].(map[string]any)
	if !ok || siwcMem["access_token"] != "siwc-disk" || siwcMem["client_id"] != "oaiapp_disk" {
		t.Fatalf("in-memory Metadata should be synced with disk siwc, got %#v", authOuter.Metadata["siwc"])
	}

	// Now save an Auth that updates only siwc.access_token and siwc.refresh_token
	authSIWC := &coreauth.Auth{
		ID:       fileName,
		FileName: fileName,
		Provider: "codex",
		Metadata: map[string]any{
			"type": "codex",
			"siwc": map[string]any{
				"access_token":  "siwc-new",
				"refresh_token": "siwc-ref-new",
			},
		},
	}
	authSIWC.CodexRefreshPatch = codex.NewCredentialRefreshPatch(authOuter.Metadata, authSIWC.Metadata, false, true)
	if _, errSave := store.Save(context.Background(), authSIWC); errSave != nil {
		t.Fatalf("Save siwc error: %v", errSave)
	}
	if authSIWC.Metadata["access_token"] != "outer-new" || authSIWC.Metadata["use_siwc"] != true {
		t.Fatalf("outer or use_siwc lost on disk/memory: %#v", authSIWC.Metadata)
	}
	siwcFinal, ok := authSIWC.Metadata["siwc"].(map[string]any)
	if !ok || siwcFinal["access_token"] != "siwc-new" || siwcFinal["client_id"] != "oaiapp_disk" || siwcFinal["device_id"] != "e61bbe28-07ef-466d-8e5d-a344f94ab305" {
		t.Fatalf("siwcFinal unexpected: %#v", siwcFinal)
	}
}

func TestFileTokenStoreSaveAllowsDeletingNonCredentialMetadata(t *testing.T) {
	baseDir := t.TempDir()
	fileName := "codex-proxy.json"
	path := filepath.Join(baseDir, fileName)
	onDisk := []byte(`{"type":"codex","access_token":"outer-1","proxy_url":"http://old-proxy:8080","use_siwc":true,"siwc":{"access_token":"siwc-1","refresh_token":"siwc-ref-1","client_id":"oaiapp_1"}}`)
	if errWrite := os.WriteFile(path, onDisk, 0o600); errWrite != nil {
		t.Fatalf("write initial file: %v", errWrite)
	}

	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)

	// Operator clears proxy_url from Metadata and saves
	auth := &coreauth.Auth{
		ID:       fileName,
		FileName: fileName,
		Provider: "codex",
		Metadata: map[string]any{
			"type":         "codex",
			"access_token": "outer-1",
			"use_siwc":     true,
			"siwc": map[string]any{
				"access_token":  "siwc-1",
				"refresh_token": "siwc-ref-1",
				"client_id":     "oaiapp_1",
			},
		},
	}
	if _, errSave := store.Save(context.Background(), auth); errSave != nil {
		t.Fatalf("Save error: %v", errSave)
	}
	if _, exists := auth.Metadata["proxy_url"]; exists {
		t.Fatalf("deleted proxy_url was resurrected in memory: %v", auth.Metadata["proxy_url"])
	}
}
