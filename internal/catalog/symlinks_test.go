package catalog

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestCommittedSymlinks(t *testing.T) {
	h := testenv.Home(t)
	repo := testenv.Repo(t, filepath.Join(h, "repo"), "")
	testenv.Write(t, filepath.Join(repo, "docs", "a.md"), "REGULAR")
	testenv.Skill(t, filepath.Join(repo, "skills", "good"), "name: good\ndescription: A healthy skill.")
	testenv.Write(t, filepath.Join(h, "outside", "hidden.md"), "TARGET-MUST-NOT-BE-READ")
	for name, target := range map[string]string{
		"docs/link.md":           "a.md",
		"docs/linked":            filepath.Join(h, "outside"),
		"only/children/link.md":  "/missing-target",
		"ctxtra-source.yaml":     "docs/a.md",
		"skills/linked/SKILL.md": "../good/SKILL.md",
		"skills/alias":           "good",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(repo, name)); err != nil {
			t.Fatal(err)
		}
	}
	testenv.Run(t, repo, "git", "add", ".")
	testenv.Run(t, repo, "git", "commit", "-qm", "add content")
	ctx := context.Background()
	r := source.Open(repo)
	commit, err := r.FetchBranch(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	const remote = "https://example.invalid/team.git"
	gen, err := r.MaterializeAs(ctx, commit, "", remote)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := r.Snapshot(ctx, commit, "")
	if err != nil {
		t.Fatal(err)
	}
	conf := "schema: ctxtra/config/v1\nsources: {team: {repo: '" + remote + "', branch: main}}\n"
	path := testenv.Config(t, h, conf)
	l := lock.Parse([]byte("schema: ctxtra/lock/v1\nsources:\n  team: {repo: '" + remote + "', branch: main, commit: " + commit + "}\n"))
	stored := New(config.Parse([]byte(conf), config.Options{}), l, path)
	for name, cat := range map[string]*Catalog{
		"download": stored,
		"snapshot": stored.WithFiles("team", snapshot, gen),
		"checkout": Checkout("team", repo),
	} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				pattern string
				want    []string
				links   []string
			}{
				{"docs/*.md", []string{"team:docs/a.md"}, []string{"team:docs/link.md"}},
				{"docs/link.md", nil, []string{"team:docs/link.md"}},
				{"docs/linked/*.md", nil, []string{"team:docs/linked"}},
				{"docs/**/*.md", []string{"team:docs/a.md"}, []string{"team:docs/link.md", "team:docs/linked"}},
				{"only/**/*.md", nil, []string{"team:only/children/link.md"}},
			} {
				t.Run(tc.pattern, func(t *testing.T) {
					items, err := expand(cat, "team:"+tc.pattern, Files)
					if !slices.Equal(refs(items), tc.want) {
						t.Errorf("files %v, want %v", refs(items), tc.want)
					}
					for _, link := range tc.links {
						if err == nil || !strings.Contains(err.Error(), link) || !strings.Contains(err.Error(), "symlink") {
							t.Errorf("missing symlink error for %s: %v", link, err)
						}
					}
				})
			}
			items, _ := expand(cat, "team:**/*", Files)
			for _, item := range items {
				if strings.Contains(item.Rel, ".git") || strings.Contains(item.Rel, "hidden.md") {
					t.Errorf("private metadata or link target is exposed: %s", item.Ref())
				}
			}
			if items, err := expand(cat, "team:.git/ctxtra-symlinks", Files); err == nil || len(items) != 0 {
				t.Errorf("metadata is readable as source content: %v, %v", refs(items), err)
			}
			if m, errs := cat.Manifest(cat.Source("team")); m != nil || len(errs) != 1 || !strings.Contains(errs[0].Error(), "symlink") {
				t.Errorf("symlinked manifest: %v, %v", m, errs)
			}
			items, err := expand(cat, "team:skills/*", Skills)
			if !slices.Equal(refs(items), []string{"team:skills/good"}) || err == nil || !strings.Contains(err.Error(), "symlink") || !strings.Contains(err.Error(), "team:skills/linked/SKILL.md") || !strings.Contains(err.Error(), "team:skills/alias") {
				t.Errorf("symlinked skills: %v, %v", refs(items), err)
			}
		})
	}
	// The accepted source must still diagnose links with no cache or remote.
	if err := os.RemoveAll(source.GitDir()); err != nil {
		t.Fatal(err)
	}
	stored = New(config.Parse([]byte(conf), config.Options{}), l, path)
	if _, err := expand(stored, "team:docs/*.md", Files); err == nil || !strings.Contains(err.Error(), "team:docs/link.md") {
		t.Errorf("offline symlink error: %v", err)
	}
}
