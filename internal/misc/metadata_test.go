package misc

import "testing"

func TestMetadataString(t *testing.T) {
	metadata := map[string]any{
		"string": "  value  ",
		"bytes":  []byte(" raw "),
		"number": 42,
		"nil":    nil,
	}
	tests := []struct {
		metadata map[string]any
		key      string
		want     string
	}{
		{metadata, "string", "value"},
		{metadata, "bytes", "raw"},
		{metadata, "number", ""},
		{metadata, "nil", ""},
		{metadata, "missing", ""},
		{nil, "string", ""},
	}
	for _, tt := range tests {
		if got := MetadataString(tt.metadata, tt.key); got != tt.want {
			t.Errorf("MetadataString(%v, %q) = %q, want %q", tt.metadata, tt.key, got, tt.want)
		}
	}
}
