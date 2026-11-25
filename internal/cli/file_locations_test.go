package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/setup"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestDoctor(t *testing.T) {
	h := testenv.Home(t)
	for _, key := range paths.XDGVars {
		t.Setenv(key, filepath.Join(h, key))
	}
	n, out, errOut := run("doctor", h)
	if n != 0 {
		t.Fatalf("doctor: %d %s %s", n, out, errOut)
	}
	for _, p := range []string{paths.ConfigPath(), paths.ConfigDir(), paths.DataDir(), paths.StateDir(), paths.CacheDir()} {
		if !strings.Contains(out, p) {
			t.Errorf("missing %s: %s", p, out)
		}
	}
	n, out, errOut = run("doctor", "--json", h)
	var doc struct {
		Data struct{ Folders map[string]string }
	}
	if n != 0 || json.Unmarshal([]byte(out), &doc) != nil || len(doc.Data.Folders) != 4 {
		t.Fatalf("doctor JSON: %d %s %s", n, out, errOut)
	}
}

func TestTheFolderMovedAfterSetup(t *testing.T) {
	for _, key := range paths.XDGVars {
		t.Run(key, func(t *testing.T) {
			h := testenv.Home(t)
			t.Setenv(key, filepath.Join(h, "a", "state"))
			r, err := setup.Run(setup.Options{Agents: []string{"muse"}, Options: agent.Options{Binary: "/bin/sh"}, MCP: setup.MCPConfig{Want: true}})
			if err != nil || len(r.Errors) != 0 {
				t.Fatalf("setup: %+v %v", r, err)
			}
			t.Setenv(key, filepath.Join(h, "b", "state"))
			n, out, _ := run("doctor", h)
			if n != 1 {
				t.Fatalf("doctor: %d %s", n, out)
			}
			for _, what := range []string{"the hooks use " + key + "=", "the ctxtra MCP proxy entries use " + key + "=", filepath.Join(h, "a", "state"), filepath.Join(h, "b", "state"), "ctxtra setup --agent muse"} {
				if !strings.Contains(out, what) {
					t.Errorf("missing %s: %s", what, out)
				}
			}
			r, err = setup.Run(setup.Options{Agents: []string{"muse"}, Options: agent.Options{Binary: "/bin/sh"}, MCP: setup.MCPConfig{Want: true}})
			if err != nil || len(r.Errors) != 0 {
				t.Fatalf("repair: %+v %v", r, err)
			}
			if n, out, _ = run("doctor", h); n != 0 {
				t.Fatalf("after repair: %d %s", n, out)
			}
		})
	}
}
