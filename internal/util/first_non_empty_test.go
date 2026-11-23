package util

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		values      []string
		want        string
		wantTrimmed string
	}{
		{nil, "", ""},
		{[]string{"", "  ", "\t"}, "", ""},
		{[]string{" ", " a ", "b"}, " a ", "a"},
		{[]string{"a", "b"}, "a", "a"},
	}
	for _, tc := range cases {
		if got := FirstNonEmpty(tc.values...); got != tc.want {
			t.Errorf("FirstNonEmpty(%q) = %q, want %q", tc.values, got, tc.want)
		}
		if got := FirstNonEmptyTrimmed(tc.values...); got != tc.wantTrimmed {
			t.Errorf("FirstNonEmptyTrimmed(%q) = %q, want %q", tc.values, got, tc.wantTrimmed)
		}
	}
}
