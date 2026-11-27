package xai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveTokenToFileUsesOwnerOnlyPermissions(t *testing.T) {
	for _, preexistingWide := range []bool{false, true} {
		t.Run(map[bool]string{false: "new file", true: "pre-existing wide file"}[preexistingWide], func(t *testing.T) {
			authFilePath := filepath.Join(t.TempDir(), "xai-token.json")
			if preexistingWide {
				if errWrite := os.WriteFile(authFilePath, []byte(`{"old":true}`), 0o644); errWrite != nil {
					t.Fatal(errWrite)
				}
			}
			storage := &TokenStorage{AccessToken: "token", RefreshToken: "refresh"}
			if errSave := storage.SaveTokenToFile(authFilePath); errSave != nil {
				t.Fatalf("SaveTokenToFile() error = %v", errSave)
			}
			info, errStat := os.Stat(authFilePath)
			if errStat != nil {
				t.Fatal(errStat)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("mode = %o, want 600", got)
			}
		})
	}
}
