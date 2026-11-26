package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// An instruction over an agent's capacity fails doctor and context
// explain, also in JSON, and context files --agent shows what that agent's
// sessions get.
func TestCapacityErrors(t *testing.T) {
	h := testenv.Home(t)
	testenv.Write(t, filepath.Join(h, "big.md"), strings.Repeat("x", 400000))
	testenv.Write(t, filepath.Join(h, "small.md"), "small")
	testenv.Config(t, h, "schema: ctxtra/config/v1\nbindings:\n  - {name: all, instructions: [~/small.md, ~/big.md]}\n")
	for _, args := range [][]string{
		{"doctor", h},
		{"context", "explain", h},
		{"context", "explain", h, "--agent", "claude-code"},
	} {
		if code, out, _ := run(args...); code != 1 || !strings.Contains(out, "limit of") {
			t.Errorf("%v: exit %d\n%s", args, code, out)
		}
	}
	for _, args := range [][]string{{"doctor", h, "--json"}, {"context", "explain", h, "--json"}, {"context", "files", h, "--agent", "claude-code", "--json"}} {
		code, out, _ := run(args...)
		var doc struct {
			Diagnostics []struct{ Severity, Message string }
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		found := false
		for _, d := range doc.Diagnostics {
			found = found || d.Severity == "error" && strings.Contains(d.Message, "big.md")
		}
		if code != 1 || !found {
			t.Errorf("%v: exit %d, diagnostics %+v", args, code, doc.Diagnostics)
		}
	}
	code, out, _ := run("context", "files", h, "--agent", "claude-code")
	if code != 1 || strings.Contains(out, "big.md") || !strings.Contains(out, "small.md") {
		t.Errorf("files --agent: exit %d\n%s", code, out)
	}
	if code, out, _ := run("context", "files", h); code != 0 || !strings.Contains(out, "big.md") {
		t.Errorf("files without an agent: exit %d\n%s", code, out)
	}
}
