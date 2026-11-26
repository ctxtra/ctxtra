package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Where a source's files come from, and what a session gets when they can't
// be read.
func TestSources(t *testing.T) {
	const iamTeam = "sources:\n  iam-team: {repo: 'git@github.com:acme/iam-ctx.git', branch: main}\n"
	iamFiles := map[string]string{"instructions/go.md": "GO", "instructions/rust.md": "RUST"}

	t.Run("sessions read the locked commit", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("a"), iamFiles)
		h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("b"), map[string]string{"instructions/go.md": "NEWER"})
		env := h.load(iamTeam+"bindings:\n  - {name: b, instructions: ['iam-team:instructions/go.md']}\n", entry)
		res := start(env, h.dir, "")
		noErrors(t, res)
		if len(res.Files) != 1 || res.Files[0].Content != "GO" || res.Files[0].Ref != "iam-team:instructions/go.md" {
			t.Errorf("files %+v", res.Files)
		}
	})

	t.Run("a tag and a rev read their locked commits", func(t *testing.T) {
		h := newHome(t)
		tag := h.accept("obs-team", "https://github.com/acme/obs.git", "tag: v1.4.0", "", commit("a"), map[string]string{"x.md": "TAG"})
		rev := h.accept("security", "git@github.com:acme/sec.git", "rev: "+commit("b"), "", commit("b"), map[string]string{"y.md": "REV"})
		env := h.load("sources:\n  obs-team: {repo: 'https://github.com/acme/obs.git', tag: v1.4.0}\n"+
			"  security: {repo: 'git@github.com:acme/sec.git', rev: "+commit("b")+"}\n"+
			"bindings:\n  - {name: b, instructions: ['obs-team:x.md', 'security:y.md']}\n", tag, rev)
		res := start(env, h.dir, "")
		noErrors(t, res)
		if len(res.Files) != 2 || res.Files[0].Content != "TAG" || res.Files[1].Content != "REV" {
			t.Errorf("files %+v", res.Files)
		}
	})

	t.Run("two folders of one commit", func(t *testing.T) {
		h := newHome(t)
		files := map[string]string{"teams/billing/a.md": "BILLING", "teams/iam/a.md": "IAM"}
		b := h.accept("billing", "https://h/ctx.git", "branch: main", "teams/billing", commit("a"), files)
		i := h.accept("iam", "https://h/ctx.git", "branch: main", "teams/iam", commit("a"), files)
		env := h.load("sources:\n  billing: {repo: 'https://h/ctx.git', branch: main, subdir: teams/billing}\n"+
			"  iam: {repo: 'https://h/ctx.git', branch: main, subdir: teams/iam}\n"+
			"bindings:\n  - {name: b, instructions: ['billing:a.md', 'iam:a.md']}\n", b, i)
		res := start(env, h.dir, "")
		noErrors(t, res)
		if len(res.Files) != 2 || res.Files[0].Content != "BILLING" || res.Files[1].Content != "IAM" {
			t.Errorf("files %+v", res.Files)
		}
	})

	t.Run("a raw file from a source", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("a"), iamFiles)
		env := h.load(iamTeam+"bindings:\n  - {name: b, instructions: ['iam-team:instructions/*.md']}\n", entry)
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "go.md", "rust.md")
	})

	t.Run("one team's folder of a shared repository", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("billing-ctx", "git@github.com:acme/agent-context.git", "branch: main", "teams/billing", commit("a"), map[string]string{
			"teams/billing/skills/pay/SKILL.md": "---\nname: pay\ndescription: Pay.\n---\nbody",
			"teams/iam/skills/login/SKILL.md":   "---\nname: login\ndescription: Log in.\n---\nbody",
		})
		env := h.load("sources:\n  billing-ctx: {repo: 'git@github.com:acme/agent-context.git', branch: main, subdir: teams/billing}\n"+
			"bindings:\n  - {name: b, skills: ['billing-ctx:skills/*']}\n", entry)
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "skills", skillNames(res), "pay")
		if res.Skills[0].SourcePath != "skills/pay/SKILL.md" {
			t.Errorf("source path %q", res.Skills[0].SourcePath)
		}
	})

	t.Run("my own clone", func(t *testing.T) {
		h := newHome(t)
		h.repo("dev/iam-ctx", "")
		h.write("dev/iam-ctx/instructions/go.md", "EDITED")
		entry := h.accept("iam-dev", "~/dev/iam-ctx", "branch: my-change", "", commit("c"), map[string]string{"instructions/go.md": "ACCEPTED"})
		env := h.load("sources:\n  iam-dev: {repo: ~/dev/iam-ctx, branch: my-change}\nbindings:\n  - {name: b, instructions: ['iam-dev:instructions/go.md']}\n", entry)
		res := start(env, h.dir, "")
		noErrors(t, res)
		if len(res.Files) != 1 || res.Files[0].Content != "ACCEPTED" {
			t.Errorf("files %+v", res.Files)
		}
	})

	t.Run("from ssh to https", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("a"), iamFiles)
		env := h.load("sources:\n  iam-team: {repo: 'https://github.com/acme/iam-ctx', branch: main}\nbindings:\n  - {name: b, instructions: ['iam-team:instructions/go.md']}\n", entry)
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "go.md")
	})

	t.Run("the user switches branch", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		entry := h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("a"), iamFiles)
		env := h.load("sources:\n  iam-team: {repo: 'git@github.com:acme/iam-ctx.git', branch: next}\nbindings:\n  - {name: b, instructions: ['iam-team:instructions/go.md', ~/me.md]}\n", entry)
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "me.md")
		oneError(t, res, "source iam-team", "accepted for another branch, tag or rev", "ctxtra source update iam-team")
		eq(t, "unavailable", trace(t, res, "b").Unavailable, "iam-team")
	})

	t.Run("the user changes the address", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("a"), iamFiles)
		env := h.load("sources:\n  iam-team: {repo: 'git@github.com:other/iam-ctx.git', branch: main}\nbindings:\n  - {name: b, instructions: ['iam-team:instructions/go.md']}\n", entry)
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files))
		oneError(t, res, "accepted for another repo", "ctxtra source update iam-team")
	})

	t.Run("subdir changed", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("billing-ctx", "git@github.com:acme/agent-context.git", "branch: main", "teams/billing", commit("a"), nil)
		env := h.load("sources:\n  billing-ctx: {repo: 'git@github.com:acme/agent-context.git', branch: main, subdir: teams}\nbindings:\n  - {name: b, skills: ['billing-ctx:*']}\n", entry)
		oneError(t, start(env, h.dir, ""), "accepted for another subdir", "ctxtra source update billing-ctx")
	})

	t.Run("a config without its lock file", func(t *testing.T) {
		h := newHome(t)
		h.write("dotfiles/agents/a.md", "A")
		env := h.load("sources:\n  iam-team: {repo: 'git@github.com:acme/iam-ctx.git', branch: main}\n  obs-team: {repo: 'https://github.com/acme/obs.git', branch: v1}\n  dotfiles: {dir: ~/dotfiles}\n" +
			"bindings:\n  - {name: b, instructions: ['iam-team:x.md', 'obs-team:y.md', 'dotfiles:agents/*.md']}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "a.md")
		if errs := errorsOf(res, yamlx.Inactive); len(errs) != 2 || len(res.Errors) != 2 {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("a broken lock file", func(t *testing.T) {
		h := newHome(t)
		h.write(".config/ctxtra/ctxtra.lock", "schema: ctxtra/lock/v1\nsources: [\n")
		env := h.load(iamTeam + "bindings:\n  - {name: b, instructions: ['iam-team:instructions/go.md']}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files))
		e := oneError(t, res, "ctxtra.lock")
		if e.Err.Mistake != yamlx.Unreadable {
			t.Errorf("mistake %v", e.Err.Mistake)
		}
	})

	t.Run("a config symlinked from dotfiles", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("a"), iamFiles)
		h.write("dev/dotfiles/ctxtra/config.yaml", "schema: ctxtra/config/v1\n"+iamTeam+"bindings:\n  - {name: b, instructions: ['iam-team:instructions/go.md', notes.md]}\n")
		h.write("dev/dotfiles/ctxtra/ctxtra.lock", "schema: ctxtra/lock/v1\nsources:\n"+entry)
		h.write("dev/dotfiles/ctxtra/notes.md", "NOTES")
		link := h.path(".config/ctxtra/config.yaml")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(h.path("dev/dotfiles/ctxtra/config.yaml"), link); err != nil {
			t.Fatal(err)
		}
		res := start(Load(link, agentNames), h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "go.md", "notes.md")
	})

	t.Run("a new laptop", func(t *testing.T) {
		h := newHome(t)
		entry := "  iam-team:\n    repo: git@github.com:acme/iam-ctx.git\n    branch: main\n    commit: " + commit("a") + "\n"
		env := h.load(iamTeam+"bindings:\n  - {name: b, instructions: ['iam-team:instructions/go.md']}\n", entry)
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files))
		oneError(t, res, "not downloaded", "ctxtra source download")
	})

	t.Run("a live folder not cloned yet", func(t *testing.T) {
		h := newHome(t)
		h.write("agent-context/me.md", "me")
		env := h.load("sources: {dotfiles: {dir: ~/dev/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/*.md', ~/agent-context/me.md]}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "me.md")
		if s := env.Catalog.Source("dotfiles"); s.Why != "~/dev/dotfiles is not on this machine" {
			t.Errorf("why %q", s.Why)
		}
	})

	t.Run("a clone on one machine only", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		entry := h.accept("iam-dev", "~/dev/iam-ctx", "branch: my-change", "", commit("c"), map[string]string{"x.md": "X"})
		env := h.load("sources:\n  iam-dev: {repo: ~/dev/iam-ctx, branch: my-change}\nbindings:\n  - {name: b, instructions: ['iam-dev:x.md', ~/me.md]}\n", entry)
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "me.md")
	})

	t.Run("a repo path that isn't a git repository", func(t *testing.T) {
		h := newHome(t)
		h.mkdir("dev/iam-ctx")
		h.write("dotfiles", "a file")
		env := h.load("sources:\n  iam-dev: {repo: ~/dev/iam-ctx, branch: main}\n  dotfiles: {dir: ~/dotfiles}\n")
		res := start(env, h.dir, "")
		if len(res.Errors) != 2 || !strings.Contains(res.Errors[0].Error(), "is not a git repository") || !strings.Contains(res.Errors[1].Error(), "is not a folder") {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("an edit applies right away", func(t *testing.T) {
		h := newHome(t)
		h.repo("dev/dotfiles", "")
		h.write("dev/dotfiles/agents/a.md", "OLD")
		env := h.load("sources: {dotfiles: {dir: ~/dev/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/a.md']}\n")
		h.write("dev/dotfiles/agents/a.md", "NEW")
		res := start(env, h.dir, "")
		noErrors(t, res)
		if len(res.Files) != 1 || res.Files[0].Content != "NEW" {
			t.Errorf("files %+v", res.Files)
		}
	})

	t.Run("a file in a cloned repository", func(t *testing.T) {
		h := newHome(t)
		h.repo("dev/team-notes", "git@github.com:acme/team-notes.git")
		h.write("dev/team-notes/rules.md", "RULES")
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, instructions: [~/dev/team-notes/rules.md, ~/me.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "me.md")
		if e := oneError(t, res, "rules.md"); e.Err.Mistake != yamlx.UntrustedFile {
			t.Errorf("mistake %v", e.Err.Mistake)
		}
	})

	t.Run("a symlink out of a source", func(t *testing.T) {
		h := newHome(t)
		h.repo("dev/clone", "git@github.com:acme/clone.git")
		h.write("dev/clone/x.md", "THEIRS")
		h.write("dotfiles/agents/a.md", "A")
		symlink(t, h.path("dev/clone/x.md"), h.path("dotfiles/agents/x.md"))
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/*.md']}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "a.md")
		oneError(t, res, "x.md")
	})

	t.Run("a symlink to my own notes", func(t *testing.T) {
		h := newHome(t)
		h.write("notes/x.md", "MINE")
		h.mkdir("dotfiles/agents")
		symlink(t, h.path("notes/x.md"), h.path("dotfiles/agents/x.md"))
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/*.md']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		if len(res.Files) != 1 || res.Files[0].Content != "MINE" {
			t.Errorf("files %+v", res.Files)
		}
	})

	t.Run("a file name with a colon", func(t *testing.T) {
		h := newHome(t)
		h.write(".config/ctxtra/draft:notes.md", "DRAFT")
		h.write("tmp/draft:notes.md", "TMP")
		env := h.load("bindings:\n  - {name: b, instructions: ['./draft:notes.md', '" + h.path("tmp/draft:notes.md") + "']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		if len(res.Files) != 2 || res.Files[0].Content != "DRAFT" || res.Files[1].Content != "TMP" {
			t.Errorf("files %+v", res.Files)
		}
	})

	t.Run("leaving the source", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("a"), iamFiles)
		env := h.load(iamTeam+"bindings:\n  - {name: b, instructions: ['iam-team:../secrets.md', 'iam-team:instructions/go.md']}\n", entry)
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "go.md")
		oneError(t, res, "leaves its source")
	})

	t.Run("unknown source", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, instructions: ['nope:x.md', ~/me.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "me.md")
		oneError(t, res, "binding b", "no source nope")
	})

	t.Run("a file of bindings in a source", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("a"), map[string]string{
			"bindings.yaml": "bindings:\n  - {name: evil, instructions: [~/.ssh/id]}\n",
		})
		env := h.load(iamTeam+"bindings:\n  - {name: me, instructions: ['iam-team:bindings.yaml']}\n", entry)
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "applied", applied(res), "me")
		eq(t, "files", bases(res.Files), "bindings.yaml")
	})
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// Globs in a dir source: a folder the trust rules refuse leaves out only
// what is in it, symlinks to trusted folders are followed, and a pattern is
// read as its clean path.
func TestDirSourceGlobs(t *testing.T) {
	t.Run("an untrusted clone inside a dir source", func(t *testing.T) {
		h := newHome(t)
		h.write("dotfiles/agents/me.md", "ME")
		h.repo("dotfiles/agents/team", "git@github.com:acme/team.git")
		h.write("dotfiles/agents/team/x.md", "THEIRS")
		h.write("dotfiles/agents/zz/last.md", "LAST")
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/**/*.md']}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "me.md", "last.md")
		if e := oneError(t, res, "agents/team"); e.Err.Mistake != yamlx.UntrustedFile || strings.Contains(e.Error(), ".ctxtra-glob") {
			t.Errorf("error %v", e)
		}
	})

	t.Run("a symlink to a trusted folder", func(t *testing.T) {
		h := newHome(t)
		h.write("dotfiles/agents/me.md", "ME")
		h.write("notes/go.md", "GO")
		symlink(t, h.path("notes"), h.path("dotfiles/agents/notes"))
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/**/*.md']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "me.md", "go.md")
	})

	t.Run("a symlink to an untrusted folder", func(t *testing.T) {
		h := newHome(t)
		h.write("dotfiles/agents/me.md", "ME")
		h.repo("dev/clone", "git@github.com:acme/clone.git")
		h.write("dev/clone/x.md", "THEIRS")
		symlink(t, h.path("dev/clone"), h.path("dotfiles/agents/clone"))
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/**/*.md']}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "me.md")
		oneError(t, res, "agents/clone")
	})

	t.Run("a symlink cycle", func(t *testing.T) {
		h := newHome(t)
		h.write("dotfiles/agents/me.md", "ME")
		symlink(t, h.path("dotfiles"), h.path("dotfiles/agents/loop"))
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:**/*.md']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "me.md")
	})

	t.Run("a folder symlink in a repo source isn't followed", func(t *testing.T) {
		h := newHome(t)
		h.write("notes/go.md", "GO")
		entry := h.accept("team", "https://github.com/acme/team.git", "branch: main", "", commit("a"), map[string]string{"agents/me.md": "ME"})
		symlink(t, h.path("notes"), filepath.Join(source.GenDir("https://github.com/acme/team.git", "", commit("a")), "agents", "notes"))
		env := h.load("sources: {team: {repo: 'https://github.com/acme/team.git', branch: main}}\nbindings:\n  - {name: b, instructions: ['team:agents/**/*.md']}\n", entry)
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "me.md")
	})

	t.Run("a pattern with ./ or ..", func(t *testing.T) {
		h := newHome(t)
		h.write("dotfiles/agents/a.md", "A")
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n" +
			"  - {name: dot, instructions: ['dotfiles:./agents/*.md']}\n  - {name: up, instructions: ['dotfiles:agents/../agents/*.md']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "a.md")
		eq(t, "up's files", trace(t, res, "up").Files)
		if len(trace(t, res, "dot").Files) != 1 {
			t.Errorf("dot: %+v", trace(t, res, "dot"))
		}
	})
}

func TestDirSourceGlobLoops(t *testing.T) {
	for _, c := range []struct{ name, link string }{{"a symlink to itself", "loop"}, {"two symlinks to each other", "other"}} {
		t.Run(c.name, func(t *testing.T) {
			h := newHome(t)
			h.write("dotfiles/agents/me.md", "ME")
			h.write("dotfiles/agents/zz/last.md", "LAST")
			symlink(t, c.link, h.path("dotfiles/agents/loop"))
			if c.link == "other" {
				symlink(t, "loop", h.path("dotfiles/agents/other"))
			}
			env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/**/*.md']}\n")
			res := start(env, h.dir, "")
			eq(t, "files", bases(res.Files), "me.md", "last.md")
			if len(res.Errors) == 0 {
				t.Fatal("no error for the loop")
			}
			for _, e := range res.Errors {
				if e.Err.Mistake != yamlx.UnreadableFile || !strings.Contains(e.Error(), "agents/") {
					t.Errorf("error %v", e)
				}
			}
		})
	}
}

func TestDirSourceSkillLoop(t *testing.T) {
	h := newHome(t)
	h.skill("dotfiles/skills/good", "good")
	h.mkdir("dotfiles/skills/bad")
	symlink(t, "SKILL.md", h.path("dotfiles/skills/bad/SKILL.md"))
	env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, skills: ['dotfiles:skills/*']}\n")
	res := start(env, h.dir, "")
	eq(t, "skills", skillNames(res), "good")
	if e := oneError(t, res, "skills/bad/SKILL.md"); e.Err.Mistake != yamlx.UnreadableFile {
		t.Errorf("mistake %v", e.Err.Mistake)
	}
}

// A dir source in a repository with remotes is read live only after
// source trust recorded them, and stops when they change.
func TestDirSourceTrust(t *testing.T) {
	h := newHome(t)
	h.repo("dotfiles", "git@github.com:me/dotfiles.git")
	h.write("dotfiles/agents/me.md", "ME")
	cfg := "sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/*.md']}\n"
	res := start(h.load(cfg), h.dir, "")
	eq(t, "files", bases(res.Files))
	oneError(t, res, "remotes github.com/me/dotfiles", "ctxtra source trust dotfiles")

	if err := state.Trust(paths.Real(h.path("dotfiles")), []string{"github.com/me/dotfiles"}); err != nil {
		t.Fatal(err)
	}
	res = start(h.load(cfg), h.dir, "")
	noErrors(t, res)
	eq(t, "files", bases(res.Files), "me.md")

	testenv.Run(t, h.path("dotfiles"), "git", "remote", "add", "fork", "git@github.com:other/dotfiles.git")
	res = start(h.load(cfg), h.dir, "")
	eq(t, "files", bases(res.Files))
	oneError(t, res, "remotes of ~/dotfiles changed", "github.com/me/dotfiles, github.com/other/dotfiles", "ctxtra source trust dotfiles")
}

// A folder without a remote is read live without trust.
func TestDirSourceWithoutRemote(t *testing.T) {
	h := newHome(t)
	h.repo("dotfiles", "")
	h.write("dotfiles/agents/me.md", "ME")
	res := start(h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:agents/*.md']}\n"), h.dir, "")
	noErrors(t, res)
	eq(t, "files", bases(res.Files), "me.md")
}

// A symlink on the way to a dir source's folder passes the trust check at
// its own location: a link in another repository with a remote keeps the
// source from being read, at the source's folder or above it, even when
// the folder it leads to is trusted, and also when that folder is in the
// same repository. A link outside git doesn't.
func TestDirSourceRoute(t *testing.T) {
	h := newHome(t)
	h.repo("untrusted", "git@github.com:other/untrusted.git")
	h.write("private-notes/sub/rules.md", "PRIVATE")
	h.repo("dotfiles", "git@github.com:me/dotfiles.git")
	h.write("dotfiles/agents/me.md", "ME")
	if err := state.Trust(paths.Real(h.path("dotfiles/agents")), []string{"github.com/me/dotfiles"}); err != nil {
		t.Fatal(err)
	}
	symlink(t, h.path("private-notes"), h.path("untrusted/link"))
	symlink(t, h.path("dotfiles/agents"), h.path("untrusted/to-dotfiles"))
	h.mkdir("links")
	symlink(t, h.path("private-notes"), h.path("links/notes"))
	symlink(t, "agents", h.path("dotfiles/inner"))
	for _, c := range []struct {
		dir, file, repo string
		blocked         bool
	}{
		{"~/untrusted/link", "sub/rules.md", "untrusted", true},
		{"~/untrusted/link/sub", "rules.md", "untrusted", true},
		{"~/untrusted/to-dotfiles", "me.md", "untrusted", true},
		{"~/links/notes", "sub/rules.md", "", false},
		{"~/dotfiles/inner", "me.md", "dotfiles", true},
	} {
		t.Run(c.dir, func(t *testing.T) {
			res := start(h.load("sources: {dot: {dir: "+c.dir+"}}\nbindings:\n  - {name: b, instructions: ['dot:"+c.file+"']}\n"), h.dir, "")
			if !c.blocked {
				noErrors(t, res)
				eq(t, "files", bases(res.Files), filepath.Base(c.file))
				return
			}
			eq(t, "files", bases(res.Files))
			oneError(t, res, c.dir+" leads through a symlink at ~/"+c.repo+"/", "in ~/"+c.repo+", which has the remotes")
		})
	}
}

// A session's total read limit counts the config, the lock and the
// manifests it reads, not only the content files.
func TestTotalLimitCoversConfig(t *testing.T) {
	file := strings.Repeat("x", config.MaxFileBytes-100)
	pad := "# " + strings.Repeat("p", 3<<20) + "\n"
	files := "  - {name: b, instructions: [~/a.md, ~/b.md, ~/c.md, ~/d.md]}\n"
	for _, c := range []struct{ name, conf, lock string }{
		{"nothing else", "bindings:\n" + files, ""},
		{"config", pad + "bindings:\n" + files, ""},
		{"lock", "sources: {team: {repo: 'https://github.com/acme/team.git', branch: main}}\nbindings:\n" + files, pad},
		{"manifest", "sources: {tools: {dir: ~/tools}}\nbindings:\n  - {name: s, mcp-servers: ['tools:*']}\n" + files, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHome(t)
			for _, n := range []string{"a", "b", "c", "d"} {
				h.write(n+".md", file)
			}
			h.write("tools/ctxtra-source.yaml", "schema: ctxtra/source/v1\n"+"# "+strings.Repeat("p", 1<<19)+"\nmcp-servers:\n  build: {command: [build-mcp]}\n")
			if c.lock != "" {
				h.write(".config/ctxtra/ctxtra.lock", "schema: ctxtra/lock/v1\n"+c.lock)
			}
			res := start(h.load(c.conf), h.dir, "")
			var limit []string
			for _, e := range res.Errors {
				if strings.Contains(e.Error(), "total read limit") {
					limit = append(limit, e.Error())
				}
			}
			if c.name == "nothing else" {
				eq(t, "files", bases(res.Files), "a.md", "b.md", "c.md", "d.md")
				noErrors(t, res)
				return
			}
			eq(t, "files", bases(res.Files), "a.md", "b.md", "c.md")
			if len(limit) != 1 || !strings.Contains(limit[0], "d.md") {
				t.Errorf("errors %v", res.Errors)
			}
		})
	}
}

// A file that the session counted without reading it as content, the
// config or a manifest, still delivers its text when a binding names it.
func TestCountedFilesKeepTheirContent(t *testing.T) {
	h := newHome(t)
	h.write("tools/ctxtra-source.yaml", "schema: ctxtra/source/v1\n# MANIFEST_MARKER\nmcp-servers:\n  build: {command: [build-mcp]}\n")
	for name, conf := range map[string]string{
		"the config": "# CONFIG_MARKER\nbindings:\n  - {name: b, instructions: [~/.config/ctxtra/config.yaml]}\n",
		"a manifest, servers first": "sources: {tools: {dir: ~/tools}}\nbindings:\n  - {name: s, mcp-servers: ['tools:build']}\n" +
			"  - {name: b, instructions: ['tools:ctxtra-source.yaml']}\n",
		"a manifest, instruction first": "sources: {tools: {dir: ~/tools}}\nbindings:\n  - {name: b, instructions: ['tools:ctxtra-source.yaml']}\n" +
			"  - {name: s, mcp-servers: ['tools:build']}\n",
	} {
		t.Run(name, func(t *testing.T) {
			res := start(h.load(conf), h.dir, "")
			noErrors(t, res)
			want := "MANIFEST_MARKER"
			if name == "the config" {
				want = "CONFIG_MARKER"
			}
			if len(res.Files) != 1 || !strings.Contains(res.Files[0].Content, want) {
				t.Errorf("files %+v", res.Files)
			}
			if name != "the config" && len(res.MCPServers) != 1 {
				t.Errorf("servers %+v", res.MCPServers)
			}
		})
	}
}

// A link on the way to a dir source passes when its location is in
// another dir source that is trusted on its own: the user trusted that
// folder. Without that source, the same link is refused, and two sources
// can't make each other's links pass.
func TestDirSourceRouteThroughTrustedSource(t *testing.T) {
	h := newHome(t)
	h.repo("dotfiles", "git@github.com:me/dotfiles.git")
	h.write("notes/x.md", "NOTES")
	symlink(t, h.path("notes"), h.path("dotfiles/notes"))
	if err := state.Trust(paths.Real(h.path("dotfiles")), []string{"github.com/me/dotfiles"}); err != nil {
		t.Fatal(err)
	}
	binding := "bindings:\n  - {name: b, instructions: ['notes:x.md']}\n"
	res := start(h.load("sources:\n  notes: {dir: ~/dotfiles/notes}\n  dot: {dir: ~/dotfiles}\n"+binding), h.dir, "")
	noErrors(t, res)
	eq(t, "files", bases(res.Files), "x.md")

	res = start(h.load("sources:\n  notes: {dir: ~/dotfiles/notes}\n"+binding), h.dir, "")
	eq(t, "files", bases(res.Files))
	oneError(t, res, "~/dotfiles/notes leads through a symlink at ~/dotfiles/notes")

	// Each source's folder holds the link on the way to the other, and
	// both folders' remotes are trusted: neither link passes first.
	h.repo("a", "git@github.com:other/a.git")
	h.repo("b", "git@github.com:other/b.git")
	h.write("a/x.md", "A")
	h.write("b/x.md", "B")
	symlink(t, h.path("b"), h.path("a/to-b"))
	symlink(t, h.path("a"), h.path("b/to-a"))
	for _, r := range []string{"a", "b"} {
		if err := state.Trust(paths.Real(h.path(r)), []string{"github.com/other/" + r}); err != nil {
			t.Fatal(err)
		}
	}
	res = start(h.load("sources:\n  sa: {dir: ~/b/to-a}\n  sb: {dir: ~/a/to-b}\nbindings:\n  - {name: b, instructions: ['sa:x.md', 'sb:x.md']}\n"), h.dir, "")
	eq(t, "files", bases(res.Files))
}
