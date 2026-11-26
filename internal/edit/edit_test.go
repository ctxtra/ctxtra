package edit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

const (
	commitA = "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"
	commitB = "4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e"
	commitC = "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432"
)

var opts = config.Options{Agents: []string{"claude-code", "codex", "cursor"}}

// files are a config and its lock in a new folder, with the given
// content; "" leaves a file out.
func files(t *testing.T, config, lockFile string) Files {
	t.Helper()
	dir := t.TempDir()
	f := Files{
		Config:  filepath.Join(dir, "config", "config.yaml"),
		Mutex:   filepath.Join(dir, "state", "config.lock"),
		Backups: filepath.Join(dir, "state", "backups"),
		Options: opts,
	}
	write := func(path, data string) {
		if data == "" {
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(f.Config, config)
	write(lock.Path(f.Config), lockFile)
	return f
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}

func node(t *testing.T, s string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(s), &n); err != nil {
		t.Fatal(err)
	}
	return n.Content[0]
}

func entry(name, repo, commit string) *lock.Entry {
	return &lock.Entry{Name: name, Repo: repo, Ref: config.Ref{Kind: config.Branch, Name: "main"}, Commit: commit}
}

func source(repo string) string { return fmt.Sprintf("{repo: '%s', branch: main}", repo) }

// state is the config and the lock as parsed; it fails on errors.
func state(t *testing.T, f Files) (*config.Config, *lock.Lock) {
	t.Helper()
	c := config.Parse([]byte(read(t, f.Config)), opts)
	l := &lock.Lock{}
	if data := read(t, lock.Path(f.Config)); data != "" {
		l = lock.Parse([]byte(data))
	}
	if len(c.Errors) > 0 || len(l.Errors) > 0 {
		t.Fatalf("config %v, lock %v", c.Errors, l.Errors)
	}
	return c, l
}

const (
	twoSources = config.NewFile + "sources:\n" +
		"  obs-team:\n    repo: https://github.com/acme/observability-ctx.git\n    branch: main\n" +
		"  iam-team:\n    repo: git@github.com:acme/iam-ctx.git\n    branch: main\n"
	twoEntries = "schema: ctxtra/lock/v1\nsources:\n" +
		"  iam-team:\n    repo: git@github.com:acme/iam-ctx.git\n    branch: main\n    commit: " + commitA + "\n" +
		"  obs-team:\n    repo: https://github.com/acme/observability-ctx.git\n    branch: main\n    commit: " + commitA + "\n"
)

func TestWritingTheLock(t *testing.T) {
	t.Run("two adds at once", func(t *testing.T) {
		f := files(t, "", "")
		var wg sync.WaitGroup
		names := []string{"a", "b", "c", "d", "e", "f"}
		for _, name := range names {
			wg.Go(func() {
				repo := "https://github.com/acme/" + name + ".git"
				if _, err := f.AddSource(name, node(t, source(repo)), entry(name, repo, commitA)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		c, l := state(t, f)
		for _, name := range names {
			if c.Source(name) == nil || l.Entry(name) == nil {
				t.Errorf("source %s: in the config %v, in the lock %v", name, c.Source(name) != nil, l.Entry(name) != nil)
			} else if ch := lock.Compare(l.Entry(name), c.Source(name)); !ch.Same() {
				t.Errorf("source %s isn't active: %+v", name, ch)
			}
		}
	})
	t.Run("two updates at once", func(t *testing.T) {
		f := files(t, twoSources, twoEntries)
		_, l := state(t, f)
		var wg sync.WaitGroup
		for _, name := range []string{"iam-team", "obs-team"} {
			was := l.Entry(name)
			wg.Go(func() {
				next := *was
				next.Commit = commitB
				if err := f.Accept(&next, was); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		_, l = state(t, f)
		if l.Entry("iam-team").Commit != commitB || l.Entry("obs-team").Commit != commitB {
			t.Errorf("lock\n%s", read(t, lock.Path(f.Config)))
		}
	})
	t.Run("the same source accepted twice", func(t *testing.T) {
		f := files(t, twoSources, twoEntries)
		_, l := state(t, f)
		was := l.Entry("iam-team")
		first, second := *was, *was
		first.Commit, second.Commit = commitB, commitC
		if err := f.Accept(&first, was); err != nil {
			t.Fatal(err)
		}
		before := read(t, lock.Path(f.Config))
		err := f.Accept(&second, was)
		if !errors.Is(err, ErrLockChanged) || !strings.Contains(err.Error(), "the lock changed during the review") {
			t.Errorf("err %v", err)
		}
		if read(t, lock.Path(f.Config)) != before {
			t.Error("the lock changed")
		}
	})
	t.Run("interrupted add", func(t *testing.T) {
		// The lock has the entry, the config doesn't: the add stopped in
		// between.
		f := files(t, config.NewFile, "schema: ctxtra/lock/v1\nsources:\n  a:\n    repo: https://x.example/a.git\n    branch: main\n    commit: "+commitA+"\n")
		if _, err := f.AddSource("a", node(t, source("https://x.example/a.git")), entry("a", "https://x.example/a.git", commitB)); err != nil {
			t.Fatal(err)
		}
		c, l := state(t, f)
		if c.Source("a") == nil || len(l.Entries) != 1 || l.Entry("a").Commit != commitB {
			t.Errorf("config\n%s\nlock\n%s", read(t, f.Config), read(t, lock.Path(f.Config)))
		}
	})
	t.Run("the lock is written before the config", func(t *testing.T) {
		f := files(t, config.NewFile, "")
		orig := writeFile
		defer func() { writeFile = orig }()
		var order []string
		writeFile = func(path string, data []byte, backups string, dry bool) (string, bool, error) {
			order = append(order, filepath.Base(path))
			if filepath.Base(path) == "config.yaml" {
				return path, false, errors.New("disk full")
			}
			return orig(path, data, backups, dry)
		}
		_, err := f.AddSource("a", node(t, source("https://x.example/a.git")), entry("a", "https://x.example/a.git", commitA))
		if err == nil || !slices.Equal(order, []string{"ctxtra.lock", "config.yaml"}) {
			t.Errorf("err %v, order %v", err, order)
		}
		// The crash's state: an unused lock entry, which a session ignores.
		if read(t, f.Config) != config.NewFile || !strings.Contains(read(t, lock.Path(f.Config)), "  a:\n") {
			t.Errorf("config\n%s\nlock\n%s", read(t, f.Config), read(t, lock.Path(f.Config)))
		}
	})
	t.Run("a config symlinked from dotfiles", func(t *testing.T) {
		f := files(t, "", "")
		dotfiles := filepath.Join(t.TempDir(), "dotfiles", "ctxtra")
		if err := os.MkdirAll(dotfiles, 0o755); err != nil {
			t.Fatal(err)
		}
		real := filepath.Join(dotfiles, "config.yaml")
		if err := os.WriteFile(real, []byte(twoSources), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(f.Config), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, f.Config); err != nil {
			t.Fatal(err)
		}
		if err := f.Accept(entry("iam-team", "git@github.com:acme/iam-ctx.git", commitB), nil); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(read(t, filepath.Join(dotfiles, "ctxtra.lock")), commitB) {
			t.Error("the lock isn't next to the real file")
		}
		if _, err := f.Add(config.Bindings, "me", node(t, "{skills: ['iam-team:skills/*']}"), config.Place{}); err != nil {
			t.Fatal(err)
		}
		if st, err := os.Lstat(f.Config); err != nil || st.Mode()&os.ModeSymlink == 0 {
			t.Error("the config is no longer a symlink")
		}
		if !strings.Contains(read(t, real), "name: me") {
			t.Error("the real file didn't change")
		}
	})
}

func TestTheLocksForm(t *testing.T) {
	t.Run("a removed source", func(t *testing.T) {
		old := "  old:\n    repo: https://x.example/old.git\n    branch: main\n    commit: " + commitA + "\n"
		f := files(t, twoSources, twoEntries+old)
		_, l := state(t, f)
		was := l.Entry("iam-team")
		next := *was
		next.Commit = commitB
		if err := f.Accept(&next, was); err != nil {
			t.Fatal(err)
		}
		want := "schema: ctxtra/lock/v1\nsources:\n" +
			"  iam-team:\n    repo: git@github.com:acme/iam-ctx.git\n    branch: main\n    commit: " + commitB + "\n" +
			"  obs-team:\n    repo: https://github.com/acme/observability-ctx.git\n    branch: main\n    commit: " + commitA + "\n"
		if got := read(t, lock.Path(f.Config)); got != want {
			t.Errorf("got\n%s\nwant\n%s", got, want)
		}
	})
	t.Run("removing a source removes its entry", func(t *testing.T) {
		f := files(t, twoSources, twoEntries)
		if err := f.Remove(config.Sources, "obs-team"); err != nil {
			t.Fatal(err)
		}
		c, l := state(t, f)
		if c.Source("obs-team") != nil || l.Entry("obs-team") != nil || l.Entry("iam-team") == nil {
			t.Errorf("config\n%s\nlock\n%s", read(t, f.Config), read(t, lock.Path(f.Config)))
		}
	})
	t.Run("a binding leaves the lock alone", func(t *testing.T) {
		old := "  old:\n    repo: https://x.example/old.git\n    branch: main\n    commit: " + commitA + "\n"
		f := files(t, twoSources, twoEntries+old)
		if _, err := f.Add(config.Bindings, "me", node(t, "{skills: ['iam-team:skills/*']}"), config.Place{}); err != nil {
			t.Fatal(err)
		}
		if read(t, lock.Path(f.Config)) != twoEntries+old {
			t.Error("the lock changed")
		}
	})
	t.Run("a dir source has no entry", func(t *testing.T) {
		f := files(t, "", "")
		if _, err := f.AddSource("dotfiles", node(t, "{dir: ~/dev/dotfiles}"), nil); err != nil {
			t.Fatal(err)
		}
		if read(t, lock.Path(f.Config)) != "" {
			t.Error("a lock was written")
		}
	})
}

func TestWritingTheConfig(t *testing.T) {
	t.Run("two commands at once", func(t *testing.T) {
		f := files(t, config.NewFile, "")
		stop := make(chan struct{})
		var reads sync.WaitGroup
		reads.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := yamlx.Parse([]byte(read(t, f.Config)), yamlx.ConfigKind); err != nil {
					t.Errorf("a session read half a config: %v", err)
					return
				}
			}
		})
		var wg sync.WaitGroup
		names := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
		for _, name := range names {
			wg.Go(func() {
				if _, err := f.Add(config.Bindings, name, node(t, "{skills: [x]}"), config.Place{}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		close(stop)
		reads.Wait()
		c, _ := state(t, f)
		if len(c.Bindings) != len(names) {
			t.Errorf("config\n%s", read(t, f.Config))
		}
	})
	t.Run("a write that would add a mistake changes nothing", func(t *testing.T) {
		f := files(t, twoSources, twoEntries)
		_, err := f.Add(config.Bindings, "me", node(t, "{agents: [nobody], skills: [x]}"), config.Place{})
		if err == nil || !strings.Contains(err.Error(), "nothing changed") || !strings.Contains(err.Error(), "unknown agent") {
			t.Errorf("err %v", err)
		}
		if read(t, f.Config) != twoSources {
			t.Error("the config changed")
		}
	})
	t.Run("a name that is taken changes nothing", func(t *testing.T) {
		f := files(t, twoSources, twoEntries)
		_, err := f.AddSource("iam-team", node(t, source("https://x.example/other.git")), entry("iam-team", "https://x.example/other.git", commitB))
		var exists *config.ExistsError
		if !errors.As(err, &exists) || read(t, f.Config) != twoSources || read(t, lock.Path(f.Config)) != twoEntries {
			t.Errorf("err %v", err)
		}
	})
	t.Run("a README run twice", func(t *testing.T) {
		f := files(t, twoSources, twoEntries)
		added, err := f.AddSource("iam-team", node(t, source("git@github.com:acme/iam-ctx.git")), entry("iam-team", "git@github.com:acme/iam-ctx.git", commitB))
		if err != nil || added || read(t, lock.Path(f.Config)) != twoEntries {
			t.Errorf("added %v, err %v", added, err)
		}
	})
	t.Run("an unchanged config isn't written", func(t *testing.T) {
		f := files(t, "", "")
		if err := f.Run(func(*Tx) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(f.Config); !errors.Is(err, os.ErrNotExist) {
			t.Error("a config was written")
		}
	})
	t.Run("an error from the caller changes nothing", func(t *testing.T) {
		f := files(t, twoSources, twoEntries)
		err := f.Run(func(tx *Tx) error {
			if _, err := tx.Config.Add(config.Bindings, "me", node(t, "{skills: [x]}"), config.Place{}); err != nil {
				return err
			}
			tx.SetLockEntry(entry("iam-team", "git@github.com:acme/iam-ctx.git", commitB))
			return errors.New("review failed")
		})
		if err == nil || read(t, f.Config) != twoSources || read(t, lock.Path(f.Config)) != twoEntries {
			t.Errorf("err %v", err)
		}
	})
}

func TestLockThatCantBeWritten(t *testing.T) {
	// A lock with invalid YAML or another schema refuses every write, also
	// one that changes only the config.
	for name, broken := range map[string]string{
		"invalid YAML":   "schema: ctxtra/lock/v1\nsources: [\n",
		"another schema": "schema: ctxtra/other\nsources: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := files(t, twoSources, broken)
			err := f.Accept(entry("iam-team", "git@github.com:acme/iam-ctx.git", commitB), nil)
			if err == nil || !strings.Contains(err.Error(), "ctxtra.lock: line ") || !strings.Contains(err.Error(), "nothing changed") {
				t.Errorf("err %v", err)
			}
			_, err = f.Add(config.Bindings, "me", node(t, "{skills: [x]}"), config.Place{})
			if err == nil || !strings.Contains(err.Error(), "fix it or remove it") {
				t.Errorf("config-only edit: %v", err)
			}
			if read(t, lock.Path(f.Config)) != broken || read(t, f.Config) != twoSources {
				t.Error("a file changed")
			}
		})
	}
	t.Run("another source's broken entry", func(t *testing.T) {
		broken := strings.Replace(twoEntries, "commit: "+commitA+"\n  obs-team", "commit: abc\n  obs-team", 1)
		f := files(t, twoSources, broken)
		was := lock.Parse([]byte(broken)).Entry("obs-team")
		err := f.Accept(entry("obs-team", "https://github.com/acme/observability-ctx.git", commitB), was)
		if err == nil || !strings.Contains(err.Error(), "lock entry iam-team") {
			t.Errorf("err %v", err)
		}
		// Its own broken entry is replaced.
		if err := f.Accept(entry("iam-team", "git@github.com:acme/iam-ctx.git", commitB), nil); err != nil {
			t.Error(err)
		}
	})
	t.Run("an entry for a source the config doesn't have", func(t *testing.T) {
		f := files(t, twoSources, twoEntries)
		err := f.Run(func(tx *Tx) error {
			tx.SetLockEntry(entry("gone", "https://x.example/gone.git", commitB))
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "no repo source gone") {
			t.Errorf("err %v", err)
		}
	})
	t.Run("a layout the editor can't change", func(t *testing.T) {
		flow := "{schema: ctxtra/config/v1, sources: {a: {dir: ~/a}}}\n"
		f := files(t, flow, "")
		_, err := f.Add(config.Sources, "b", node(t, "{dir: ~/b}"), config.Place{})
		if err == nil || !strings.Contains(err.Error(), "block style") || read(t, f.Config) != flow {
			t.Errorf("err %v", err)
		}
	})
	t.Run("a config YAML can't read", func(t *testing.T) {
		bad := config.NewFile + "sources: [\n"
		f := files(t, bad, twoEntries)
		_, err := f.Add(config.Bindings, "me", node(t, "{skills: [x]}"), config.Place{})
		if err == nil || !strings.Contains(err.Error(), "fix it first") || read(t, f.Config) != bad {
			t.Errorf("err %v", err)
		}
	})
}

// A commit that fails after the write puts both files back as they were:
// a config that existed, and a lock that didn't.
func TestCommitThatFailsRestores(t *testing.T) {
	const before = "schema: ctxtra/config/v1\n# mine\n"
	f := files(t, before, "")
	failed := errors.New("the credentials can't be saved")
	err := f.RunCommit(func(tx *Tx) error {
		if _, err := tx.Config.Add(config.Sources, "team", node(t, source("https://h/team")), config.Place{}); err != nil {
			return err
		}
		tx.SetLockEntry(entry("team", "https://h/team", strings.Repeat("a", 40)))
		return nil
	}, func(write func() error) error {
		if err := write(); err != nil {
			return err
		}
		if !strings.Contains(read(t, f.Config), "team") || read(t, lock.Path(f.Config)) == "" {
			t.Error("the write didn't happen before the commit failed")
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("error %v", err)
	}
	if got := read(t, f.Config); got != before {
		t.Errorf("config %q", got)
	}
	if _, err := os.Stat(lock.Path(f.Config)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the lock file stays: %v", err)
	}
}
