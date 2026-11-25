package state

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// setup.json holds absolute config paths, once each, and the agents setup
// found; a broken file is an error, a missing one is none.
func TestSetups(t *testing.T) {
	h := testenv.Home(t)
	if _, ok, err := LoadSetups(); ok || err != nil {
		t.Fatalf("missing: %v, %v", ok, err)
	}
	var s Setups
	t.Chdir(h)
	s.Install("hermes-agent", "rel/.hermes/plugins/ctxtra/__init__.py")
	s.Install("hermes-agent", filepath.Join(h, "rel", ".hermes", "plugins", "ctxtra", "..", "ctxtra", "__init__.py"))
	s.Install("claude-code", "/c/settings.json")
	s.Detect("cursor")
	s.Detect("cursor")
	if err := WriteSetups(s); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadSetups()
	if !ok || err != nil || !slices.Equal(got.Installed["hermes-agent"], []string{filepath.Join(h, "rel/.hermes/plugins/ctxtra/__init__.py")}) ||
		!slices.Equal(got.Detected, []string{"cursor"}) {
		t.Fatalf("%+v, %v, %v", got, ok, err)
	}
	if !got.Known("claude-code", "/c/../c/settings.json") || got.Known("claude-code", "/d/settings.json") || got.Known("codex", "/c/settings.json") {
		t.Error("Known")
	}
	if !got.Seen("cursor") || !got.Seen("claude-code") || got.Seen("pi") {
		t.Error("Seen")
	}
	got.Uninstall("claude-code", "/c/settings.json")
	if got.Seen("claude-code") || len(got.Installed) != 1 {
		t.Errorf("uninstall: %+v", got)
	}
	if err := os.WriteFile(SetupsPath(), []byte(`{"detected": "x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LoadSetups(); ok || err == nil {
		t.Errorf("broken: %v, %v", ok, err)
	}
}
