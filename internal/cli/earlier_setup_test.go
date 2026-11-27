package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/setup"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestEarlierEntriesAddNothing(t *testing.T) {
	h := testenv.Home(t)
	testenv.Write(t, filepath.Join(h, "rules.md"), "PRIVATE INSTRUCTION")
	testenv.Config(t, h, "schema: ctxtra/config/v1\nbindings:\n  - name: me\n    instructions: [~/rules.md]\n")
	for _, name := range []string{"claude", "copilot", "hermes", "claude-code", "codex", "cursor", "opencode", "pi", "antigravity", "devin", "muse", "dsh"} {
		t.Run(name, func(t *testing.T) {
			if code, out, _ := run("hook", name, "SessionStart", "--slot", "0", "--of", "40"); code != 0 || out != "{}\n" {
				t.Fatalf("earlier hook: %d, %q", code, out)
			}
			if code, out, _ := run("mcp", "proxy", name); code != 0 || out != "" {
				t.Fatalf("earlier proxy: %d, %q", code, out)
			}
		})
	}
}

func TestDoctorEarlierEntry(t *testing.T) {
	h, bin := noticeHome(t)
	binary = func() (string, []string, error) { return bin, nil, nil }
	t.Cleanup(func() { binary = setup.Binary })
	testenv.Config(t, h, "schema: ctxtra/config/v1\n")
	testenv.Write(t, filepath.Join(h, ".ctxtra", "unused"), "")
	testenv.Write(t, filepath.Join(h, ".claude", "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"ctxtra hook claude SessionStart"}]}]}}`)
	if code, out, _ := run("doctor", h); code != 1 || !strings.Contains(out, "ctxtra setup --agent claude-code") ||
		!strings.Contains(out, "~/.ctxtra can be deleted once every agent is set up again") {
		t.Fatalf("doctor: %d, %s", code, out)
	}
	if code, _, errOut := run("setup", "--agent", "claude-code"); code != 0 {
		t.Fatalf("setup: %d, %s", code, errOut)
	}
	if code, out, _ := run("doctor", h); code != 0 || strings.Contains(out, "earlier ctxtra") {
		t.Fatalf("doctor after replacement: %d, %s", code, out)
	}
}

// A setup that an earlier ctxtra wrote has a hook of each slot: the notices
// group them in a line for each file, and doctor counts them by event.
func TestEarlierHooksGrouped(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("PATH", t.TempDir())
	bin := fakeBin(t, filepath.Join(h, "build", "ctxtra"))
	self := filepath.Join(h, "bin", "ctxtra")
	running = func() string { return self }
	binary = func() (string, []string, error) { return self, nil, nil }
	t.Cleanup(func() { running, binary = setup.Running, setup.Binary })
	testenv.Config(t, h, "schema: ctxtra/config/v1\n")
	for _, c := range testenv.Clients(t) {
		if c.ID == "current" && c.Agent == "claude-code" {
			c.Load(t, h, bin)
		}
	}
	for _, c := range testenv.Clients(t) {
		if c.ID == "current" && c.Agent == "codex" {
			c.Load(t, filepath.Join(h, "codex"), bin)
		}
	}
	// What an earlier ctxtra wrote: the hooks without the setup version.
	settings := filepath.Join(h, ".claude", "settings.json")
	hooks := filepath.Join(h, ".codex", "hooks.json")
	for from, to := range map[string]string{settings: settings, filepath.Join(h, "codex", ".codex", "hooks.json"): hooks} {
		b, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		testenv.Write(t, to, strings.ReplaceAll(strings.ReplaceAll(string(b), filepath.Join(h, "codex"), h), " --setup-version=1", ""))
	}

	_, _, errOut := run("source", "list")
	want := []string{
		"claude-code: 80 hooks in ~/.claude/settings.json are from an earlier ctxtra; run ctxtra setup --agent claude-code",
		"codex: 2 hooks in ~/.codex/hooks.json are from an earlier ctxtra; run ctxtra setup --agent codex (Codex will then ask you to trust the hooks again)",
	}
	var got []string
	for line := range strings.Lines(errOut) {
		if strings.Contains(line, "earlier ctxtra") {
			got = append(got, strings.TrimSuffix(line, "\n"))
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("notices:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	_, out, _ := run("doctor")
	for _, line := range []string{
		"    80 hooks in ~/.claude/settings.json are from an earlier ctxtra (40 SessionStart, 40 SubagentStart); run ctxtra setup --agent claude-code\n",
		"    2 hooks in ~/.codex/hooks.json are from an earlier ctxtra (1 SessionStart, 1 SubagentStart); run ctxtra setup --agent codex (Codex will then ask you to trust the hooks again)\n",
	} {
		if strings.Count(out, line) != 1 {
			t.Errorf("doctor doesn't have %q once:\n%s", line, out)
		}
	}
	if n := strings.Count(out, "from an earlier ctxtra"); n != 2 {
		t.Errorf("doctor has %d notices about earlier hooks:\n%s", n, out)
	}
}
