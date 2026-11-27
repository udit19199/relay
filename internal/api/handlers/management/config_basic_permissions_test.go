package management

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteConfigUsesOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if errWrite := os.WriteFile(path, []byte("port: 8317\n"), 0o644); errWrite != nil {
		t.Fatal(errWrite)
	}
	if errWrite := WriteConfig(path, []byte("port: 8317\n")); errWrite != nil {
		t.Fatalf("WriteConfig() error = %v", errWrite)
	}
	info, errStat := os.Stat(path)
	if errStat != nil {
		t.Fatal(errStat)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}
