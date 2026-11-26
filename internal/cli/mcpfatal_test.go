package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// A config or lock that can't be read as a whole is an error of mcp list
// and mcp status, with the file and line, not a config without servers.
func TestMCPCommandsReportFatalErrors(t *testing.T) {
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
			for _, args := range [][]string{{"mcp", "list"}, {"mcp", "status", h}} {
				code, out, errOut := run(args...)
				if code != 1 || !strings.Contains(errOut, c.want) || strings.Contains(out, "no MCP servers") {
					t.Errorf("%v: %d %q\n%s", args, code, errOut, out)
				}
			}
			code, out, _ := run("mcp", "list", "--json")
			var doc struct {
				Data        []any
				Diagnostics []struct{ Severity, Message string }
			}
			if err := json.Unmarshal([]byte(out), &doc); err != nil || code != 1 || len(doc.Diagnostics) != 1 ||
				doc.Diagnostics[0].Severity != "error" || !strings.Contains(doc.Diagnostics[0].Message, c.want) {
				t.Errorf("json: %d %v %s", code, err, out)
			}
		})
	}
}
