//go:build unix

package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	sessionstatus "github.com/ctxtra/ctxtra/internal/mcp/status"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

// mcp add and mcp show stop a server with every process it started.
func TestServerToolsStopsDescendants(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "child.pid")
	s := upstream.Server{ID: "a", Name: "a", Command: []string{exe, "-child", pidfile}, Env: map[string]string{"CTXTRA_TEST_MCPSERVER": "1"}}
	tools, err := serverTools(s, dir, false)
	if err != nil || len(tools) == 0 {
		t.Fatalf("%v %v", tools, err)
	}
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(b))
	for end := time.Now().Add(4 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("the server's child %d still runs", pid)
}

// When the agent stops reading the proxy's output, the proxy doesn't die
// of SIGPIPE before it stopped its servers.
func TestProxyBrokenStdout(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := t.TempDir()
	pidfile := filepath.Join(h, "a.pid")
	cfg := filepath.Join(h, "config.yaml")
	if err := os.WriteFile(cfg, []byte("schema: ctxtra/v1\nmcp-servers:\n  a: {command: ['"+exe+"', -pidfile, '"+pidfile+"', -ignore-term], env: {CTXTRA_TEST_MCPSERVER: '1'}}\n"+
		"bindings:\n  - {name: all, match: {global: true}, mcp-servers: [a]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "mcp", "proxy", "test")
	cmd.Dir = h
	cmd.Env = []string{"HOME=" + h, "PATH=" + os.Getenv("PATH"), "CTXTRA_TEST_HOOK=1", "CTXTRA_EXPERIMENTAL_MCP=1",
		"CTXTRA_CONFIG=" + cfg, "CTXTRA_LOG=" + filepath.Join(h, "log.jsonl")}
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	} {
		_, _ = in.Write([]byte(line + "\n"))
	}
	dec := json.NewDecoder(out)
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		if m["id"] == float64(2) {
			break
		}
	}
	b, _ := os.ReadFile(pidfile)
	pid, _ := strconv.Atoi(string(b))
	_ = out.Close() // the agent stops reading
	_, _ = in.Write([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"a__big","arguments":{}}}` + "\n"))
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the proxy didn't exit")
	}
	for end := time.Now().Add(4 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("the server %d still runs", pid)
}

// startBudget is the most that ctxtra's own start may take in a proxy,
// from the process's start until it found the folder's servers: the
// agent's session waits for it.
const startBudget = 300 * time.Millisecond

// The proxy's own start stays within startBudget, and with a fast server
// the first tool list comes within a second. The best of three runs
// counts: a busy test machine can slow down any one of them.
func TestProxyStartBudget(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := t.TempDir()
	t.Setenv("HOME", h)
	dir := filepath.Join(h, "dev", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Skipf("git: %v %s", err, out)
	}
	cfg := filepath.Join(h, "config.yaml")
	if err := os.WriteFile(cfg, []byte("schema: ctxtra/v1\nmcp-servers:\n  a: {command: ['"+exe+"'], env: {CTXTRA_TEST_MCPSERVER: '1'}}\n"+
		"bindings:\n  - {name: app, match: {dirs: ['~/dev/**']}, mcp-servers: [a]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	best := struct{ init, list, resolve time.Duration }{time.Hour, time.Hour, time.Hour}
	for range 3 {
		initialize, list := proxyStart(t, exe, dir, cfg)
		rec := waitRecord(t, dir)
		best.init, best.list = min(best.init, initialize), min(best.list, list)
		best.resolve = min(best.resolve, time.Duration(rec.ResolveMs)*time.Millisecond)
	}
	t.Logf("best of 3: initialize after %v, first tool list after %v, servers found after %v", best.init, best.list, best.resolve)
	// The answer to initialize waits for the folder's servers.
	if best.init > startBudget || best.resolve > startBudget {
		t.Errorf("ctxtra's own start took %v (initialize answered after %v), over %v", best.resolve, best.init, startBudget)
	}
	if best.list > time.Second {
		t.Errorf("the first tool list took %v", best.list)
	}
}

// proxyStart starts a proxy in dir and times, from the process's start,
// its answers to initialize and to the first tool list.
func proxyStart(t *testing.T, exe, dir, cfg string) (initialize, list time.Duration) {
	t.Helper()
	cmd := exec.Command(exe, "mcp", "proxy", "claude")
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "CTXTRA_TEST_HOOK=1", "CTXTRA_EXPERIMENTAL_MCP=1",
		"CTXTRA_CONFIG=" + cfg, "CTXTRA_LOG=" + filepath.Join(os.Getenv("HOME"), "log.jsonl")}
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	began := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = in.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("proxy: %v", err)
		}
	}()
	dec := json.NewDecoder(out)
	answer := func(id float64) time.Duration {
		for {
			var m map[string]any
			if err := dec.Decode(&m); err != nil {
				t.Fatal(err)
			}
			if m["id"] == id {
				if r, _ := m["result"].(map[string]any); id == 2 && len(r["tools"].([]any)) == 0 {
					t.Fatalf("no tools: %v", m)
				}
				return time.Since(began)
			}
		}
	}
	_, _ = in.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}` + "\n"))
	initialize = answer(1)
	_, _ = in.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"))
	return initialize, answer(2)
}

// waitRecord is the status record of dir's last session, which the proxy
// writes once the first tool list is ready.
func waitRecord(t *testing.T, dir string) sessionstatus.Record {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if rec, ok, _ := sessionstatus.Read(dir); ok && rec.ListMs > 0 {
			return rec
		}
	}
	t.Fatal("no status record")
	return sessionstatus.Record{}
}
