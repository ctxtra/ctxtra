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
	commit, err := r.FetchHead(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Materialize(ctx, commit); err != nil {
		t.Fatal(err)
	}
	cfg := testenv.Config(t, h, "schema: ctxtra/v1\nsources:\n  team: {local-git: '"+dir+"', commit: '"+commit+"'}\nbindings:\n  - {name: all, match: {global: true}, instructions: [ctxtra://team/AGENTS.md]}\n")
	before, _ := Session(h, cfg)
	if len(before.Files) != 1 || len(before.Problems) != 0 {
		t.Fatalf("before: %+v", before)
	}
	if err := os.RemoveAll(paths.CacheDir()); err != nil {
		t.Fatal(err)
	}
	after, _ := Session(h, cfg)
	if after.Render() != before.Render() || len(after.Problems) != 0 {
		t.Fatalf("cache deletion changed session: %+v", after)
	}
	if _, err := os.Stat(paths.CacheDir()); !os.IsNotExist(err) {
		t.Fatalf("session accessed cache: %v", err)
	}
	if got, err := source.Open(dir).FetchHead(ctx, dir); err != nil || got != commit {
		t.Fatalf("fetch after deletion: %s %v", got, err)
	}
}
