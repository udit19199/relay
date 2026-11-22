package misc

import "strings"

// MetadataString returns the trimmed string or []byte value stored under key,
// or "" when the key is missing or holds another type.
func MetadataString(metadata map[string]any, key string) string {
	switch v := metadata[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		return ""
	}
}
