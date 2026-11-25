package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/setup"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMuseHooksWithoutTheEnvironment(t *testing.T) {
	testAgentFolders(t, "muse")
}

func TestAMovedConfigFolderInCodex(t *testing.T) {
	testAgentFolders(t, "codex")
}

func testAgentFolders(t *testing.T, name string) {
	t.Helper()
	h := testenv.Home(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range paths.XDGVars {
		t.Setenv(key, filepath.Join(h, "custom", key))
	}
	testenv.Write(t, paths.ConfigPath(), "schema: ctxtra/v1\ncheck-updates: false\nmcp-servers:\n  saved: {command: ['"+exe+"'], env: {CTXTRA_TEST_MCPSERVER: '1'}}\nbindings:\n  - {name: all, match: {global: true}, instructions: ['~/a.md'], mcp-servers: [saved]}\n")
	testenv.Write(t, filepath.Join(h, "a.md"), "CUSTOM_CONFIG_MARKER\n")
	a, _ := registry.Get(name)
	r, err := setup.Run(setup.Options{Agents: []string{name}, Options: agent.Options{Binary: exe}, MCP: setup.MCPConfig{}})
	if err != nil || len(r.Errors) > 0 {
		t.Fatalf("setup: %+v %v", r, err)
	}
	b, err := os.ReadFile(a.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string }
		}
		Servers map[string]struct {
			Command string
			Args    []string
			Env     map[string]string
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	baseEnv := []string{"HOME=" + h, "PATH=" + os.Getenv("PATH"), "CTXTRA_TEST_HOOK=1"}
	cmd := exec.Command("/bin/sh", "-c", data.Hooks["SessionStart"][0].Hooks[0].Command)
	cmd.Dir, cmd.Env = h, baseEnv
	if name == "codex" {
		for _, key := range paths.XDGVars {
			cmd.Env = append(cmd.Env, key+"="+os.Getenv(key))
		}
	}
	cmd.Stdin = strings.NewReader(`{"session_id":"cleared-environment"}`)
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "CUSTOM_CONFIG_MARKER") {
		t.Fatalf("hook: %v %s", err, out)
	}
	entry := data.Servers[agent.MCPEntryName]
	if name == "codex" {
		var cfg struct {
			Servers map[string]struct {
				Command string
				Args    []string
				Vars    []string `toml:"env_vars"`
			} `toml:"mcp_servers"`
		}
		if _, err := toml.DecodeFile(a.(agent.MCPAgent).MCP().MCPPath(), &cfg); err != nil {
			t.Fatal(err)
		}
		proxy := cfg.Servers[agent.MCPEntryName]
		entry.Command, entry.Args, entry.Env = proxy.Command, proxy.Args, map[string]string{}
		for _, key := range paths.XDGVars {
			if !slices.Contains(proxy.Vars, key) {
				t.Fatalf("proxy does not pass %s", key)
			}
			entry.Env[key] = os.Getenv(key)
		}
	}
	cmd = exec.Command(entry.Command, entry.Args...)
	cmd.Dir, cmd.Env = h, append([]string{}, baseEnv...)
	for k, v := range entry.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("proxy: %+v %v", res, err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if !strings.HasPrefix(tool.Name, "saved__") {
			t.Errorf("tool from another config: %s", tool.Name)
		}
	}
	if !slices.Contains(names, "saved__echo") {
		t.Fatalf("proxy tools: %v", names)
	}
}
