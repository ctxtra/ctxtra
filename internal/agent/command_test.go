package agent_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// message is an agent whose hook answer shows the user a message.
type message struct{}

func TestRecordedXDGQuoting(t *testing.T) {
	h := testenv.Home(t)
	for _, key := range paths.XDGVars {
		t.Setenv(key, filepath.Join(h, "quotes'\"$() `value` \\ and\nnewline", key))
	}
	bin := filepath.Join(h, "ctxtra")
	testenv.Write(t, bin, "#!/bin/sh\nprintf '%s\\000' \"$XDG_CONFIG_HOME\" \"$XDG_DATA_HOME\" \"$XDG_STATE_HOME\" \"$XDG_CACHE_HOME\"\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	command := agent.WithXDG(agent.Command(bin, "muse", "SessionStart", nil))
	hook, ok := agent.ParseCommand(command, nil)
	if !ok {
		t.Fatalf("unrecognized command: %s", command)
	}
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = []string{"HOME=" + h}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command: %v %s", err, out)
	}
	values := strings.Split(string(out), "\x00")
	if len(values) != 5 {
		t.Fatalf("output: %q", out)
	}
	for i, key := range paths.XDGVars {
		if values[i] != os.Getenv(key) || hook.Env[key] != os.Getenv(key) {
			t.Errorf("%s: output %q, parsed %q, want %q", key, values[i], hook.Env[key], os.Getenv(key))
		}
	}
}

func (message) MissingAnswer(event, msg string) string { return agent.SystemMessage(event, msg) }

// The hook text must stay byte-identical across versions: Codex trust is a
// hash of it.
func TestCommandIsStable(t *testing.T) {
	h := testenv.Home(t)
	for bin, want := range map[string]string{
		filepath.Join(h, "go", "bin", "ctxtra"): `/bin/sh -c 'f="$HOME/go/bin/ctxtra"; if [ -x "$f" ]; then exec "$f" hook codex SessionStart --setup-version=1; else /bin/cat >/dev/null 2>&1; echo "{}"; fi'`,
		"/opt/homebrew/bin/ctxtra":              `/bin/sh -c 'f="/opt/homebrew/bin/ctxtra"; if [ -x "$f" ]; then exec "$f" hook codex SessionStart --setup-version=1; else /bin/cat >/dev/null 2>&1; echo "{}"; fi'`,
	} {
		got := agent.Command(bin, "codex", "SessionStart", nil)
		if got != want {
			t.Errorf("hook command changed:\n got %s\nwant %s", got, want)
		}
		if c, ok := agent.ParseCommand(got, nil); !ok || !reflect.DeepEqual(c, agent.Hook{Binary: bin, Agent: "codex", Event: "SessionStart", Slot: -1, Current: true}) {
			t.Errorf("ParseCommand(%s) = %+v, %v", got, c, ok)
		}
	}
	withMessage := agent.Command("/opt/ctxtra", "devin", "SessionStart", message{})
	if want := `/bin/sh -c 'f="/opt/ctxtra"; if [ -x "$f" ]; then exec "$f" hook devin SessionStart --setup-version=1; else /bin/cat >/dev/null 2>&1; ` +
		`echo "{\"systemMessage\":\"ctxtra: the ctxtra binary that this hook runs is missing; reinstall ctxtra, or run ctxtra setup --agent devin with the new binary\"}"; fi'`; withMessage != want {
		t.Errorf("command with a message:\n got %s\nwant %s", withMessage, want)
	}
	slot := agent.SlotCommand("/opt/ctxtra", "claude-code", "SessionStart", 17, 40, message{})
	if want := `/bin/sh -c 'f="/opt/ctxtra"; if [ -x "$f" ]; then exec "$f" hook claude-code SessionStart --slot 17 --of 40 --setup-version=1; else /bin/cat >/dev/null 2>&1; echo "{}"; fi'`; slot != want {
		t.Errorf("slot command:\n got %s\nwant %s", slot, want)
	}
	if c, ok := agent.ParseCommand(slot, message{}); !ok || c.Slot != 17 || c.Slots != 40 || c.Binary != "/opt/ctxtra" {
		t.Errorf("ParseCommand of a slot command: %+v, %v", c, ok)
	}
	if c, _ := agent.ParseCommand(agent.Command("/opt/ctxtra", "claude-code", "SessionStart", nil), nil); c.Slot != -1 || c.Slots != 0 {
		t.Errorf("a slot in a command without one: %+v", c)
	}
	if _, ok := agent.ParseCommand("other-tool hook codex SessionStart", nil); ok {
		t.Error("another tool's command recognized as ours")
	}
	for _, bad := range []string{"rel/ctxtra", `/a"b`, "/a$b", "/a'b", "/a`b", `/a\b`} {
		if agent.ValidBinaryPath(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// Setup recognizes the form it writes, with the answers for a missing
// binary that it writes: the message only in slot 0, or in a hook without
// slots, at session start. Another form, or a command edited by hand, isn't
// ours.
func TestParseCommand(t *testing.T) {
	const pre = `/bin/sh -c 'f="/opt/ctxtra"; if [ -x "$f" ]; then exec "$f" hook claude-code `
	msg := strings.ReplaceAll(agent.SystemMessage("SessionStart", agent.MissingMessage("claude-code")), `"`, `\"`)
	for cmd, want := range map[string]bool{
		agent.SlotCommand("/opt/ctxtra", "claude-code", "SessionStart", 0, 40, message{}): true,
		agent.SlotCommand("/opt/ctxtra", "claude-code", "SessionStart", 1, 40, message{}): true,
		agent.Command("/opt/ctxtra", "claude-code", "SessionStart", message{}):            true,
		agent.Command("/opt/ctxtra", "claude-code", "SubagentStart", message{}):           true,
		// Forms that setup doesn't write: {} where it writes the message,
		// cat without its path, --slot without --of and the other way round.
		pre + `SessionStart --slot 0 --of 40 --setup-version=1; else /bin/cat >/dev/null 2>&1; echo "{}"; fi'`: false,
		pre + `SessionStart; else cat >/dev/null 2>&1; echo "{}"; fi'`:                                         false,
		pre + `SessionStart --slot 3; else /bin/cat >/dev/null 2>&1; echo "{}"; fi'`:                           false,
		pre + `SessionStart --of 40 --setup-version=1; else /bin/cat >/dev/null 2>&1; echo "{}"; fi'`:          false,
		// The message where setup doesn't write it, or edited.
		pre + `SessionStart --slot 1 --of 40 --setup-version=1; else /bin/cat >/dev/null 2>&1; echo "` + msg + `"; fi'`: false,
		pre + `SubagentStart --setup-version=1; else /bin/cat >/dev/null 2>&1; echo "` + msg + `"; fi'`:                 false,
		pre + `SessionStart; else /bin/cat >/dev/null 2>&1; echo "{\"systemMessage\":\"gone\"}"; fi'`:                   false,
		pre + `SessionStart; else /bin/cat >/dev/null 2>&1; echo "{}" >&2; fi'`:                                         false,
		pre + `SessionStart; else cat >/dev/null 2>&1; echo "` + msg + `"; fi'`:                                         false,
		pre + `SessionStart --slot x; else /bin/cat >/dev/null 2>&1; echo "{}"; fi'`:                                    false,
	} {
		if _, ok := agent.ParseCommand(cmd, message{}); ok != want {
			t.Errorf("ParseCommand(%s) = %v, want %v", cmd, ok, want)
		}
	}
	// Without a message, only {} is ours.
	if _, ok := agent.ParseCommand(agent.Command("/opt/ctxtra", "claude-code", "SessionStart", message{}), nil); ok {
		t.Error("a message recognized for an agent without one")
	}
}

// Runs the command the way the agents do: through the shell, with the hook
// JSON on stdin, from a folder elsewhere and with a minimal PATH.
func TestCommandRuns(t *testing.T) {
	h := testenv.Home(t)
	bin := filepath.Join(h, "my tools", "ctxtra")
	testenv.Write(t, bin, "#!/bin/sh\necho \"args: $*\"\ncat\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func() string {
		t.Helper()
		cmd := exec.Command("/bin/sh", "-c", agent.Command(bin, "claude-code", "SessionStart", nil))
		cmd.Env = []string{"HOME=" + h, "PATH=/usr/bin:/bin"}
		cmd.Dir = t.TempDir()
		cmd.Stdin = strings.NewReader(`{"cwd":"/x"}`)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return string(out)
	}
	if got := run(); got != "args: hook claude-code SessionStart --setup-version=1\n{\"cwd\":\"/x\"}" {
		t.Errorf("output %q", got)
	}
	// With the binary gone the hook adds nothing and still reads all of
	// stdin, also with no system tools in PATH: the agent must not get a
	// broken pipe.
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if got := run(); got != "{}\n" {
		t.Errorf("missing binary: %q", got)
	}
	cmd := exec.Command("/bin/sh", "-c", agent.Command(bin, "claude-code", "SessionStart", message{}))
	cmd.Env = []string{"HOME=" + h, "PATH=/missing"}
	cmd.Stdin = strings.NewReader(`{"cwd":"/x"}`)
	var m map[string]string
	if out, err := cmd.Output(); err != nil || json.Unmarshal(out, &m) != nil || m["systemMessage"] != agent.MissingMessage("claude-code") || len(m) != 1 {
		t.Errorf("missing binary, with a message: %v %q", err, out)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("/bin/sh", "-c", agent.Command(bin, "claude-code", "SessionStart", nil))
	cmd.Env = []string{"HOME=" + h, "PATH=/missing"}
	cmd.Stdin = r
	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r.Close()
	_, werr := w.Write(make([]byte, 2<<20))
	w.Close()
	if err := cmd.Wait(); err != nil || werr != nil || out.String() != "{}\n" {
		t.Errorf("large input, empty PATH: write %v, exit %v, output %q", werr, err, out.String())
	}
}
