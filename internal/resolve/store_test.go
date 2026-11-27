package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/source"
)

// While cleaning holds the store, a session waits once, not once per
// source or file, and gets nothing from the store; files on disk still
// apply.
func TestStoreBusyWaitsOnce(t *testing.T) {
	h := newHome(t)
	h.write("me.md", "ME")
	c1, c2 := strings.Repeat("a", 40), strings.Repeat("b", 40)
	lockFile := h.accept("a", "https://h/a.git", "branch: main", "", c1, map[string]string{"x.md": "x", "y.md": "y"}) +
		h.accept("b", "https://h/b.git", "branch: main", "", c2, map[string]string{"z.md": "z"})
	env := h.load("sources:\n  a: {repo: 'https://h/a.git', branch: main}\n  b: {repo: 'https://h/b.git', branch: main}\n"+
		"bindings:\n  - {name: x, instructions: [~/me.md, 'a:x.md', 'a:y.md', 'b:z.md']}\n", lockFile)
	unlock, err := fsutil.Lock(filepath.Join(paths.StateDir(), "store.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	began := time.Now()
	res := start(env, h.dir, "")
	if d := time.Since(began); d > source.TouchWait+500*time.Millisecond {
		t.Errorf("resolve took %v", d)
	}
	eq(t, "files", bases(res.Files), "me.md")
	if len(res.Errors) != 3 {
		t.Errorf("errors %v", res.Errors)
	}
	for _, e := range res.Errors {
		if !strings.Contains(e.Error(), "ctxtra's store is being cleaned") {
			t.Errorf("error %v", e)
		}
	}
}

// A session marks the commits it uses: cleanup keeps an old commit that a
// session started on, though it is no longer the accepted one.
func TestStoreKeepsWhatASessionUses(t *testing.T) {
	h := newHome(t)
	id := strings.Repeat("a", 40)
	lockFile := h.accept("team", "https://h/team.git", "branch: main", "", id, nil)
	gen := source.GenDir("https://h/team.git", "", id)
	h.skill(strings.TrimPrefix(filepath.Join(gen, "skills", "deploy"), h.dir+"/"), "deploy")
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(gen, old, old); err != nil {
		t.Fatal(err)
	}
	res := start(h.load("sources:\n  team: {repo: 'https://h/team.git', branch: main}\nbindings:\n  - {name: b, skills: ['team:skills/*']}\n", lockFile), h.dir, "")
	noErrors(t, res)
	eq(t, "skills", skillNames(res), "deploy")
	if n := source.Clean(nil, 30*24*time.Hour); n != 0 {
		t.Errorf("cleanup deleted %d commits", n)
	}
	if _, err := os.Stat(filepath.Join(gen, "skills", "deploy", "SKILL.md")); err != nil {
		t.Error(err)
	}
}
