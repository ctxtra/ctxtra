package fixture

import (
	"os"
	"strings"
	"testing"
)

func TestConfigDisablesUpdateChecks(t *testing.T) {
	f := New(t, "", "")
	f.Config("schema: ctxtra/v1\nbindings: []\n")
	b, err := os.ReadFile(f.Path(".config/ctxtra/config.yaml"))
	if err != nil || !strings.Contains(string(b), "check-updates: false\n") {
		t.Fatalf("config permits update checks when an agent clears its environment: %s, %v", b, err)
	}
}

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
