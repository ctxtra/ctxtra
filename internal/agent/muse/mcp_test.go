package muse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPEnvironment(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_LOG", filepath.Join(h, "custom.log"))
	t.Setenv("CTXTRA_NO_UPDATE_CHECK", "1")
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "private-state"))
	t.Setenv("CTXTRA_TOKEN", "must-not-be-copied")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "must-not-be-copied")
	t.Setenv("SERVER_TOKEN", "must-not-be-copied")
	a := Agent{}
	testenv.Write(t, a.ConfigPath(), `{"mcpServers":{"other":{"command":"other"},"ctxtra-proxy":{"command":"/old/ctxtra","args":["mcp","proxy","muse"],"env":{"USER_KEY":"keep","CTXTRA_LOG":"old"},"user_key":true}}}`)
	o := agent.MCPOptions{Want: true, EnvPass: []string{"SERVER_TOKEN"}, Options: agent.Options{Binary: "/bin/ctxtra"}}
	for i := range 2 {
		var r agent.Report
		if err := a.MCP().SetupMCP(o, &r); err != nil {
			t.Fatal(err)
		}
		if i == 1 && len(r.Changes) != 0 {
			t.Fatalf("not idempotent: %+v", r)
		}
	}
	read := func() map[string]any {
		t.Helper()
		b, err := os.ReadFile(a.ConfigPath())
		if err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		if err := json.Unmarshal(b, &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	data := read()
	if data["schema_version"] != float64(1) {
		t.Fatal("missing schema version")
	}
	servers := data["mcpServers"].(map[string]any)
	entry := servers[agent.MCPEntryName].(map[string]any)
	env := entry["env"].(map[string]any)
	if len(env) != 4 || env["XDG_STATE_HOME"] != os.Getenv("XDG_STATE_HOME") || env["USER_KEY"] != "keep" || env["CTXTRA_LOG"] != os.Getenv("CTXTRA_LOG") || env["CTXTRA_NO_UPDATE_CHECK"] != "1" {
		t.Fatalf("environment: %+v", env)
	}
	if entry["user_key"] != true || entry["mode"] != "optional" {
		t.Fatalf("entry: %+v", entry)
	}
	t.Setenv("CTXTRA_LOG", "")
	var r agent.Report
	if err := a.MCP().SetupMCP(o, &r); err != nil {
		t.Fatal(err)
	}
	entry = read()["mcpServers"].(map[string]any)[agent.MCPEntryName].(map[string]any)
	env = entry["env"].(map[string]any)
	if _, ok := env["CTXTRA_LOG"]; ok {
		t.Fatal("stale setup-owned log value")
	}
	if env["USER_KEY"] != "keep" {
		t.Fatal("user environment lost")
	}
	o.Remove = true
	if err := a.MCP().SetupMCP(o, &r); err != nil {
		t.Fatal(err)
	}
	servers = read()["mcpServers"].(map[string]any)
	if _, ok := servers[agent.MCPEntryName]; ok || servers["other"] == nil {
		t.Fatalf("remove: %+v", servers)
	}
}

func TestMCPInvalidEnv(t *testing.T) {
	testenv.Home(t)
	a := Agent{}
	original := `{"mcpServers":{"ctxtra-proxy":{"command":"/bin/ctxtra","args":["mcp","proxy","muse"],"env":"invalid"}}}`
	testenv.Write(t, a.ConfigPath(), original)
	var r agent.Report
	if err := a.MCP().SetupMCP(agent.MCPOptions{Want: true, Options: agent.Options{Binary: "/bin/ctxtra"}}, &r); err == nil {
		t.Fatal("accepted non-object env")
	}
	b, err := os.ReadFile(a.ConfigPath())
	if err != nil || string(b) != original {
		t.Fatalf("changed invalid file: %s %v", b, err)
	}
}
