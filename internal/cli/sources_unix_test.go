//go:build unix

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// source download fetches through the config's address when the lock has
// another spelling of the same repository: the one that works now.
func TestDownloadUsesConfigAddress(t *testing.T) {
	h := testenv.Home(t)
	bin := filepath.Join(h, "fakebin")
	log := filepath.Join(h, "git.log")
	testenv.Write(t, filepath.Join(bin, "git"), "#!/bin/sh\necho \"$@\" >> '"+log+"'\nexit 1\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	testenv.Config(t, h, "schema: ctxtra/config/v1\nsources:\n  team: {repo: 'https://example.com/team.git', branch: main}\n")
	lockFile := "schema: ctxtra/lock/v1\nsources:\n  team:\n    repo: git@example.com:team.git\n    branch: main\n    commit: " + strings.Repeat("a", 40) + "\n"
	lockPath := filepath.Join(h, ".config", "ctxtra", "ctxtra.lock")
	testenv.Write(t, lockPath, lockFile)
	if code, _, _ := run("source", "download", "team"); code != 1 {
		t.Errorf("exit %d with a git that fails", code)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "https://example.com/team.git") || strings.Contains(string(b), "git@example.com") {
		t.Errorf("git ran with:\n%s", b)
	}
	if got, _ := os.ReadFile(lockPath); string(got) != lockFile {
		t.Errorf("the lock changed:\n%s", got)
	}
}

// source add lists the files of a source without a manifest, up to a
// bound, and none of a source with one.
func TestSourceAddListsFiles(t *testing.T) {
	h := testenv.Home(t)
	for i := range 60 {
		testenv.Write(t, filepath.Join(h, "many", fmt.Sprintf("f%02d.md", i)), "x")
	}
	testenv.Write(t, filepath.Join(h, "declared", "a.md"), "x")
	testenv.Write(t, filepath.Join(h, "declared", "ctxtra-source.yaml"), "schema: ctxtra/source/v1\n")
	t.Chdir(h)
	_, out, errOut := run("source", "add", "--dir", "many")
	if !strings.Contains(out, "  f49.md\n") || strings.Contains(out, "f50.md") || !strings.Contains(out, "only the first 50 are listed") {
		t.Errorf("many:\n%s%s", out, errOut)
	}
	if _, out, _ := run("source", "add", "--dir", "declared"); strings.Contains(out, "a.md") {
		t.Errorf("declared:\n%s", out)
	}
	// A symlinked folder lists the files it leads to, not the links in it.
	testenv.Write(t, filepath.Join(h, "notes", "rules.md"), "x")
	if err := os.Symlink(filepath.Join(h, "many", "f00.md"), filepath.Join(h, "notes", "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(h, "notes"), filepath.Join(h, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, out, errOut := run("source", "add", "--dir", "alias", "--name", "notes"); !strings.Contains(out, "its files:\n  rules.md\n") || strings.Contains(out, "link.md") {
		t.Errorf("alias:\n%s%s", out, errOut)
	}
}
