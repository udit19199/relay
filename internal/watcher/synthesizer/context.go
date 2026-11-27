package synthesizer

import (
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// SynthesisContext provides the context needed for auth synthesis.
type SynthesisContext struct {
	// Config is the current configuration
	Config *config.Config
	// AuthDir is the directory containing auth files
	AuthDir string
	// Now is the current time for timestamps
	Now time.Time
	// IDGenerator generates stable IDs for auth entries
	IDGenerator *StableIDGenerator
	// StrictFileParsing surfaces parse errors instead of silently skipping or falling back.
	StrictFileParsing bool
	// IncompleteFiles records existing auth JSON file paths whose contents were
	// empty or not valid JSON during a non-strict directory scan, or file/directory
	// paths whose os.ReadFile or os.ReadDir failed with an error other than fs.ErrNotExist.
	IncompleteFiles map[string]struct{}
}

// RecordIncompleteFile records an existing auth JSON file whose content is not yet complete JSON
// or a file/directory path whose os.ReadFile or os.ReadDir failed with an error other than fs.ErrNotExist.
func (c *SynthesisContext) RecordIncompleteFile(path string) {
	if c == nil || path == "" {
		return
	}
	if c.IncompleteFiles == nil {
		c.IncompleteFiles = make(map[string]struct{})
	}
	c.IncompleteFiles[path] = struct{}{}
}
