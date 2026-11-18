package state

import (
	"path/filepath"
	"testing"

	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestSessionConfig(t *testing.T) {
	h := testenv.Home(t)
	def := paths.ConfigPath()
	if got := SessionConfig("muse"); got != def {
		t.Fatalf("missing record: %s", got)
	}
	var s Setups
	ap := filepath.Join(h, ".config/muse/settings.json")
	saved := filepath.Join(h, "work/ctxtra.yaml")
	s.Install("muse", ap)
	s.SetConfig("muse", ap, saved)
	if err := WriteSetups(s); err != nil {
		t.Fatal(err)
	}
	if got := SessionConfig("muse"); got != saved {
		t.Fatalf("recorded: %s", got)
	}
	if got := SessionConfig("claude"); got != def {
		t.Fatalf("another agent: %s", got)
	}
	// A missing saved file must not switch the session to the default account.
	t.Setenv("CTXTRA_CONFIG", filepath.Join(h, "override.yaml"))
	if got := SessionConfig("muse"); got != paths.ConfigPath() {
		t.Fatalf("override: %s", got)
	}
	t.Setenv("CTXTRA_CONFIG", "")
	s.Configs["muse"] = SetupConfig{AgentPath: ap, Path: "relative.yaml"}
	if err := WriteSetups(s); err != nil {
		t.Fatal(err)
	}
	if got := SessionConfig("muse"); got != def {
		t.Fatalf("relative record: %s", got)
	}
	s.SetConfig("muse", ap, saved)
	delete(s.Installed, "muse")
	if err := WriteSetups(s); err != nil {
		t.Fatal(err)
	}
	if got := SessionConfig("muse"); got != def {
		t.Fatalf("orphan record: %s", got)
	}
	testenv.Write(t, SetupsPath(), "{")
	if got := SessionConfig("muse"); got != def {
		t.Fatalf("broken record: %s", got)
	}
}

func TestUninstallConfig(t *testing.T) {
	testenv.Home(t)
	var s Setups
	s.Install("muse", "/a/settings.json")
	s.Install("muse", "/b/settings.json")
	s.SetConfig("muse", "/b/settings.json", "/config.yaml")
	s.Uninstall("muse", "/a/settings.json")
	if _, ok := s.Configs["muse"]; !ok {
		t.Fatal("removed another setup's config")
	}
	s.Uninstall("muse", "/b/settings.json")
	if _, ok := s.Configs["muse"]; ok {
		t.Fatal("kept removed setup's config")
	}
}
