package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestCleanStoreKeepsAcceptedCommits(t *testing.T) {
	h := testenv.Home(t)
	url := "https://h/team"
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	old := time.Now().Add(-40 * 24 * time.Hour)
	for _, commit := range []string{a, b, c} {
		g := source.GenDir(url, "", commit)
		testenv.Write(t, filepath.Join(g, "x.md"), "x")
		os.Chtimes(g, old, old)
	}
	mine := testenv.Config(t, h, "schema: ctxtra/config/v1\nsources:\n  team: {repo: '"+url+"', branch: main}\n")
	writeLock(t, mine, "  team:\n    repo: "+url+"\n    branch: main\n    commit: "+a+"\n")
	var out strings.Builder
	s := newSources(IO{Out: &out, Err: &out})
	s.cleanStore()
	for commit, want := range map[string]bool{a: true, b: false, c: false} {
		if source.Has(url, "", commit) != want {
			t.Errorf("%s in the store: %v, want %v (%s)", commit[:1], !want, want, out.String())
		}
	}
	// An unreadable config may pin anything, so it stops cleaning.
	testenv.Write(t, filepath.Join(source.GenDir(url, "", b), "x.md"), "x")
	os.Chtimes(source.GenDir(url, "", b), old, old)
	os.WriteFile(mine, []byte("kind: ["), 0o600)
	out.Reset()
	s.cleanStore()
	if !source.Has(url, "", b) || !strings.Contains(out.String(), "the store is not cleaned") {
		t.Errorf("unreadable config: %s", out.String())
	}
}
