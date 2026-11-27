package config

import (
	"strings"
	"testing"
)

// Server names: 1 to MaxServerName characters of a-z, 0-9 and _, a letter
// first, no __ and no _ at the end; a refused name comes with a valid one
// made from it.
func TestServerNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		ok   bool
		hint string
	}{
		{"linear_work", true, ""},
		{"a", true, ""},
		{"gh2", true, ""},
		{strings.Repeat("a", 20), true, ""},
		{strings.Repeat("a", 21), false, "for example aaaaaaaaaaaaaaaaaaaa"},
		{"linear-work", false, "for example linear_work"},
		{"Linear", false, "for example linear"},
		{"2fa", false, "for example fa"},
		{"a__b", false, "for example a_b"},
		{"a_", false, "for example a"},
		{"_a", false, "for example a"},
		{"", false, ""},
	} {
		err := CheckServerName(tc.name)
		if (err == nil) != tc.ok {
			t.Errorf("%q: %v", tc.name, err)
		}
		if err != nil && !strings.Contains(err.Error(), "a-z, 0-9 and _") || tc.hint != "" && !strings.Contains(err.Error(), tc.hint) {
			t.Errorf("%q: %v, want hint %q", tc.name, err, tc.hint)
		}
	}
}
