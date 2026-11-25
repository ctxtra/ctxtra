package resolve

import (
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// What bindings add, in which order, and the errors that leave out part of
// it.
func TestContent(t *testing.T) {
	t.Run("order of text", func(t *testing.T) {
		h := newHome(t)
		h.write("a.md", "A")
		h.write("b.md", "B")
		env := h.load("bindings:\n  - {name: me, instructions: [~/b.md]}\n  - {name: iam, instructions: [~/a.md, ~/b.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "b.md", "a.md")
		if res.Files[0].Origin != "me" || !res.Files[0].Global {
			t.Errorf("first %+v", res.Files[0])
		}
		if text := res.Render(); strings.Index(text, "\nB") > strings.Index(text, "\nA") {
			t.Errorf("me's text isn't first:\n%s", text)
		}
	})

	t.Run("duplicate name", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: iam, instructions: [~/me.md]}\n  - {name: iam, instructions: [~/me.md]}\n  - {name: other, instructions: [~/me.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "applied", applied(res), "other")
		if len(errorsOf(res, yamlx.DuplicateBinding)) != 2 {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("a missing file", func(t *testing.T) {
		h := newHome(t)
		h.write("notes/me.md", "me")
		env := h.load("bindings:\n  - {name: b, instructions: [~/notes/gone.md, ~/notes/me.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "me.md")
		e := oneError(t, res, "~/notes/gone.md is not a file", "line 3", "binding b")
		if e.Err.Mistake != yamlx.BrokenEntry {
			t.Errorf("mistake %v", e.Err.Mistake)
		}
	})

	t.Run("a glob that matches nothing", func(t *testing.T) {
		h := newHome(t)
		h.write("notes/me.md", "me")
		env := h.load("bindings:\n  - {name: b, instructions: ['~/notes/*.txt', '~/notes/*.md'], skills: ['~/skills/*']}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "me.md")
		if len(res.Errors) != 2 || !strings.Contains(res.Errors[0].Error(), "*.txt matches no file") || !strings.Contains(res.Errors[1].Error(), "matches no skill") {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("a config YAML can't read", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, instructions: [~/me.md]\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files))
		oneError(t, res, "config.yaml", "line ")
	})

	t.Run("an empty content list", func(t *testing.T) {
		h := newHome(t)
		h.skill("dotfiles/skills/review", "review")
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: [], skills: ['dotfiles:skills/*']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "skills", skillNames(res), "review")
	})

	t.Run("a file left out of a list", func(t *testing.T) {
		h := newHome(t)
		h.write("dotfiles/agents/a.md", "A")
		h.write("dotfiles/agents/draft-b.md", "B")
		h.write("agent-context/me.md", "me")
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - name: me\n    instructions:\n" +
			"      - ~/agent-context/me.md\n      - '!dotfiles:agents/draft-*.md'\n      - 'dotfiles:agents/*.md'\n      - '!~/agent-context/gone.md'\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "me.md", "a.md")
		if ex := trace(t, res, "me").Excluded; len(ex) != 1 || ex[0] != (Exclusion{"!dotfiles:agents/draft-*.md", "dotfiles:agents/draft-b.md"}) {
			t.Errorf("excluded %+v", ex)
		}
	})

	t.Run("a skill left out of a list", func(t *testing.T) {
		h := newHome(t)
		h.skill("dotfiles/skills/a", "a")
		h.skill("dotfiles/skills/old", "old")
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, skills: ['dotfiles:skills/*', '!dotfiles:skills/old']}\n")
		eq(t, "skills", skillNames(start(env, h.dir, "")), "a")
	})

	t.Run("collections are not read yet", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, collections: [go-kit], instructions: [~/me.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "me.md")
		oneError(t, res, "collections go-kit", "not implemented yet")
	})

	t.Run("only ! in collections", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, collections: ['!iam-team:team-legacy'], instructions: [~/me.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "applied", applied(res))
		oneError(t, res, "no collection for them to remove", "binding b is left out")
	})

	t.Run("a file counts once", func(t *testing.T) {
		h := newHome(t)
		h.write("dotfiles/a.md", "A")
		env := h.load("sources: {dotfiles: {dir: ~/dotfiles}}\nbindings:\n  - {name: b, instructions: ['dotfiles:*.md', ~/dotfiles/a.md]}\n  - {name: c, instructions: [~/dotfiles/a.md]}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "a.md")
	})
}

// A glob that adds too much is an error, and the session gets what fits.
func TestCapacity(t *testing.T) {
	t.Run("a glob adds a large folder", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		for _, n := range []string{"a", "b", "c", "d", "e"} {
			h.write("big/"+n+".md", strings.Repeat(n, 40000))
		}
		env := h.load("max-ctx-chars: 40000\nbindings:\n  - {name: me, instructions: [~/me.md]}\n  - {name: big, instructions: ['~/big/*.md']}\n")
		res := start(env, h.dir, "")
		eq(t, "files", bases(res.Files), "me.md")
		eq(t, "dropped", bases(res.Dropped), "a.md", "b.md", "c.md", "d.md", "e.md")
		e := oneError(t, res, "max-ctx-chars", "bindings big")
		if e.Err.Mistake != yamlx.OverCapacity || res.Dropped[0].Over != "max-ctx-chars" {
			t.Errorf("mistake %v, over %q", e.Err.Mistake, res.Dropped[0].Over)
		}
	})

	t.Run("an agent's capacity", func(t *testing.T) {
		h := newHome(t)
		h.write("a.md", strings.Repeat("a", 100))
		h.write("b.md", strings.Repeat("b", 100))
		env := h.load("bindings:\n  - {name: me, instructions: [~/a.md, ~/b.md]}\n")
		res := start(env, h.dir, "")
		res.Fit(func(r *Result) bool { return len(r.Files) < 2 }, "Codex's limit")
		eq(t, "files", bases(res.Files), "a.md")
		oneError(t, res, "over Codex's limit", "bindings me")
	})
}

// Same-named skills and servers, and a source's server under the config's
// name for it.
func TestSameNames(t *testing.T) {
	const manifest = "schema: ctxtra/source/v1\nmcp-servers:\n  jira: {command: [jira-mcp, --site, '%s'], pass-env: [JIRA_TOKEN]}\n"

	t.Run("two release skills", func(t *testing.T) {
		h := newHome(t)
		h.skill("iam/skills/release", "release")
		h.skill("obs/skills/release", "release")
		env := h.load("sources: {iam-team: {dir: ~/iam}, obs-team: {dir: ~/obs}}\nbindings:\n  - {name: b, skills: ['iam-team:skills/*', 'obs-team:skills/*']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "skills", skillNames(res), "release", "release")
		if len(res.SameSkills) != 1 || res.SameSkills[0].First.Source != "iam-team" || res.SameSkills[0].Other.Source != "obs-team" {
			t.Errorf("same skills %+v", res.SameSkills)
		}
	})

	t.Run("two servers named jira", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/ctxtra-source.yaml", strings.ReplaceAll(manifest, "%s", "iam"))
		env := h.load("sources: {iam-team: {dir: ~/iam}}\nmcp-servers: {jira: {url: https://jira.me.example.com/mcp}}\n" +
			"bindings:\n  - {name: a, mcp-servers: [jira]}\n  - {name: b, mcp-servers: ['iam-team:jira']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "served", served(res), "jira")
		if res.MCPServers[0].ID != "jira" || len(res.MCPConflicts) != 1 || res.MCPConflicts[0] != (MCPConflict{"jira", "iam-team:jira", "jira", "b"}) {
			t.Errorf("servers %+v, conflicts %+v", res.MCPServers, res.MCPConflicts)
		}
	})

	t.Run("two teams' jira, bound directly", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/ctxtra-source.yaml", strings.ReplaceAll(manifest, "%s", "iam"))
		h.write("obs/ctxtra-source.yaml", strings.ReplaceAll(manifest, "%s", "obs"))
		env := h.load("sources: {iam-team: {dir: ~/iam}, obs-team: {dir: ~/obs}}\n" +
			"mcp-servers: {jira_iam: {use: 'iam-team:jira'}, jira_obs: {use: 'obs-team:jira'}}\n" +
			"bindings:\n  - {name: a, mcp-servers: ['iam-team:jira']}\n  - {name: b, mcp-servers: [jira_obs, jira_iam]}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "served", served(res), "jira_iam", "jira_obs")
		s := res.MCPServers[0]
		if s.ID != "iam-team:jira" || s.Source != "iam-team" || !slices.Equal(s.Command, []string{"jira-mcp", "--site", "iam"}) || !slices.Equal(s.EnvPass, []string{"JIRA_TOKEN"}) || s.Pinned {
			t.Errorf("server %+v", s)
		}
		if len(res.MCPConflicts) != 0 {
			t.Errorf("conflicts %+v", res.MCPConflicts)
		}
	})

	t.Run("two names for one server", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/ctxtra-source.yaml", strings.ReplaceAll(manifest, "%s", "iam"))
		env := h.load("sources: {iam-team: {dir: ~/iam}}\nmcp-servers: {a: {use: 'iam-team:jira'}, b: {use: 'iam-team:jira'}}\n" +
			"bindings:\n  - {name: x, mcp-servers: ['iam-team:jira']}\n")
		res := start(env, h.dir, "")
		eq(t, "served", served(res), "jira")
		if len(res.Errors) != 2 {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("a server left out of a list", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/ctxtra-source.yaml", "schema: ctxtra/source/v1\nmcp-servers:\n  jira: {url: https://a.example.com/mcp}\n  wiki: {url: https://b.example.com/mcp}\n")
		env := h.load("sources: {iam-team: {dir: ~/iam}}\nmcp-servers: {jira_iam: {use: 'iam-team:jira'}}\n" +
			"bindings:\n  - {name: x, mcp-servers: ['iam-team:*', '!jira_iam']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "served", served(res), "wiki")
	})

	t.Run("a repo source's servers need approval", func(t *testing.T) {
		h := newHome(t)
		entry := h.accept("iam-team", "git@github.com:acme/iam-ctx.git", "branch: main", "", commit("a"), map[string]string{
			"ctxtra-source.yaml": strings.ReplaceAll(manifest, "%s", "iam"),
		})
		env := h.load("sources: {iam-team: {repo: 'git@github.com:acme/iam-ctx.git', branch: main}}\nbindings:\n  - {name: x, mcp-servers: ['iam-team:jira']}\n", entry)
		res := start(env, h.dir, "")
		noErrors(t, res)
		if len(res.MCPServers) != 1 || !res.MCPServers[0].Pinned || res.MCPServers[0].Server.Origin != "repo:github.com/acme/iam-ctx" {
			t.Errorf("servers %+v", res.MCPServers)
		}
	})

	t.Run("a broken new name among servers a pattern matches", func(t *testing.T) {
		h := newHome(t)
		h.write("team/ctxtra-source.yaml", "schema: ctxtra/source/v1\nmcp-servers:\n  wiki: {url: https://w.example.com/mcp}\n")
		env := h.load("sources: {team: {dir: ~/team}}\nmcp-servers:\n  asana: {url: https://a.example.com/mcp}\n" +
			"  jira: {use: 'team:jira'}\n  linear: {url: https://mcp.linear.app/mcp}\nbindings:\n  - {name: x, mcp-servers: ['*']}\n")
		res := start(env, h.dir, "")
		eq(t, "served", served(res), "asana", "linear")
		oneError(t, res, "jira (use team:jira)", "declares no MCP server jira")
	})

	t.Run("a server a source doesn't declare", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/ctxtra-source.yaml", strings.ReplaceAll(manifest, "%s", "iam"))
		h.mkdir("obs")
		env := h.load("sources: {iam-team: {dir: ~/iam}, obs-team: {dir: ~/obs}}\nbindings:\n  - {name: x, mcp-servers: ['iam-team:wiki', 'obs-team:jira', 'iam-team:jira']}\n")
		res := start(env, h.dir, "")
		eq(t, "served", served(res), "jira")
		if len(res.Errors) != 2 || !strings.Contains(res.Errors[0].Error(), "declares no MCP server wiki") || !strings.Contains(res.Errors[1].Error(), "has no ctxtra-source.yaml") {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("an error in a manifest", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/ctxtra-source.yaml", "schema: ctxtra/source/v1\nmcp-servers:\n  jira: {url: https://a.example.com/mcp}\n  Wiki: {url: https://b.example.com/mcp}\n")
		env := h.load("sources: {iam-team: {dir: ~/iam}}\nbindings:\n  - {name: x, mcp-servers: ['iam-team:*']}\n")
		res := start(env, h.dir, "")
		eq(t, "served", served(res), "jira")
		oneError(t, res, "iam-team:ctxtra-source.yaml: line 4", "Wiki")
	})
}

func TestManifestReadOnlyWhenUsed(t *testing.T) {
	h := newHome(t)
	h.write("me.md", "me")
	h.write("iam/ctxtra-source.yaml", "schema: ctxtra/source/v1\nmcp-servers:\n  Bad: {url: https://a.example.com/mcp}\n")
	env := h.load("sources: {iam-team: {dir: ~/iam}}\nbindings:\n  - {name: a, instructions: [~/me.md]}\n  - {name: b, profiles: [iam], mcp-servers: ['iam-team:*']}\n")
	noErrors(t, start(env, h.dir, ""))
	if s := env.Catalog.Source("iam-team"); s.ManifestErrors() != nil {
		t.Errorf("a manifest no applied binding uses was read: %v", s.ManifestErrors())
	}
	oneError(t, start(env, h.dir, "", "iam"), "Bad")
}

// An Env resolves several sessions: each reports only the manifests it read.
func TestManifestErrorsOfEachSession(t *testing.T) {
	h := newHome(t)
	h.write("me.md", "me")
	h.write("iam/ctxtra-source.yaml", "schema: ctxtra/source/v1\nmcp-servers:\n  Bad: {url: https://a.example.com/mcp}\n")
	env := h.load("sources: {iam-team: {dir: ~/iam}}\nmcp-servers: {jira: {use: 'iam-team:jira'}}\nbindings:\n" +
		"  - {name: a, instructions: [~/me.md]}\n  - {name: b, profiles: [iam], mcp-servers: [jira]}\n")
	if res := start(env, h.dir, "", "iam"); len(res.Errors) != 2 || !strings.Contains(res.Errors[1].Error(), "Bad") {
		t.Errorf("with iam: %v", res.Errors)
	}
	noErrors(t, start(env, h.dir, ""))
}
