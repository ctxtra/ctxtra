package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ctxtra/ctxtra/internal/cfg"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

var commitA = strings.Repeat("a", 40)

// catalog writes the config and lock into a new home, where prepare has
// made what the machine has, and returns their catalog.
func catalog(t *testing.T, config, lockFile string, prepare func(home string)) (*Catalog, string) {
	t.Helper()
	h := testenv.Home(t)
	prepare(h)
	path := testenv.Config(t, h, "schema: ctxtra/config/v1\n"+config)
	c := cfg.Parse([]byte("schema: ctxtra/config/v1\n"+config), cfg.Options{})
	for _, e := range c.Errors {
		t.Fatalf("config: %v", e)
	}
	l := lock.Parse([]byte("schema: ctxtra/lock/v1\n" + lockFile))
	return New(c, l, path), h
}

func entry(text string) cfg.Entry {
	c := cfg.Parse([]byte(fmt.Sprintf("schema: ctxtra/config/v1\nbindings:\n  - {name: b, instructions: ['%s']}\n", text)), cfg.Options{})
	return c.Bindings[0].Instructions[0]
}

func refs(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Ref())
	}
	return out
}

func TestSourceStates(t *testing.T) {
	const config = `sources:
  team: {repo: 'https://github.com/acme/team.git', branch: main}
  moved: {repo: 'https://github.com/acme/moved.git', branch: next}
  absent: {repo: 'https://github.com/acme/absent.git', branch: main}
  clone: {repo: ~/dev/clone, branch: main}
  live: {dir: ~/live}
  gone: {dir: ~/gone}
`
	lockFile := fmt.Sprintf(`sources:
  team: {repo: 'git@github.com:acme/team.git', branch: main, commit: '%[1]s'}
  moved: {repo: 'https://github.com/acme/moved.git', branch: main, commit: '%[1]s'}
  absent: {repo: 'https://github.com/acme/absent.git', branch: main, commit: '%[1]s'}
`, commitA)
	h := testenv.Home(t)
	testenv.Write(t, filepath.Join(source.GenDir("git@github.com:acme/team.git", commitA), "x.md"), "x")
	testenv.Write(t, filepath.Join(h, "live", "y.md"), "y")
	path := testenv.Config(t, h, "schema: ctxtra/config/v1\n"+config)
	c := New(cfg.Parse([]byte("schema: ctxtra/config/v1\n"+config), cfg.Options{}), lock.Parse([]byte("schema: ctxtra/lock/v1\n"+lockFile)), path)

	want := map[string]State{"team": Active, "moved": Inactive, "absent": Inactive, "clone": NotHere, "live": Active, "gone": NotHere}
	for name, state := range want {
		if s := c.Source(name); s.State != state {
			t.Errorf("%s: state %v (%s), want %v", name, s.State, s.Why, state)
		}
	}
	if top := c.Source("team").Top; top != source.GenDir("git@github.com:acme/team.git", commitA) {
		t.Errorf("team's top %s: the store keeps a commit under the address it was downloaded from", top)
	}
	var names []string
	for _, e := range c.Errors() {
		names = append(names, e.Err.Part.Name)
	}
	if !slices.Equal(names, []string{"moved", "absent"}) {
		t.Errorf("errors of %v", names)
	}
	_, err := c.Paths(entry("gone:*.md"), Files)
	var u *Unavailable
	if !errors.As(err, &u) || u.Source.Name != "gone" {
		t.Errorf("an entry of a source not on this machine: %v", err)
	}
}

func TestPaths(t *testing.T) {
	c, h := catalog(t, "sources: {dotfiles: {dir: ~/dotfiles}}\n", "", func(h string) {
		testenv.Write(t, filepath.Join(h, "dotfiles", "agents", "a.md"), "a")
		testenv.Write(t, filepath.Join(h, "dotfiles", "agents", "deep", "b.md"), "b")
		testenv.Skill(t, filepath.Join(h, "dotfiles", "skills", "review"), "name: review\ndescription: d")
		if err := os.MkdirAll(filepath.Join(h, "dotfiles", "skills", "empty"), 0o755); err != nil {
			t.Fatal(err)
		}
		testenv.Write(t, filepath.Join(h, ".config", "ctxtra", "notes.md"), "n")
	})

	items, err := c.Paths(entry("dotfiles:agents/**/*.md"), Files)
	if err != nil || !slices.Equal(refs(items), []string{"dotfiles:agents/a.md", "dotfiles:agents/deep/b.md"}) {
		t.Errorf("files %v, %v", refs(items), err)
	}
	items, err = c.Paths(entry("dotfiles:skills/*"), Skills)
	if err != nil || !slices.Equal(refs(items), []string{"dotfiles:skills/review"}) {
		t.Errorf("skills %v, %v", refs(items), err)
	}
	items, err = c.Paths(entry("notes.md"), Files)
	if err != nil || len(items) != 1 || items[0].Path != filepath.Join(h, ".config", "ctxtra", "notes.md") {
		t.Errorf("a path relative to the config: %v, %v", refs(items), err)
	}
	if _, err := c.Paths(entry("dotfiles:agents/*.txt"), Files); err == nil || !strings.Contains(err.Error(), "matches no file") {
		t.Errorf("no match: %v", err)
	}
	if _, err := c.Paths(entry("nope:*.md"), Files); err == nil || !strings.Contains(err.Error(), "no source nope") {
		t.Errorf("unknown source: %v", err)
	}
	content, err := c.Read(items[0], nil)
	if err != nil || string(content) != "n" {
		t.Errorf("read %q, %v", content, err)
	}
}

func TestGlobLimit(t *testing.T) {
	c, _ := catalog(t, "sources: {dotfiles: {dir: ~/dotfiles}}\n", "", func(h string) {
		for i := range 101 {
			dir := filepath.Join(h, "dotfiles", fmt.Sprint(i))
			for j := range 100 {
				testenv.Write(t, filepath.Join(dir, fmt.Sprintf("%d.txt", j)), "")
			}
		}
	})
	if _, err := c.Paths(entry("dotfiles:**/*.md"), Files); err == nil || !strings.Contains(err.Error(), "10,000") {
		t.Errorf("a pattern over a large folder: %v", err)
	}
}

func TestStoredSymlinkLeftOut(t *testing.T) {
	config := "sources: {team: {repo: 'https://github.com/acme/team.git', branch: main}}\n"
	lockFile := fmt.Sprintf("sources:\n  team: {repo: 'https://github.com/acme/team.git', branch: main, commit: '%s'}\n", commitA)
	h := testenv.Home(t)
	gen := source.GenDir("https://github.com/acme/team.git", commitA)
	testenv.Write(t, filepath.Join(h, "secret.md"), "secret")
	testenv.Write(t, filepath.Join(gen, "a.md"), "a")
	if err := os.Symlink(filepath.Join(h, "secret.md"), filepath.Join(gen, "b.md")); err != nil {
		t.Fatal(err)
	}
	path := testenv.Config(t, h, "schema: ctxtra/config/v1\n"+config)
	c := New(cfg.Parse([]byte("schema: ctxtra/config/v1\n"+config), cfg.Options{}), lock.Parse([]byte("schema: ctxtra/lock/v1\n"+lockFile)), path)
	items, err := c.Paths(entry("team:*.md"), Files)
	if err != nil || !slices.Equal(refs(items), []string{"team:a.md"}) {
		t.Errorf("files %v, %v", refs(items), err)
	}
	if _, err := c.Paths(entry("team:b.md"), Files); err == nil {
		t.Error("a symlink in a repo source is read")
	}
}

func TestStoreReadOnlyBySourcePath(t *testing.T) {
	config := "sources: {team: {repo: 'https://github.com/acme/team.git', branch: main}}\n"
	lockFile := fmt.Sprintf("sources:\n  team: {repo: 'https://github.com/acme/team.git', branch: main, commit: '%s'}\n", commitA)
	h := testenv.Home(t)
	gen := source.GenDir("https://github.com/acme/team.git", commitA)
	testenv.Write(t, filepath.Join(gen, "a.md"), "a")
	path := testenv.Config(t, h, "schema: ctxtra/config/v1\n"+config)
	c := New(cfg.Parse([]byte("schema: ctxtra/config/v1\n"+config), cfg.Options{}), lock.Parse([]byte("schema: ctxtra/lock/v1\n"+lockFile)), path)
	items, err := c.Paths(entry(filepath.Join(gen, "a.md")), Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(items[0], nil); !IsUntrusted(err) {
		t.Errorf("a file of the store by its path: %v", err)
	}
}

func TestGlobCharactersInTheHomeFolder(t *testing.T) {
	c, h := catalog(t, "", "", func(h string) {
		home := filepath.Join(h, "a[b]{c}")
		testenv.Write(t, filepath.Join(home, "notes", "x.md"), "x")
		t.Setenv("HOME", home)
	})
	home := filepath.Join(h, "a[b]{c}")
	items, err := c.Paths(entry("~/notes/*.md"), Files)
	if err != nil || len(items) != 1 || items[0].Path != filepath.Join(home, "notes", "x.md") {
		t.Fatalf("items %v, %v", refs(items), err)
	}
	if ok, _ := doublestar.Match(c.DiskPattern("~/notes/x*"), filepath.ToSlash(items[0].Path)); !ok {
		t.Errorf("pattern %s doesn't match %s", c.DiskPattern("~/notes/x*"), items[0].Path)
	}
}
