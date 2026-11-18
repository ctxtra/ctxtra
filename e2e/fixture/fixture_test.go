package fixture

import (
	"strings"
	"testing"
)

func TestEnvDisablesUpdateChecks(t *testing.T) {
	f := New(t, "", "")
	var value string
	for _, entry := range f.Env() {
		if v, ok := strings.CutPrefix(entry, "CTXTRA_NO_UPDATE_CHECK="); ok {
			value = v
		}
	}
	if value != "1" {
		t.Fatalf("CTXTRA_NO_UPDATE_CHECK = %q, want 1", value)
	}
}
