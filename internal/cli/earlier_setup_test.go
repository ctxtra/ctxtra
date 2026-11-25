package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/setup"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestEarlierEntriesAddNothing(t *testing.T) {
	h := testenv.Home(t)
	testenv.Write(t, filepath.Join(h, "rules.md"), "PRIVATE INSTRUCTION")
	testenv.Config(t, h, "schema: ctxtra/v1\nbindings:\n  - name: me\n    match: {global: true}\n    instructions: [~/rules.md]\n")
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
	testenv.Config(t, h, "schema: ctxtra/v1\n")
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
