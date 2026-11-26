//go:build unix

package cli

import (
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
