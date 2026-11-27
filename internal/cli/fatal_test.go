package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/setup"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// A config or lock that can't be read as a whole fails every command that
// reads it, with the file and line, before it does anything: not a config
// without servers, sources or bindings.
func TestCommandsReportFatalErrors(t *testing.T) {
	binary = func() (string, []string, error) { return "/opt/homebrew/bin/ctxtra", nil, nil }
	t.Cleanup(func() { binary = setup.Binary })
	for _, c := range []struct{ name, conf, lock, want string }{
		{"config syntax", "schema: ctxtra/config/v1\nmcp-servers: [\n", "", "config.yaml: line 3"},
		{"config schema", "schema: ctxtra/other\n", "", "config.yaml: line 1"},
		{"lock syntax", "schema: ctxtra/config/v1\nsources: {team: {repo: 'https://github.com/acme/team.git', branch: main}}\n", "schema: ctxtra/lock/v1\nsources: [\n", "ctxtra.lock: line 3"},
		{"lock syntax, no repo source", "schema: ctxtra/config/v1\n", "schema: ctxtra/lock/v1\nsources: [\n", "ctxtra.lock: line 3"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := testenv.Home(t)
			file := testenv.Config(t, h, c.conf)
			if c.lock != "" {
				testenv.Write(t, filepath.Join(h, ".config", "ctxtra", "ctxtra.lock"), c.lock)
			}
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(http.StatusNotFound)
			}))
			defer srv.Close()
			testenv.Write(t, filepath.Join(h, "notes.md"), "notes")
			for _, args := range [][]string{{"mcp", "list"}, {"mcp", "status", h}, {"mcp", "show", "x"}, {"source", "list"},
				{"source", "show", "team"}, {"binding", "list"}, {"check-updates"}, {"setup", "--agent", "codex", "--dry-run"},
				{"mcp", "add", "web", srv.URL + "/mcp"}, {"mcp", "add", "web", srv.URL + "/mcp", "--dry-run"}, {"mcp", "remove", "web"},
				{"skill", "install", "ctxtra"}, {"skill", "list"}, {"binding", "add", "b", "--instruction", "~/notes.md"},
				{"binding", "remove", "b", "--dry-run"}, {"binding", "remove", "b"}, {"source", "add", "--dir", h},
				{"source", "update"}, {"source", "download"}, {"source", "remove", "team"}, {"mcp", "login", "web"}, {"mcp", "status"}} {
				code, out, errOut := run(args...)
				if code != 1 || !strings.Contains(errOut, c.want) || strings.Contains(out, "no MCP servers") || strings.Contains(out, "would") {
					t.Errorf("%v: %d %q\n%s", args, code, errOut, out)
				}
			}
			if requests != 0 {
				t.Errorf("%d requests to the MCP server", requests)
			}
			if _, err := os.Stat(paths.SkillDir("ctxtra")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a skill was installed: %v", err)
			}
			if b, _ := os.ReadFile(file); string(b) != c.conf {
				t.Errorf("the config changed:\n%s", b)
			}
			for _, args := range [][]string{{"mcp", "list", "--json"}, {"source", "list", "--json"}, {"source", "show", "team", "--json"},
				{"binding", "list", "--json"}, {"skill", "list", "--json"}} {
				code, out, _ := run(args...)
				var doc struct {
					Diagnostics []struct{ Severity, Message string }
				}
				if err := json.Unmarshal([]byte(out), &doc); err != nil || code != 1 || len(doc.Diagnostics) != 1 ||
					doc.Diagnostics[0].Severity != "error" || !strings.Contains(doc.Diagnostics[0].Message, c.want) {
					t.Errorf("%v: %d %v %s", args, code, err, out)
				}
			}
		})
	}
}

// skill install doesn't count a broken binding as one that adds the skill:
// it refuses before it writes anything.
func TestSkillInstallBrokenBinding(t *testing.T) {
	for name, bindings := range map[string]string{
		"an unknown key": "  - {name: ctxtra-system, typo: true, skills: [~/.local/share/ctxtra/skills/ctxtra]}\n",
		"a name twice": "  - {name: ctxtra-system, skills: [~/.local/share/ctxtra/skills/ctxtra]}\n" +
			"  - {name: ctxtra-system, skills: [~/.local/share/ctxtra/skills/ctxtra]}\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := testenv.Home(t)
			conf := "schema: ctxtra/config/v1\nbindings:\n" + bindings
			file := testenv.Config(t, h, conf)
			code, out, errOut := run("skill", "install", "ctxtra")
			if code != 1 || !strings.Contains(errOut, "the binding ctxtra-system has errors") || strings.Contains(out, "adds the skill") {
				t.Errorf("exit %d\n%s%s", code, out, errOut)
			}
			if _, err := os.Stat(paths.SkillDir("ctxtra")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the skill was written: %v", err)
			}
			if b, _ := os.ReadFile(file); string(b) != conf {
				t.Errorf("the config changed:\n%s", b)
			}
		})
	}
}
