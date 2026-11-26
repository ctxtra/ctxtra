package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

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
	} {
		t.Run(c.name, func(t *testing.T) {
			h := testenv.Home(t)
			testenv.Config(t, h, c.conf)
			if c.lock != "" {
				testenv.Write(t, filepath.Join(h, ".config", "ctxtra", "ctxtra.lock"), c.lock)
			}
			for _, args := range [][]string{{"mcp", "list"}, {"mcp", "status", h}, {"mcp", "show", "x"}, {"source", "list"},
				{"source", "show", "team"}, {"binding", "list"}, {"check-updates"}, {"setup", "--agent", "codex", "--dry-run"}} {
				code, out, errOut := run(args...)
				if code != 1 || !strings.Contains(errOut, c.want) || strings.Contains(out, "no MCP servers") || strings.Contains(out, "would") {
					t.Errorf("%v: %d %q\n%s", args, code, errOut, out)
				}
			}
			for _, args := range [][]string{{"mcp", "list", "--json"}, {"source", "list", "--json"}, {"source", "show", "team", "--json"}, {"binding", "list", "--json"}} {
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
