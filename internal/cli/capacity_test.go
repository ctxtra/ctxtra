package cli

import (
	"encoding/json"
	"os"
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

// A binding's trace names a file by the path its hit has: a file reached
// through a symlink is added or dropped under one name.
func TestTraceUsesRealPaths(t *testing.T) {
	h := testenv.Home(t)
	testenv.Write(t, filepath.Join(h, "real.md"), strings.Repeat("x", 600))
	if err := os.Symlink(filepath.Join(h, "real.md"), filepath.Join(h, "alias.md")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ max, status string }{{"", "added"}, {"max-ctx-chars: 30\n", "dropped"}} {
		testenv.Config(t, h, "schema: ctxtra/config/v1\n"+c.max+"bindings:\n  - {name: b, instructions: [~/alias.md]}\n")
		_, out, _ := run("context", "explain", h, "--json")
		var doc struct {
			Data struct {
				Bindings []struct {
					Files []struct {
						Path, Status string
						Chars        int
					}
				}
			}
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Data.Bindings) != 1 || len(doc.Data.Bindings[0].Files) != 1 {
			t.Fatalf("%v\n%s", err, out)
		}
		f := doc.Data.Bindings[0].Files[0]
		if f.Status != c.status || c.status == "added" && f.Chars != 600 || filepath.Base(f.Path) != "real.md" {
			t.Errorf("%q: %+v", c.max, f)
		}
	}
}
