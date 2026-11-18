package muse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPLogPath(t *testing.T) {
	for _, value := range []string{"logs/ctxtra.log", "~/logs/ctxtra.log"} {
		t.Run(value, func(t *testing.T) {
			h := testenv.Home(t)
			t.Chdir(h)
			t.Setenv("CTXTRA_LOG", value)
			a := Agent{}
			var r agent.Report
			if err := a.MCP().SetupMCP(agent.MCPOptions{Want: true, Options: agent.Options{Binary: "/bin/ctxtra"}}, &r); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(a.ConfigPath())
			if err != nil {
				t.Fatal(err)
			}
			var data struct {
				Servers map[string]struct {
					Env map[string]string `json:"env"`
				} `json:"mcpServers"`
			}
			if err := json.Unmarshal(b, &data); err != nil {
				t.Fatal(err)
			}
			if got, want := data.Servers[agent.MCPEntryName].Env["CTXTRA_LOG"], filepath.Join(h, "logs/ctxtra.log"); got != want {
				t.Fatalf("log path %q, want %q", got, want)
			}
		})
	}
}
