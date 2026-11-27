package setup

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestReplacesEarlierSetups(t *testing.T) {
	for _, a := range registry.All() {
		t.Run(a.Name(), func(t *testing.T) {
			h := testenv.Home(t)
			t.Setenv("PATH", t.TempDir())
			bin := filepath.Join(h, "bin", "ctxtra")
			testenv.Write(t, bin, "#!/bin/sh\n")
			if err := os.Chmod(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			o := Options{Agents: []string{a.Name()}, Options: agent.Options{Binary: bin}}
			r, err := Run(o)
			if err != nil || len(r.Errors) > 0 {
				t.Fatalf("setup: %v, %v", err, r.Errors)
			}
			for _, path := range r.Changes {
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				old := regexp.MustCompile(`,\s*"--setup-version=1"`).ReplaceAllString(string(b), "")
				old = strings.NewReplacer(
					"claude-code", "claude", "github-copilot", "copilot", "hermes-agent", "hermes",
					" "+agent.SetupVersionArg, "").Replace(old)
				testenv.Write(t, path, old)
			}
			notices := a.Check(bin, false)
			if !strings.Contains(strings.Join(notices, "\n"), "earlier ctxtra") ||
				!strings.Contains(strings.Join(notices, "\n"), "ctxtra setup --agent "+a.Name()) {
				t.Fatalf("earlier setup not diagnosed: %v", notices)
			}
			if m, ok := a.(agent.MCPAgent); ok {
				n := m.MCP().CheckMCP(bin, agent.MCPCheck{})
				if !strings.Contains(strings.Join(n, "\n"), "earlier ctxtra") || !strings.Contains(strings.Join(n, "\n"), "ctxtra setup --agent "+a.Name()) {
					t.Fatalf("earlier proxy not diagnosed: %v", n)
				}
			}
			for range 2 {
				r, err = Run(o)
				if err != nil || len(r.Errors) > 0 {
					t.Fatalf("replace: %v, %v", err, r.Errors)
				}
				if n := strings.Join(a.Check(bin, false), "\n"); strings.Contains(n, "earlier ctxtra") {
					t.Fatalf("earlier entry remains: %s", n)
				}
			}
			if len(r.Changes) > 0 {
				t.Fatalf("repeated setup changed %v", r.Changes)
			}
		})
	}
}

func TestSetsUpProxyWithoutServers(t *testing.T) {
	for _, a := range registry.All() {
		m, ok := a.(agent.MCPAgent)
		if !ok {
			continue
		}
		t.Run(a.Name(), func(t *testing.T) {
			h := testenv.Home(t)
			t.Setenv("PATH", t.TempDir())
			bin := filepath.Join(h, "bin", "ctxtra")
			r, err := Run(Options{Agents: []string{a.Name()}, Options: agent.Options{Binary: bin}})
			if err != nil || len(r.Errors) > 0 {
				t.Fatalf("setup: %v, %v", err, r.Errors)
			}
			cmd, ok := m.MCP().MCPCommand()
			if !ok || strings.Join(cmd, " ") != bin+" "+strings.Join(agent.MCPArgs(a.Name()), " ") {
				t.Fatalf("proxy: %v, %v", cmd, ok)
			}
		})
	}
}
