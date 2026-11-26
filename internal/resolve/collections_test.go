package resolve

import (
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// iamManifest is the manifest of the dir source iam-team in tests.
func (h *home) iamManifest(manifest string) {
	h.write("iam/ctxtra-source.yaml", "schema: ctxtra/source/v1\n"+manifest)
}

const iamSource = "sources: {iam-team: {dir: ~/iam}}\n"

// Collections: named sets in a source's manifest or the config, bound by
// name.
func TestCollections(t *testing.T) {
	t.Run("bind a collection", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/docs/conventions/a.md", "A")
		h.write("iam/docs/conventions/b.md", "B")
		h.write("iam/docs/other.md", "O")
		h.iamManifest("collections:\n  essentials:\n    instructions: [docs/conventions/*.md]\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:essentials']}\n"), h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "a.md", "b.md")
		if f := res.Files[0]; f.Ref != "iam-team:docs/conventions/a.md" || f.Origin != "iam" {
			t.Errorf("hit %+v", f)
		}
	})

	t.Run("the source moves files", func(t *testing.T) {
		h := newHome(t)
		conf := "sources: {iam-team: {repo: 'https://github.com/acme/iam-ctx.git', branch: main}}\n" +
			"bindings:\n  - {name: iam, collections: ['iam-team:essentials']}\n"
		v1 := h.accept("iam-team", "https://github.com/acme/iam-ctx.git", "branch: main", "", commit("a"), map[string]string{
			"ctxtra-source.yaml": "schema: ctxtra/source/v1\ncollections:\n  essentials: {instructions: ['docs/*.md']}\n",
			"docs/a.md":          "A",
		})
		eq(t, "files", refsOf(start(h.load(conf, v1), h.dir, "").Files), "iam-team:docs/a.md")
		v2 := h.accept("iam-team", "https://github.com/acme/iam-ctx.git", "branch: main", "", commit("c"), map[string]string{
			"ctxtra-source.yaml": "schema: ctxtra/source/v1\ncollections:\n  essentials: {instructions: ['guides/*.md']}\n",
			"guides/a.md":        "A",
		})
		res := start(h.load(conf, v2), h.dir, "")
		noErrors(t, res)
		eq(t, "files", refsOf(res.Files), "iam-team:guides/a.md")
	})

	t.Run("a manifest deeper in the repository", func(t *testing.T) {
		h := newHome(t)
		files := map[string]string{
			"ctxtra-source.yaml":               "schema: ctxtra/source/v1\ncollections:\n  essentials: {instructions: [root.md]}\n",
			"root.md":                          "root",
			"teams/billing/ctxtra-source.yaml": "schema: ctxtra/source/v1\ncollections:\n  essentials: {instructions: [billing.md]}\n",
			"teams/billing/billing.md":         "billing",
		}
		l := h.accept("billing-ctx", "https://github.com/acme/agent-context.git", "branch: main", "teams/billing", commit("b"), files)
		res := start(h.load("sources:\n  billing-ctx: {repo: 'https://github.com/acme/agent-context.git', branch: main, subdir: teams/billing}\n"+
			"bindings:\n  - {name: billing, collections: ['billing-ctx:essentials']}\n", l), h.dir, "")
		noErrors(t, res)
		eq(t, "files", refsOf(res.Files), "billing-ctx:billing.md")
	})

	t.Run("a source without a manifest", func(t *testing.T) {
		h := newHome(t)
		h.write("docs/guides/a.md", "A")
		env := h.load("sources: {docs-repo: {dir: ~/docs}}\nbindings:\n  - {name: d, instructions: ['docs-repo:guides/*.md']}\n" +
			"  - {name: c, collections: ['docs-repo:essentials'], instructions: ['docs-repo:guides/a.md']}\n")
		res := start(env, h.dir, "")
		eq(t, "files", refsOf(res.Files), "docs-repo:guides/a.md")
		oneError(t, res, "binding c", "source docs-repo has no ctxtra-source.yaml", "the entry is left out")
	})

	t.Run("another source in a manifest", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/a.md", "A")
		h.iamManifest("collections:\n  essentials:\n    collections: ['org:base']\n    instructions: [a.md]\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:essentials']}\n"), h.dir, "")
		eq(t, "files", refsOf(res.Files), "iam-team:a.md")
		oneError(t, res, "iam-team:ctxtra-source.yaml", "collection essentials", "a manifest references only its own collections")
	})

	t.Run("a team set on top of an org set", func(t *testing.T) {
		h := newHome(t)
		for _, f := range []string{"org/a.md", "org/legacy.md", "org/z.md", "iam/b.md"} {
			h.write("acme/"+f, f)
		}
		h.write("acme/ctxtra-source.yaml", "schema: ctxtra/source/v1\ncollections:\n  base: {instructions: [org/*.md]}\n"+
			"  iam.essentials: {collections: [base], instructions: [iam/*.md, '!org/legacy.md']}\n")
		res := start(h.load("sources: {acme: {dir: ~/acme}}\nbindings:\n  - {name: iam, collections: ['acme:iam.essentials']}\n"), h.dir, "")
		noErrors(t, res)
		eq(t, "files", refsOf(res.Files), "acme:org/a.md", "acme:org/z.md", "acme:iam/b.md")
	})

	t.Run("a cycle", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/a.md", "A")
		h.write("iam/b.md", "B")
		h.write("iam/c.md", "C")
		h.iamManifest("collections:\n  a: {collections: [b], instructions: [a.md]}\n  b: {collections: [a], instructions: [b.md]}\n" +
			"  c: {collections: [a], instructions: [c.md]}\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:c', 'iam-team:b']}\n"), h.dir, "")
		eq(t, "files", refsOf(res.Files), "iam-team:c.md")
		e := oneError(t, res, "iam-team:ctxtra-source.yaml", "collections a and b nest each other, so they add nothing")
		if e.Err.Mistake != yamlx.CollectionCycle {
			t.Errorf("mistake %v", e.Err.Mistake)
		}
	})

	t.Run("a collection that nests itself", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/a.md", "A")
		h.iamManifest("collections:\n  a: {collections: [a], instructions: [a.md]}\n  all: {collections: ['*'], instructions: [a.md]}\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:all']}\n"), h.dir, "")
		// A pattern doesn't match its own collection: all nests a, which
		// nests itself.
		eq(t, "files", refsOf(res.Files), "iam-team:a.md")
		oneError(t, res, "collection a nests itself")
	})

	t.Run("missing collection at session start", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/a.md", "A")
		h.iamManifest("collections:\n  essentials: {instructions: [a.md]}\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:ops-only', 'iam-team:essentials']}\n"), h.dir, "")
		eq(t, "files", refsOf(res.Files), "iam-team:a.md")
		oneError(t, res, "binding iam", "source iam-team has no collection ops-only")
	})

	t.Run("a nested collection that doesn't exist", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/a.md", "A")
		h.iamManifest("collections:\n  essentials: {collections: [gone, 'none-*'], instructions: [a.md, 'docs/*.md']}\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:essentials']}\n"), h.dir, "")
		eq(t, "files", refsOf(res.Files), "iam-team:a.md")
		var msgs []string
		for _, e := range res.Errors {
			msgs = append(msgs, e.Err.Msg)
		}
		eq(t, "errors", msgs, "source iam-team has no collection gone", "none-* matches no collection of source iam-team",
			"iam-team:docs/*.md matches no file")
	})

	t.Run("unquoted in a manifest", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/docs/a.md", "A")
		h.write("iam/docs/draft-b.md", "B")
		h.iamManifest("collections:\n  essentials: {instructions: [docs/*.md, !docs/draft-*.md]}\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:essentials']}\n"), h.dir, "")
		eq(t, "files", bases(res.Files))
		if len(res.Errors) == 0 || !strings.Contains(res.Errors[len(res.Errors)-1].Error(), "'!docs/draft-*.md'") {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("each collection is expanded once", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/base.md", "base")
		h.write("iam/x.md", "x")
		h.write("iam/y.md", "y")
		h.iamManifest("collections:\n  base: {instructions: [base.md]}\n  x: {collections: [base], instructions: [x.md]}\n" +
			"  y: {collections: [base, x], instructions: [y.md]}\n")
		env := h.load(iamSource + "bindings:\n  - {name: one, collections: ['iam-team:y', 'iam-team:x']}\n  - {name: two, collections: ['iam-team:x']}\n")
		res := start(env, h.dir, "")
		noErrors(t, res)
		eq(t, "files", refsOf(res.Files), "iam-team:base.md", "iam-team:x.md", "iam-team:y.md")
		if len(trace(t, res, "two").Files) != 0 {
			t.Errorf("two adds %v again", trace(t, res, "two").Files)
		}
	})

	t.Run("skills and servers of a collection", func(t *testing.T) {
		h := newHome(t)
		h.skill("iam/skills/release", "release")
		h.iamManifest("collections:\n  essentials: {skills: ['skills/*'], mcp-servers: [jira]}\n" +
			"mcp-servers:\n  jira: {url: 'https://jira.example/mcp'}\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:essentials']}\n"), h.dir, "")
		noErrors(t, res)
		eq(t, "skills", skillNames(res), "release")
		eq(t, "servers", served(res), "jira")
		if res.MCPServers[0].ID != "iam-team:jira" {
			t.Errorf("server %+v", res.MCPServers[0])
		}
	})

	t.Run("errors in a collection are reported once", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/a.md", "A")
		h.iamManifest("collections:\n  essentials: {instructions: [a.md, gone.md]}\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: one, collections: ['iam-team:essentials']}\n  - {name: two, collections: ['iam-team:essentials']}\n"), h.dir, "")
		oneError(t, res, "collection essentials", "gone.md is not a file")
	})
}

// Exclusions in content lists remove what the binding's collections add.
func TestCollectionExclusions(t *testing.T) {
	t.Run("a file left out of a collection", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/docs/testing.md", "T")
		h.write("iam/docs/style.md", "S")
		h.iamManifest("collections:\n  essentials: {instructions: ['docs/*.md']}\n")
		res := start(h.load(iamSource+"bindings:\n  - name: iam\n    collections: ['iam-team:essentials']\n    instructions: ['!iam-team:docs/testing.md']\n"), h.dir, "")
		noErrors(t, res)
		eq(t, "files", refsOf(res.Files), "iam-team:docs/style.md")
		if ex := trace(t, res, "iam").Excluded; len(ex) != 1 || ex[0] != (Exclusion{"!iam-team:docs/testing.md", "iam-team:docs/testing.md"}) {
			t.Errorf("excluded %+v", ex)
		}
	})

	t.Run("collections by pattern, one left out", func(t *testing.T) {
		h := newHome(t)
		for _, f := range []string{"api.md", "backend.md", "legacy.md", "shared.md"} {
			h.write("iam/"+f, f)
		}
		h.iamManifest("collections:\n  team-api: {instructions: [api.md, shared.md]}\n" +
			"  team-backend: {collections: [team-legacy], instructions: [backend.md]}\n" +
			"  team-legacy: {instructions: [legacy.md, shared.md]}\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:team-*', '!iam-team:team-legacy']}\n"), h.dir, "")
		noErrors(t, res)
		eq(t, "files", refsOf(res.Files), "iam-team:api.md", "iam-team:shared.md", "iam-team:backend.md")
	})

	t.Run("a ! collection that excludes nothing", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/a.md", "A")
		h.iamManifest("collections:\n  essentials: {instructions: [a.md]}\n")
		res := start(h.load(iamSource+"bindings:\n  - {name: iam, collections: ['iam-team:essentials', '!iam-team:legacy']}\n"), h.dir, "")
		eq(t, "files", refsOf(res.Files), "iam-team:a.md")
		oneError(t, res, "!iam-team:legacy excludes nothing")
	})

	t.Run("a ! collection of a source not on this machine", func(t *testing.T) {
		h := newHome(t)
		res := start(h.load("sources: {iam-team: {dir: ~/gone}}\nbindings:\n  - {name: iam, collections: ['iam-team:team-*', '!iam-team:team-legacy']}\n"), h.dir, "")
		noErrors(t, res)
		eq(t, "unavailable", trace(t, res, "iam").Unavailable, "iam-team")
	})

	t.Run("only ! instructions with collections", func(t *testing.T) {
		h := newHome(t)
		h.skill("iam/skills/release", "release")
		h.skill("iam/skills/old", "old")
		h.iamManifest("collections:\n  essentials: {skills: ['skills/*'], mcp-servers: [jira, wiki]}\n" +
			"mcp-servers:\n  jira: {url: 'https://jira.example/mcp'}\n  wiki: {url: 'https://wiki.example/mcp'}\n")
		res := start(h.load(iamSource+"bindings:\n  - name: iam\n    collections: ['iam-team:essentials']\n"+
			"    skills: ['!iam-team:skills/old']\n    mcp-servers: ['!iam-team:wiki']\n"), h.dir, "")
		noErrors(t, res)
		eq(t, "skills", skillNames(res), "release")
		eq(t, "servers", served(res), "jira")
	})
}

// The config's own collections: one set for several bindings.
func TestConfigCollections(t *testing.T) {
	t.Run("one set, two places", func(t *testing.T) {
		h := newHome(t)
		h.write(".config/ctxtra/go-notes.md", "notes")
		h.write("go-style/go/style.md", "style")
		h.mkdir("dev/app")
		h.repo("work/api", "git@github.com:acme/api.git")
		env := h.load("sources: {go-style: {dir: ~/go-style}}\ncollections:\n  go-kit:\n    instructions: [go-notes.md, 'go-style:go/*.md']\n" +
			"bindings:\n  - {name: go, location: {dirs: [~/dev/**]}, collections: [go-kit]}\n" +
			"  - {name: go-work, location: {repos: [github.com/acme/**]}, collections: [go-kit]}\n")
		for _, dir := range []string{"dev/app", "work/api"} {
			res := start(env, h.path(dir), "")
			noErrors(t, res)
			eq(t, dir, bases(res.Files), "go-notes.md", "style.md")
		}
	})

	t.Run("a source's collection and the config's own server", func(t *testing.T) {
		h := newHome(t)
		h.write("iam/a.md", "A")
		h.write("me.md", "me")
		h.iamManifest("collections:\n  base: {instructions: [a.md]}\n")
		res := start(h.load(iamSource+"mcp-servers: {linear: {url: 'https://mcp.linear.app/mcp'}}\n"+
			"collections:\n  mine: {collections: ['iam-team:base'], instructions: [~/me.md], mcp-servers: [linear]}\n"+
			"  outer: {collections: [mine]}\n"+
			"bindings:\n  - {name: b, collections: [outer]}\n"), h.dir, "")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "a.md", "me.md")
		eq(t, "servers", served(res), "linear")
	})

	t.Run("a config collection that doesn't exist", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		res := start(h.load("bindings:\n  - {name: b, collections: [go-kit], instructions: [~/me.md]}\n"), h.dir, "")
		eq(t, "files", bases(res.Files), "me.md")
		oneError(t, res, "binding b", "the config has no collection go-kit")
	})

	t.Run("a cycle in the config", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		res := start(h.load("collections:\n  a: {collections: [b], instructions: [~/me.md]}\n  b: {collections: [a]}\n"+
			"bindings:\n  - {name: x, collections: [a]}\n"), h.dir, "")
		eq(t, "files", bases(res.Files))
		oneError(t, res, "config.yaml", "collections a and b nest each other")
	})
}

// What a binding adds: its collections first, then its own entries.
func TestCollectionAndRawFilesTogether(t *testing.T) {
	h := newHome(t)
	h.write("obs/docs/a.md", "A")
	h.write("obs/runbooks/a.md", "R")
	h.write("obs/runbooks/b.md", "R2")
	h.write("obs/ctxtra-source.yaml", "schema: ctxtra/source/v1\ncollections:\n  essentials: {instructions: [docs/a.md, runbooks/b.md]}\n")
	res := start(h.load("sources: {obs-team: {dir: ~/obs}}\nbindings:\n  - name: obs\n    instructions: ['obs-team:runbooks/*.md']\n"+
		"    collections: ['obs-team:essentials']\n"), h.dir, "")
	noErrors(t, res)
	eq(t, "files", refsOf(res.Files), "obs-team:docs/a.md", "obs-team:runbooks/b.md", "obs-team:runbooks/a.md")
}

func refsOf(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Ref)
	}
	return out
}
