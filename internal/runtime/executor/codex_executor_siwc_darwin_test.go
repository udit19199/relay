package executor

import (
	"context"
	"os"
	"testing"

	codexauth "github.com/udit19199/relay/internal/auth/codex"
)

func TestMain(m *testing.M) {
	codexauth.ReadSIWCSystemState = func(context.Context) ([]byte, error) {
		return []byte("Current System Capabilities are: CPU Graphics Audio Network\nCurrent Power State: 4\n"), nil
	}
	os.Exit(m.Run())
}
