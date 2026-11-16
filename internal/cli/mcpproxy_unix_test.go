//go:build unix

package cli

import (
	"encoding/json"
	"fmt"
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
		began := time.Now()
		initialize, list, err := proxyStart(exe, dir, cfg)
		if err != nil {
			t.Logf("a run failed: %v", err)
			continue
		}
		rec := waitRecord(t, dir, began)
		best.init, best.list = min(best.init, initialize), min(best.list, list)
		best.resolve = min(best.resolve, time.Duration(rec.ResolveMs)*time.Millisecond)
	}
	if best.list == time.Hour {
		t.Fatal("no run got the server's tools")
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
// its answers to initialize and to the first tool list. The agent is Codex
// for its long deadline: a slow run has the server's tools too.
func proxyStart(exe, dir, cfg string) (initialize, list time.Duration, err error) {
	cmd := exec.Command(exe, "mcp", "proxy", "codex")
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "CTXTRA_TEST_HOOK=1", "CTXTRA_EXPERIMENTAL_MCP=1",
		"CTXTRA_CONFIG=" + cfg, "CTXTRA_LOG=" + filepath.Join(os.Getenv("HOME"), "log.jsonl")}
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	began := time.Now()
	if err := cmd.Start(); err != nil {
		return 0, 0, err
	}
	defer func() {
		_ = in.Close()
		if werr := cmd.Wait(); werr != nil && err == nil {
			err = fmt.Errorf("proxy: %w", werr)
		}
	}()
	dec := json.NewDecoder(out)
	answer := func(id float64) (time.Duration, error) {
		for {
			var m map[string]any
			if err := dec.Decode(&m); err != nil {
				return 0, err
			}
			if m["id"] == id {
				if r, _ := m["result"].(map[string]any); id == 2 && len(r["tools"].([]any)) == 0 {
					return 0, fmt.Errorf("no tools: %v", m)
				}
				return time.Since(began), nil
			}
		}
	}
	_, _ = in.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}` + "\n"))
	if initialize, err = answer(1); err != nil {
		return 0, 0, err
	}
	_, _ = in.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"))
	list, err = answer(2)
	return initialize, list, err
}

// waitRecord is the status record of dir's session started after began,
// once it has the first tool list's answer.
func waitRecord(t *testing.T, dir string, began time.Time) sessionstatus.Record {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if rec, ok, _ := sessionstatus.Read(dir); ok && rec.ListMs > 0 && !rec.Started.Before(began) {
			return rec
		}
	}
	t.Fatal("no status record")
	return sessionstatus.Record{}
}
