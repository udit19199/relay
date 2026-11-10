package cliproxy

import (
	"flag"
	"os"
	"testing"
)

// TestMain skips upstream Antigravity model-registration tests. Relay never
// registers Antigravity (see forkDisabledProviders), so those tests wait for a
// registration that cannot happen and hang or fail.
func TestMain(m *testing.M) {
	flag.Parse()
	if flag.Lookup("test.skip").Value.String() == "" {
		_ = flag.Set("test.skip", "^TestAntigravity")
	}
	os.Exit(m.Run())
}
