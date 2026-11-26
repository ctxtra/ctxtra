package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

var commitA = strings.Repeat("a", 40)

// catalog writes the config and lock into a new home, where prepare has
// made what the machine has, and returns their catalog.
func catalog(t *testing.T, conf, lockFile string, prepare func(home string)) (*Catalog, string) {
	t.Helper()
	h := testenv.Home(t)
	prepare(h)
	path := testenv.Config(t, h, "schema: ctxtra/config/v1\n"+conf)
	c := config.Parse([]byte("schema: ctxtra/config/v1\n"+conf), config.Options{})
	for _, e := range c.Errors {
		t.Fatalf("config: %v", e)
	}
	l := lock.Parse([]byte("schema: ctxtra/lock/v1\n" + lockFile))
	return New(c, l, path), h
}

func entry(text string) config.Entry {
	c := config.Parse([]byte(fmt.Sprintf("schema: ctxtra/config/v1\nbindings:\n  - {name: b, instructions: ['%s']}\n", text)), config.Options{})
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
	const conf = `sources:
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
	path := testenv.Config(t, h, "schema: ctxtra/config/v1\n"+conf)
	c := New(config.Parse([]byte("schema: ctxtra/config/v1\n"+conf), config.Options{}), lock.Parse([]byte("schema: ctxtra/lock/v1\n"+lockFile)), path)

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
	_, err := expand(c, "gone:*.md", Files)
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

	items, err := expand(c, "dotfiles:agents/**/*.md", Files)
	if err != nil || !slices.Equal(refs(items), []string{"dotfiles:agents/a.md", "dotfiles:agents/deep/b.md"}) {
		t.Errorf("files %v, %v", refs(items), err)
	}
	items, err = expand(c, "dotfiles:skills/*", Skills)
	if err != nil || !slices.Equal(refs(items), []string{"dotfiles:skills/review"}) {
		t.Errorf("skills %v, %v", refs(items), err)
	}
	items, err = expand(c, "notes.md", Files)
	if err != nil || len(items) != 1 || items[0].Path != filepath.Join(h, ".config", "ctxtra", "notes.md") {
		t.Errorf("a path relative to the config: %v, %v", refs(items), err)
	}
	if _, err := expand(c, "dotfiles:agents/*.txt", Files); err == nil || !strings.Contains(err.Error(), "matches no file") {
		t.Errorf("no match: %v", err)
	}
	if _, err := expand(c, "nope:*.md", Files); err == nil || !strings.Contains(err.Error(), "no source nope") {
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
	if _, err := expand(c, "dotfiles:**/*.md", Files); err == nil || !strings.Contains(err.Error(), "10,000") {
		t.Errorf("a pattern over a large folder: %v", err)
	}
}

func TestStoredSymlinkLeftOut(t *testing.T) {
	conf := "sources: {team: {repo: 'https://github.com/acme/team.git', branch: main}}\n"
	lockFile := fmt.Sprintf("sources:\n  team: {repo: 'https://github.com/acme/team.git', branch: main, commit: '%s'}\n", commitA)
	h := testenv.Home(t)
	gen := source.GenDir("https://github.com/acme/team.git", commitA)
	testenv.Write(t, filepath.Join(h, "secret.md"), "secret")
	testenv.Write(t, filepath.Join(gen, "a.md"), "a")
	if err := os.Symlink(filepath.Join(h, "secret.md"), filepath.Join(gen, "b.md")); err != nil {
		t.Fatal(err)
	}
	path := testenv.Config(t, h, "schema: ctxtra/config/v1\n"+conf)
	c := New(config.Parse([]byte("schema: ctxtra/config/v1\n"+conf), config.Options{}), lock.Parse([]byte("schema: ctxtra/lock/v1\n"+lockFile)), path)
	items, err := expand(c, "team:*.md", Files)
	if err != nil || !slices.Equal(refs(items), []string{"team:a.md"}) {
		t.Errorf("files %v, %v", refs(items), err)
	}
	if _, err := expand(c, "team:b.md", Files); err == nil {
		t.Error("a symlink in a repo source is read")
	}
}

func TestStoreReadOnlyBySourcePath(t *testing.T) {
	conf := "sources: {team: {repo: 'https://github.com/acme/team.git', branch: main}}\n"
	lockFile := fmt.Sprintf("sources:\n  team: {repo: 'https://github.com/acme/team.git', branch: main, commit: '%s'}\n", commitA)
	h := testenv.Home(t)
	gen := source.GenDir("https://github.com/acme/team.git", commitA)
	testenv.Write(t, filepath.Join(gen, "a.md"), "a")
	path := testenv.Config(t, h, "schema: ctxtra/config/v1\n"+conf)
	c := New(config.Parse([]byte("schema: ctxtra/config/v1\n"+conf), config.Options{}), lock.Parse([]byte("schema: ctxtra/lock/v1\n"+lockFile)), path)
	items, err := expand(c, filepath.Join(gen, "a.md"), Files)
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
	items, err := expand(c, "~/notes/*.md", Files)
	if err != nil || len(items) != 1 || items[0].Path != filepath.Join(home, "notes", "x.md") {
		t.Fatalf("items %v, %v", refs(items), err)
	}
	if ok, _ := doublestar.Match(c.DiskPattern("~/notes/x*"), filepath.ToSlash(items[0].Path)); !ok {
		t.Errorf("pattern %s doesn't match %s", c.DiskPattern("~/notes/x*"), items[0].Path)
	}
}

// expand is Paths of the entry text, with its errors joined.
func expand(c *Catalog, text string, kind Kind) ([]Item, error) {
	items, errs := c.Paths(entry(text), kind)
	return items, errors.Join(errs...)
}

// A local server's ~/ program is in the home folder, for the config's
// servers and for a manifest's.
func TestHomeProgram(t *testing.T) {
	c, h := catalog(t, `sources: {tools: {dir: ~/tools}}
mcp-servers:
  home: {command: ['~/bin/mcp', --stdio]}
  name: {command: [mcp]}
`, "", func(h string) {
		testenv.Write(t, filepath.Join(h, "tools", "ctxtra-source.yaml"), "schema: ctxtra/source/v1\nmcp-servers:\n  build: {command: ['~/bin/build', --all]}\n")
	})
	want := map[string][]string{
		"home":        {filepath.Join(h, "bin", "mcp"), "--stdio"},
		"name":        {"mcp"},
		"tools:build": {filepath.Join(h, "bin", "build"), "--all"},
	}
	for ref, command := range want {
		s, ok := c.Server(ref)
		if !ok || !slices.Equal(s.Command, command) {
			t.Errorf("%s: %v %v, want %v", ref, ok, s.Command, command)
		}
	}
	if raw := c.conf.MCPServers[0].Command[0]; raw != "~/bin/mcp" {
		t.Errorf("the config's model changed: %s", raw)
	}
}

// Manifests read within a session's limit read no more than it has left:
// one that doesn't fit is neither read nor kept, and one read before
// counts again in the next session.
func TestManifestWithinLimit(t *testing.T) {
	const n, size = 36, 512 << 10
	var conf strings.Builder
	conf.WriteString("sources:\n")
	for i := range n {
		fmt.Fprintf(&conf, "  s%02d: {dir: ~/s%02d}\n", i, i)
	}
	c, _ := catalog(t, conf.String(), "", func(h string) {
		for i := range n {
			testenv.Write(t, filepath.Join(h, fmt.Sprintf("s%02d", i), "ctxtra-source.yaml"),
				"schema: ctxtra/source/v1\n# "+strings.Repeat("p", size)+"\nmcp-servers:\n  build: {command: [build-mcp]}\n")
		}
	})
	const total = 16 << 20
	rd := fsutil.NewReader(config.MaxFileBytes, total)
	read, refused := 0, 0
	for _, s := range c.Sources() {
		m, _, err := c.ManifestWithin(s, rd)
		switch {
		case err != nil && errors.Is(err, fsutil.ErrTotalLimit) && !s.manifestRead:
			refused++
		case err == nil && m != nil:
			read += s.manifestSize
		default:
			t.Fatalf("%s: %v %v", s.Name, m, err)
		}
	}
	if read > total || refused == 0 {
		t.Errorf("read %d bytes of manifests, %d refused", read, refused)
	}
	rd = fsutil.NewReader(config.MaxFileBytes, size)
	first := c.Sources()[0]
	if _, _, err := c.ManifestWithin(first, rd); !errors.Is(err, fsutil.ErrTotalLimit) {
		t.Errorf("a manifest read before counts again: %v", err)
	}
}

// Waits that end well count against the budget too: each mark of the store
// gets what the earlier ones left, and after one fails, the rest are
// refused without waiting.
func TestStoreWaitBudget(t *testing.T) {
	var waits []time.Duration
	old := touch
	t.Cleanup(func() { touch = old })
	touch = func(dir string, wait time.Duration) error {
		waits = append(waits, wait)
		if wait < 450*time.Millisecond {
			time.Sleep(wait)
			return fsutil.ErrLockTimeout
		}
		time.Sleep(450 * time.Millisecond)
		return nil
	}
	var conf, lockFile strings.Builder
	conf.WriteString("sources:\n")
	lockFile.WriteString("sources:\n")
	names := []string{"a", "b", "c", "d"}
	for _, n := range names {
		fmt.Fprintf(&conf, "  %s: {repo: 'https://h/%s.git', branch: main}\n", n, n)
		fmt.Fprintf(&lockFile, "  %s: {repo: 'https://h/%s.git', branch: main, commit: '%s'}\n", n, n, strings.Repeat(n, 40))
	}
	c, _ := catalog(t, conf.String(), lockFile.String(), func(string) {
		for _, n := range names {
			testenv.Write(t, filepath.Join(source.GenDir("https://h/"+n+".git", strings.Repeat(n, 40)), "x.md"), "x")
		}
	})
	start := time.Now()
	var failed []string
	for _, n := range names {
		if _, err := expand(c, n+":x.md", Files); err != nil {
			failed = append(failed, n)
		}
	}
	if d := time.Since(start); d > 1300*time.Millisecond {
		t.Errorf("took %v, waits %v", d, waits)
	}
	if len(waits) != 3 || waits[0] != source.TouchWait || waits[1] >= waits[0] || waits[2] >= waits[1] {
		t.Errorf("waits %v", waits)
	}
	if !slices.Equal(failed, []string{"c", "d"}) {
		t.Errorf("failed %v", failed)
	}
	// A commit marked once isn't marked again.
	if _, err := expand(c, "a:x.md", Files); err != nil || len(waits) != 3 {
		t.Errorf("again: %v, waits %v", err, waits)
	}
}
