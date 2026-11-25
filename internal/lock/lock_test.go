package lock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/cfg"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

const (
	commitA = "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"
	commitB = "4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e"
)

func config(t *testing.T, body string) *cfg.Config {
	t.Helper()
	c := cfg.Parse([]byte("schema: ctxtra/config/v1\n"+body), cfg.Options{})
	if len(c.Errors) > 0 {
		t.Fatalf("config: %v", c.Errors)
	}
	return c
}

func TestDemoLock(t *testing.T) {
	data, err := os.ReadFile("testdata/demo.lock")
	if err != nil {
		t.Fatal(err)
	}
	l := Parse(data)
	if len(l.Errors) > 0 || !l.Readable() {
		t.Fatalf("errors %v", l.Errors)
	}
	if len(l.Entries) != 6 {
		t.Fatalf("entries %d", len(l.Entries))
	}
	if e := l.Entry("billing-ctx"); e.Repo != "git@github.com:acme/agent-context.git" || e.Ref != (cfg.Ref{Kind: cfg.Branch, Name: "main"}) ||
		e.Subdir != "teams/billing" || e.Commit != "5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f" {
		t.Errorf("billing-ctx %+v", e)
	}
	if l.Entry("dotfiles") != nil {
		t.Error("a dir source has an entry")
	}

	t.Run("written in the same form", func(t *testing.T) {
		demo, err := os.ReadFile("../cfg/testdata/demo.yaml")
		if err != nil {
			t.Fatal(err)
		}
		c := cfg.Parse(demo, cfg.Options{Agents: []string{"claude-code", "codex", "cursor"}})
		got, err := Format(l.Entries, c)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(data) {
			t.Errorf("Format:\n%s\nwant:\n%s", got, data)
		}
	})
	t.Run("every entry matches the demo config", func(t *testing.T) {
		demo, _ := os.ReadFile("../cfg/testdata/demo.yaml")
		c := cfg.Parse(demo, cfg.Options{})
		for _, e := range l.Entries {
			if ch := Compare(e, c.Source(e.Name)); !ch.Same() {
				t.Errorf("%s: %+v", e.Name, ch)
			}
		}
	})
}

func TestARemovedSource(t *testing.T) {
	c := config(t, "sources:\n  obs-team: {repo: 'https://github.com/acme/obs.git', tag: v1.4.0}\n"+
		"  iam-team: {repo: 'git@github.com:acme/iam-ctx.git', branch: main}\n  dotfiles: {dir: ~/dev/dotfiles}\n")
	l := Parse([]byte("schema: ctxtra/lock/v1\nsources:\n" +
		"  old: {repo: 'git@github.com:acme/old.git', branch: main, commit: " + commitA + "}\n" +
		"  obs-team: {repo: 'https://github.com/acme/obs.git', tag: v1.4.0, commit: " + commitB + "}\n" +
		"  dotfiles: {repo: 'git@github.com:me/dotfiles.git', branch: main, commit: " + commitB + "}\n"))
	if len(l.Errors) > 0 {
		t.Fatal(l.Errors)
	}
	entries := append(l.Entries, &Entry{Name: "iam-team", Repo: "git@github.com:acme/iam-ctx.git", Ref: cfg.Ref{Kind: cfg.Branch, Name: "main"}, Commit: commitA})
	got, err := Format(entries, c)
	if err != nil {
		t.Fatal(err)
	}
	want := "schema: ctxtra/lock/v1\nsources:\n" +
		"  iam-team:\n    repo: git@github.com:acme/iam-ctx.git\n    branch: main\n    commit: " + commitA + "\n" +
		"  obs-team:\n    repo: https://github.com/acme/obs.git\n    tag: v1.4.0\n    commit: " + commitB + "\n"
	if string(got) != want {
		t.Errorf("Format:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormat(t *testing.T) {
	c := config(t, "sources:\n  s: {repo: 'git@github.com:a/b.git', tag: '1.10'}\n")
	t.Run("text that YAML would read as a number", func(t *testing.T) {
		e := &Entry{Name: "s", Repo: "git@github.com:a/b.git", Ref: cfg.Ref{Kind: cfg.Tag, Name: "1.10"}, Commit: commitA}
		data, err := Format([]*Entry{e}, c)
		if err != nil {
			t.Fatal(err)
		}
		l := Parse(data)
		if len(l.Errors) > 0 || l.Entry("s").Ref.Name != "1.10" {
			t.Errorf("read back %s: %v", data, l.Errors)
		}
	})
	for _, tt := range []struct {
		name  string
		entry Entry
		want  string
	}{
		{"a short commit", Entry{Name: "s", Repo: "x", Ref: cfg.Ref{Kind: cfg.Tag, Name: "v1"}, Commit: "1a2b3c"}, "not a full commit ID"},
		{"no ref", Entry{Name: "s", Repo: "x", Commit: commitA}, "branch, tag or rev is missing"},
		{"no repo", Entry{Name: "s", Ref: cfg.Ref{Kind: cfg.Tag, Name: "v1"}, Commit: commitA}, "repo is missing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Format([]*Entry{&tt.entry}, c); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Format: %v, want %q", err, tt.want)
			}
		})
	}
	t.Run("no sources", func(t *testing.T) {
		data, err := Format(nil, c)
		if err != nil || string(data) != "schema: ctxtra/lock/v1\n" {
			t.Errorf("Format = %q, %v", data, err)
		}
	})
}

func TestParse(t *testing.T) {
	t.Run("a broken lock file", func(t *testing.T) {
		l := Parse([]byte("schema: ctxtra/lock/v1\nsources: [\n"))
		if l.Readable() || len(l.Entries) > 0 || len(l.Errors) != 1 || l.Errors[0].Out() != yamlx.OutFile {
			t.Errorf("lock %+v", l)
		}
	})
	t.Run("a config saved as the lock", func(t *testing.T) {
		l := Parse([]byte("schema: ctxtra/config/v1\n"))
		if l.Readable() || !strings.Contains(l.Errors[0].Error(), "this is a config, not a lock file") {
			t.Errorf("errors %v", l.Errors)
		}
	})
	for _, tt := range []struct{ name, entry, want string }{
		{"a YAML tag", "{repo: !x y, branch: main, commit: " + commitA + "}", "quote"},
		{"a short commit", "{repo: x, branch: main, commit: 1a2b3c}", "commit 1a2b3c is not a full commit ID"},
		{"no commit", "{repo: x, branch: main}", "commit is missing"},
		{"branch and tag", "{repo: x, branch: main, tag: v1, commit: " + commitA + "}", "one of branch, tag and rev"},
		{"an unknown key", "{repo: x, branch: main, commit: " + commitA + ", accepted: today}", `unknown key "accepted"`},
		{"a tag that YAML reads as a number", "{repo: x, tag: 1.10, commit: " + commitA + "}", "quote it: tag: '1.10'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := Parse([]byte("schema: ctxtra/lock/v1\nsources:\n  bad: " + tt.entry + "\n  ok: {repo: x, branch: main, commit: " + commitA + "}\n"))
			if len(l.Errors) != 1 || !strings.Contains(l.Errors[0].Error(), tt.want) || !strings.Contains(l.Errors[0].Error(), "lock entry bad is left out") {
				t.Fatalf("errors %v, want %q", l.Errors, tt.want)
			}
			if l.Entry("bad") != nil || l.Entry("ok") == nil || !l.Readable() {
				t.Error("the broken entry is used, or the other one isn't")
			}
		})
	}
}

func TestCompare(t *testing.T) {
	locked := &Entry{Name: "iam-team", Repo: "git@github.com:acme/iam-ctx.git", Ref: cfg.Ref{Kind: cfg.Branch, Name: "main"},
		Subdir: "teams/billing", Commit: commitA}
	tests := []struct {
		name   string
		source string
		want   Changes
	}{
		{"the same source", "{repo: 'git@github.com:acme/iam-ctx.git', branch: main, subdir: teams/billing}", Changes{}},
		{"from ssh to https", "{repo: 'https://github.com/acme/iam-ctx', branch: main, subdir: teams/billing}", Changes{}},
		{"another form of the folder", "{repo: 'git@github.com:acme/iam-ctx.git', branch: main, subdir: ./teams/billing/}", Changes{}},
		{"the user switches branch", "{repo: 'git@github.com:acme/iam-ctx.git', branch: next, subdir: teams/billing}", Changes{Ref: true}},
		{"a tag instead of the branch", "{repo: 'git@github.com:acme/iam-ctx.git', tag: main, subdir: teams/billing}", Changes{Ref: true}},
		{"the user changes the address", "{repo: 'git@github.com:other/iam-ctx.git', branch: main, subdir: teams/billing}", Changes{Repo: true}},
		{"subdir changed", "{repo: 'git@github.com:acme/iam-ctx.git', branch: main, subdir: teams}", Changes{Subdir: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := config(t, "sources:\n  iam-team: "+tt.source+"\n")
			got := Compare(locked, c.Source("iam-team"))
			if got != tt.want {
				t.Errorf("Compare = %+v, want %+v", got, tt.want)
			}
			if got.NewSource() != (tt.want.Repo || tt.want.Subdir) {
				t.Errorf("NewSource = %v", got.NewSource())
			}
		})
	}
}

func TestPath(t *testing.T) {
	t.Run("a config symlinked from dotfiles", func(t *testing.T) {
		dir := t.TempDir()
		dotfiles := filepath.Join(dir, "dotfiles", "ctxtra")
		home := filepath.Join(dir, "config", "ctxtra")
		for _, d := range []string{dotfiles, home} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		real := filepath.Join(dotfiles, "config.yaml")
		if err := os.WriteFile(real, []byte("schema: ctxtra/config/v1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(home, "config.yaml")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		want, _ := filepath.EvalSymlinks(dotfiles)
		if got := Path(link); got != filepath.Join(want, FileName) {
			t.Errorf("Path = %s, want next to %s", got, real)
		}
	})
	t.Run("no config yet", func(t *testing.T) {
		if got := Path("/nowhere/ctxtra/config.yaml"); got != "/nowhere/ctxtra/ctxtra.lock" {
			t.Errorf("Path = %s", got)
		}
	})
}
