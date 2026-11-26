package source

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// A tag is fetched to its commit, also an annotated one, and a moved tag
// gives the new commit.
func TestFetchTag(t *testing.T) {
	ctx := context.Background()
	dir, url := team(t)
	first := commit(t, dir, map[string]string{"a.md": "A"})
	testenv.Run(t, dir, "git", "tag", "light")
	testenv.Run(t, dir, "git", "tag", "-a", "annotated", "-m", "x")
	r := Open(url)
	for _, tag := range []string{"light", "annotated"} {
		got, err := r.FetchTag(ctx, tag)
		if err != nil || got != first {
			t.Errorf("%s: %q, %v; want %q", tag, got, err, first)
		}
		if got, err := r.TagHead(ctx, tag); err != nil || got != first {
			t.Errorf("TagHead %s: %q, %v; want %q", tag, got, err, first)
		}
	}
	second := commit(t, dir, map[string]string{"a.md": "B"})
	testenv.Run(t, dir, "git", "tag", "-f", "-a", "annotated", "-m", "moved")
	if got, err := r.FetchTag(ctx, "annotated"); err != nil || got != second {
		t.Errorf("moved: %q, %v; want %q", got, err, second)
	}
	if got, err := r.TagHead(ctx, "annotated"); err != nil || got != second {
		t.Errorf("TagHead moved: %q, %v", got, err)
	}
	if _, err := r.FetchTag(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "can't fetch the tag nope") {
		t.Errorf("missing tag: %v", err)
	}
	if _, err := r.TagHead(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "the tag nope is not in") {
		t.Errorf("TagHead missing tag: %v", err)
	}
	for _, bad := range []string{"-x", "a..b", "x.lock", ""} {
		if CheckTag(bad) == nil {
			t.Errorf("CheckTag(%q)", bad)
		}
	}
}

// source add --rev takes any revision and gives its full ID: a full or
// short commit ID, a tag or a branch.
func TestResolveRev(t *testing.T) {
	ctx := context.Background()
	dir, url := team(t)
	first := commit(t, dir, map[string]string{"a.md": "A"})
	testenv.Run(t, dir, "git", "tag", "-a", "v1", "-m", "x")
	second := commit(t, dir, map[string]string{"a.md": "B"})
	testenv.Run(t, dir, "git", "branch", "side", first)
	for _, c := range []struct{ rev, want string }{
		{second, second}, {first[:9], first}, {strings.ToUpper(first[:9]), first}, {"v1", first}, {"side", first}, {"main", second},
	} {
		if got, err := Open(url).ResolveRev(ctx, c.rev); err != nil || got != c.want {
			t.Errorf("%s: %q, %v; want %q", c.rev, got, err, c.want)
		}
	}
	for _, bad := range []string{"nope", "-x", "abc"} {
		if _, err := Open(url).ResolveRev(ctx, bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	// The branches and tags it fetched to find the revision are not kept.
	r := Open(url)
	out, err := r.git(ctx, true, nil, "for-each-ref", "--format=%(refname)", "refs/ctxtra/revs/")
	if err != nil || len(out) > 0 {
		t.Errorf("refs left: %q, %v", out, err)
	}
}

// A folder of a repository: its files by their paths from it, with the
// limits checked for the whole commit; a symlink is never followed as the
// folder.
func TestFilesOfSubdir(t *testing.T) {
	ctx := context.Background()
	dir, url := team(t)
	c := commit(t, dir, map[string]string{"teams/billing/skills/x/SKILL.md": "X", "teams/iam/y.md": "Y"})
	if err := os.Symlink("teams/billing", filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	withLink := commit(t, dir, nil)
	r := Open(url)
	if _, err := r.FetchBranch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	entries, _, err := r.Files(ctx, c, "teams/billing")
	if err != nil || len(entries) != 1 || entries[0].Path != "skills/x/SKILL.md" {
		t.Errorf("entries %+v, %v", entries, err)
	}
	if _, _, err := r.Files(ctx, withLink, "linked"); err == nil || !strings.Contains(err.Error(), "subdir linked is a blob, not a folder") {
		t.Errorf("a symlink as subdir: %v", err)
	}
	if _, _, err := r.Files(ctx, c, "teams/none"); err == nil || !strings.Contains(err.Error(), "has no folder teams/none") {
		t.Errorf("a missing subdir: %v", err)
	}
	a, err1 := r.TreeID(ctx, c, "teams/billing")
	b, err2 := r.TreeID(ctx, withLink, "teams/billing")
	if err1 != nil || err2 != nil || a != b {
		t.Errorf("tree IDs %s %v, %s %v", a, err1, b, err2)
	}
	// Over the limits outside subdir: refused, and nothing is stored.
	big := commit(t, dir, map[string]string{"assets/big.bin": strings.Repeat("x", MaxFileBytes+1)})
	if _, err := r.FetchBranch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Materialize(ctx, big, "teams/billing"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a large file outside subdir: %v", err)
	}
	if Has(url, "teams/billing", big) {
		t.Error("stored over the limit")
	}
	// Two folders of one commit have their own generations.
	g1, err1 := r.Materialize(ctx, c, "teams/billing")
	g2, err2 := r.Materialize(ctx, c, "teams/iam")
	if err1 != nil || err2 != nil || g1 == g2 || !Exists(filepath.Join(g1, "skills/x/SKILL.md")) || !Exists(filepath.Join(g2, "y.md")) ||
		Exists(filepath.Join(g1, "y.md")) {
		t.Errorf("generations %s %v, %s %v", g1, err1, g2, err2)
	}
}

// A snapshot has the files that the store would have, read from the cache,
// and writing nothing outside it.
func TestSnapshot(t *testing.T) {
	ctx := context.Background()
	dir, url := team(t)
	if err := os.MkdirAll(filepath.Join(dir, "teams/billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.md", filepath.Join(dir, "teams/billing/link.md")); err != nil {
		t.Fatal(err)
	}
	c := commit(t, dir, map[string]string{"teams/billing/a.md": "A", "teams/billing/skills/x/SKILL.md": "X", "other.md": "O"})
	r := Open(url)
	if _, err := r.FetchBranch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	s, err := r.Snapshot(ctx, c, "teams/billing")
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(s, "a.md", "skills/x/SKILL.md"); err != nil {
		t.Error(err)
	}
	if b, err := fs.ReadFile(s, "skills/x/SKILL.md"); err != nil || string(b) != "X" {
		t.Errorf("SKILL.md: %q, %v", b, err)
	}
	if _, err := fs.Stat(s, "link.md"); err == nil || len(s.Skipped) != 1 || s.Skipped[0].Path != "link.md" {
		t.Errorf("the symlink: %v, skipped %+v", err, s.Skipped)
	}
	if _, err := fs.Stat(s, "other.md"); err == nil {
		t.Error("a file outside subdir")
	}
	if Exists(StoreDir()) {
		t.Error("a snapshot wrote to the store")
	}
}
