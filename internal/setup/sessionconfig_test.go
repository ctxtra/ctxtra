package setup

import (
	"path/filepath"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestSetupRecordsConfig(t *testing.T) {
	h := testenv.Home(t)
	first, second := filepath.Join(h, "first.yaml"), filepath.Join(h, "second.yaml")
	t.Setenv("CTXTRA_CONFIG", first)
	o := Options{Agents: []string{"muse"}, Options: agent.Options{Binary: "/bin/ctxtra"}}
	run := func() {
		t.Helper()
		r, err := Run(o)
		if err != nil || len(r.Errors) > 0 || r.Record != nil {
			t.Fatalf("%+v, %v", r, err)
		}
	}
	run()
	check := func(want string) {
		t.Helper()
		t.Setenv("CTXTRA_CONFIG", "")
		if got := state.SessionConfig("muse"); got != want {
			t.Fatalf("config %s, want %s", got, want)
		}
		t.Setenv("CTXTRA_CONFIG", second)
	}
	check(first)
	o.DryRun = true
	run()
	check(first)
	o.DryRun = false
	// A failed agent setup cannot change the recorded config.
	testenv.Write(t, filepath.Join(h, ".config/muse/settings.json"), "{")
	r, err := Run(o)
	if err != nil || len(r.Errors) != 1 {
		t.Fatalf("failed setup: %+v %v", r, err)
	}
	check(first)
	testenv.Write(t, filepath.Join(h, ".config/muse/settings.json"), "{}")
	run()
	check(second)
	o.Remove = true
	run()
	s, _, err := state.LoadSetups()
	if err != nil || len(s.Configs) != 0 {
		t.Fatalf("remove: %+v %v", s, err)
	}
}
