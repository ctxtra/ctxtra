package resolve

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestCacheRemoved(t *testing.T) {
	h := testenv.Home(t)
	dir := testenv.Repo(t, filepath.Join(h, "team"), "")
	r := source.Open(dir)
	ctx := context.Background()
	commit, err := r.FetchBranch(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Materialize(ctx, commit, ""); err != nil {
		t.Fatal(err)
	}
	cfg := testenv.Config(t, h, "schema: ctxtra/config/v1\nsources:\n  team: {repo: '"+dir+"', branch: main}\nbindings:\n  - {name: all, instructions: ['team:AGENTS.md']}\n")
	testenv.Write(t, filepath.Join(filepath.Dir(cfg), "ctxtra.lock"), "schema: ctxtra/lock/v1\nsources:\n  team:\n    repo: "+dir+"\n    branch: main\n    commit: "+commit+"\n")
	before := Load(cfg, agentNames).Resolve(Input{Dir: h})
	if len(before.Files) != 1 || len(before.Errors) != 0 {
		t.Fatalf("before: %+v", before)
	}
	if err := os.RemoveAll(paths.CacheDir()); err != nil {
		t.Fatal(err)
	}
	after := Load(cfg, agentNames).Resolve(Input{Dir: h})
	if after.Render() != before.Render() || len(after.Errors) != 0 {
		t.Fatalf("cache deletion changed session: %+v", after)
	}
	if _, err := os.Stat(paths.CacheDir()); !os.IsNotExist(err) {
		t.Fatalf("session accessed cache: %v", err)
	}
	if got, err := source.Open(dir).FetchBranch(ctx, "main"); err != nil || got != commit {
		t.Fatalf("fetch after deletion: %s %v", got, err)
	}
}
