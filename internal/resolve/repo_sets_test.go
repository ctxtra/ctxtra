package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/gitrepo"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// inRepo resolves a session in folder sub of a clone of repo, a host/path,
// in h's home.
func inRepo(env *Env, h *home, repo, sub string) *Result {
	root := h.mkdir("clones/" + repo[strings.LastIndex(repo, "/")+1:])
	return env.Resolve(Input{Dir: h.mkdir("clones/" + repo[strings.LastIndex(repo, "/")+1:] + "/" + sub),
		Repo: &gitrepo.Repo{Root: root, Top: root, Subpath: sub, Remotes: []string{repo}}})
}

// teamManifest is a dir source iam-team with the manifest's repo sets.
func teamManifest(h *home, sets string) string {
	h.write("iam/ctxtra-source.yaml", "schema: ctxtra/source/v1\nrepo-sets:\n"+sets)
	return "sources: {iam-team: {dir: ~/iam}}\n"
}

func TestRepoSets(t *testing.T) {
	t.Run("the team starts a new repository", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		config := teamManifest(h, "  services: [github.com/acme/iam-api]\n") +
			"bindings:\n  - {name: b, location: {repo-sets: ['iam-team:services']}, instructions: [~/me.md]}\n"
		eq(t, "before", applied(inRepo(h.load(config), h, "github.com/acme/iam-billing", ".")))
		teamManifest(h, "  services: [github.com/acme/iam-api, github.com/acme/iam-billing]\n")
		res := inRepo(h.load(config), h, "github.com/acme/iam-billing", ".")
		noErrors(t, res)
		eq(t, "after", applied(res), "b")
	})

	t.Run("a folder in a set", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load(teamManifest(h, "  services: [~/work/**, github.com/acme/iam-api]\n") +
			"bindings:\n  - {name: b, location: {repo-sets: ['iam-team:services']}, instructions: [~/me.md]}\n")
		res := inRepo(env, h, "github.com/acme/iam-api", ".")
		eq(t, "applied", applied(res), "b")
		e := oneError(t, res, "iam-team:ctxtra-source.yaml", "~/work/** is a folder; a set lists repositories only")
		if e.Err.Mistake != yamlx.BrokenEntry {
			t.Errorf("mistake %v", e.Err.Mistake)
		}
	})

	t.Run("a team's set with the user's own changes", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load(teamManifest(h, "  services: [github.com/acme/iam-*]\n") +
			"bindings:\n  - name: b\n    location: {repo-sets: ['iam-team:services'], repos: [github.com/me/iam-playground, '!github.com/acme/iam-legacy']}\n" +
			"    instructions: [~/me.md]\n")
		eq(t, "iam-api", applied(inRepo(env, h, "github.com/acme/iam-api", ".")), "b")
		eq(t, "iam-playground", applied(inRepo(env, h, "github.com/me/iam-playground", ".")), "b")
		res := inRepo(env, h, "github.com/acme/iam-legacy", ".")
		noErrors(t, res)
		eq(t, "iam-legacy", applied(res))
		if c := trace(t, res, "b").Conditions[0]; c.Not != "!github.com/acme/iam-legacy" {
			t.Errorf("left out by %q", c.Not)
		}
	})

	t.Run("my own set", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("repo-sets: {acme: [github.com/acme/**]}\nbindings:\n  - {name: b, location: {repo-sets: [acme]}, instructions: [~/me.md]}\n")
		res := inRepo(env, h, "github.com/acme/api", ".")
		noErrors(t, res)
		eq(t, "applied", applied(res), "b")
		if c := trace(t, res, "b").Conditions[0]; c.Why != "repo-sets acme matches github.com/acme/api (set entry github.com/acme/**)" {
			t.Errorf("why %q", c.Why)
		}
	})

	t.Run("not in a team's repos", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		h.write("corp/ctxtra-source.yaml", "schema: ctxtra/source/v1\nrepo-sets:\n  clients: [github.com/acme/client-*]\n")
		env := h.load("sources: {corp: {dir: ~/corp}}\nbindings:\n  - {name: b, location: {repo-sets: ['!corp:clients']}, instructions: [~/me.md]}\n")
		res := inRepo(env, h, "github.com/acme/client-x", ".")
		noErrors(t, res)
		eq(t, "client-x", applied(res))
		c := trace(t, res, "b").Conditions[0]
		if c.Not != "!corp:clients" || !strings.Contains(c.Why, "set entry github.com/acme/client-*") {
			t.Errorf("left out by %q: %s", c.Not, c.Why)
		}
		eq(t, "api", applied(inRepo(env, h, "github.com/acme/api", ".")), "b")
	})

	t.Run("a set's own exclusions and parts", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load(teamManifest(h, "  services:\n    - github.com/acme/iam-*\n    - '!github.com/acme/iam-sandbox'\n"+
			"    - {repo: github.com/acme/mono, subdirs: [services/iam/**]}\n") +
			"bindings:\n  - {name: b, location: {repo-sets: ['iam-team:services']}, instructions: [~/me.md]}\n")
		eq(t, "iam-api", applied(inRepo(env, h, "github.com/acme/iam-api", ".")), "b")
		eq(t, "iam-sandbox", applied(inRepo(env, h, "github.com/acme/iam-sandbox", ".")))
		eq(t, "mono services/iam", applied(inRepo(env, h, "github.com/acme/mono", "services/iam/api")), "b")
		eq(t, "mono web", applied(inRepo(env, h, "github.com/acme/mono", "web")))
	})

	t.Run("moving repositories into a set", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("repo-sets: {acme: [github.com/acme/**, '!github.com/acme/legacy']}\nbindings:\n" +
			"  - {name: inline, location: {dirs: [~/work/**], repos: [github.com/acme/**, '!github.com/acme/legacy']}, instructions: [~/me.md]}\n" +
			"  - {name: set, location: {dirs: [~/work/**], repo-sets: [acme]}, instructions: [~/me.md]}\n")
		session := func(dir string) *Result {
			h.mkdir(dir)
			return env.Resolve(Input{Dir: h.path(dir), Repo: &gitrepo.Repo{Root: h.path(dir), Top: h.path(dir), Subpath: ".", Remotes: []string{"github.com/acme/legacy"}}})
		}
		eq(t, "under ~/work", applied(session("work/legacy")), "inline", "set")
		eq(t, "under ~/dev", applied(session("dev/legacy")))
	})

	t.Run("an empty set matches nothing", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("repo-sets: {acme: []}\nbindings:\n  - {name: b, location: {repo-sets: [acme]}, instructions: [~/me.md]}\n")
		res := inRepo(env, h, "github.com/acme/api", ".")
		eq(t, "applied", applied(res))
		oneError(t, res, "the set has no repositories, so it matches nothing")
	})
}

// A set that can't be read matches nothing in a positive entry and leaves
// out the binding in a ! entry.
func TestUnreadableRepoSets(t *testing.T) {
	t.Run("a missing set", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		h.mkdir("iam")
		env := h.load("sources: {iam-team: {dir: ~/iam}}\nbindings:\n  - {name: b, location: {repo-sets: ['iam-team:services']}, instructions: [~/me.md]}\n")
		res := inRepo(env, h, "github.com/acme/iam-api", ".")
		eq(t, "applied", applied(res))
		e := oneError(t, res, "repo-sets iam-team:services can't be read", "source iam-team has no ctxtra-source.yaml", "matches nothing")
		if e.Err.Mistake != yamlx.BrokenLocation {
			t.Errorf("mistake %v", e.Err.Mistake)
		}
		// Every session reports it, also where the binding couldn't apply.
		oneError(t, start(env, h.dir, ""), "repo-sets iam-team:services can't be read")
		// So does a command that checks the binding alone.
		oneError(t, env.Alone(env.Config.Bindings[0], Input{Dir: h.dir}), "repo-sets iam-team:services can't be read")
	})

	t.Run("a set the manifest doesn't have", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load(teamManifest(h, "  web: [github.com/acme/web]\n") +
			"bindings:\n  - {name: b, location: {repo-sets: ['iam-team:services']}, instructions: [~/me.md]}\n")
		oneError(t, inRepo(env, h, "github.com/acme/iam-api", "."), "source iam-team has no repo set services")
	})

	t.Run("a missing excluded set", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		h.write("corp/ctxtra-source.yaml", "schema: ctxtra/source/v1\n")
		env := h.load("sources: {corp: {dir: ~/corp}}\nbindings:\n  - {name: b, location: {repo-sets: ['!corp:clients']}, instructions: [~/me.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "applied", applied(res))
		e := oneError(t, res, "repo-sets !corp:clients can't be read", "source corp has no repo set clients; binding b is left out")
		if e.Err.Mistake != yamlx.BrokenExclusion {
			t.Errorf("mistake %v", e.Err.Mistake)
		}
	})

	t.Run("a set with errors", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("repo-sets: {acme: {repo: github.com/acme/api}}\nbindings:\n  - {name: b, location: {repo-sets: ['!acme']}, instructions: [~/me.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "applied", applied(res))
		if len(errorsOf(res, yamlx.BrokenExclusion)) != 1 || !strings.Contains(errorsOf(res, yamlx.BrokenExclusion)[0].Error(), "the repo set acme of the config has errors") {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("an excluded set on a new machine", func(t *testing.T) {
		h := newHome(t)
		entry := "  corp:\n    repo: git@github.com:acme/corp-ctx.git\n    branch: main\n    commit: " + commit("a") + "\n"
		env := h.load("sources: {corp: {repo: 'git@github.com:acme/corp-ctx.git', branch: main}}\nmcp-servers: {linear: {url: 'https://mcp.linear.app/mcp'}}\n"+
			"bindings:\n  - {name: b, location: {repo-sets: ['!corp:clients']}, mcp-servers: [linear]}\n", entry)
		res := inRepo(env, h, "github.com/acme/client-x", ".")
		eq(t, "applied", applied(res))
		eq(t, "servers", served(res))
		var download, left bool
		for _, e := range res.Errors {
			download = download || strings.Contains(e.Error(), "ctxtra source download")
			left = left || e.Err.Mistake == yamlx.BrokenExclusion && strings.Contains(e.Error(), "repo-sets !corp:clients can't be read")
		}
		if !download || !left {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("a positive set of a source not downloaded", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		entry := "  corp:\n    repo: git@github.com:acme/corp-ctx.git\n    branch: main\n    commit: " + commit("a") + "\n"
		env := h.load("sources: {corp: {repo: 'git@github.com:acme/corp-ctx.git', branch: main}}\n"+
			"bindings:\n  - {name: b, location: {repo-sets: ['corp:services']}, instructions: [~/me.md]}\n", entry)
		res := inRepo(env, h, "github.com/acme/api", ".")
		eq(t, "applied", applied(res))
		// The source's own error says what to do; the entry adds none.
		oneError(t, res, "not downloaded", "ctxtra source download")
		eq(t, "unavailable", trace(t, res, "b").Unavailable, "corp")
	})

	t.Run("an excluded set of a source not on this machine", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("sources: {corp: {dir: ~/corp}}\nbindings:\n" +
			"  - {name: b, location: {repo-sets: ['!corp:clients']}, instructions: [~/me.md]}\n" +
			"  - {name: c, location: {repo-sets: ['corp:clients']}, instructions: [~/me.md]}\n")
		res := inRepo(env, h, "github.com/acme/api", ".")
		noErrors(t, res)
		eq(t, "applied", applied(res))
		if c := trace(t, res, "b").Conditions[0]; !strings.Contains(c.Why, "~/corp is not on this machine") {
			t.Errorf("why %q", c.Why)
		}
	})

	t.Run("a broken entry in an excluded set", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		h.write("corp/ctxtra-source.yaml", "schema: ctxtra/source/v1\nrepo-sets:\n  clients: [github.com/acme/client-*, 'github.com/acme/[x']\n")
		env := h.load("sources: {corp: {dir: ~/corp}}\nbindings:\n  - {name: b, location: {repo-sets: ['!corp:clients']}, instructions: [~/me.md]}\n")
		res := inRepo(env, h, "github.com/acme/api", ".")
		eq(t, "api", applied(res), "b")
		oneError(t, res, "github.com/acme/[x is not a valid pattern")
		eq(t, "client-x", applied(inRepo(env, h, "github.com/acme/client-x", ".")))
	})

	t.Run("an excluded set when git can't tell the repository", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("repo-sets: {clients: [github.com/acme/client-*]}\nbindings:\n  - {name: b, location: {repo-sets: ['!clients']}, instructions: [~/me.md]}\n")
		old := detectRepo
		detectRepo = func(string) (*gitrepo.Repo, error) { return nil, errors.New("git didn't answer in 3s") }
		t.Cleanup(func() { detectRepo = old })
		res := start(env, h.dir, "")
		eq(t, "applied", applied(res))
		oneError(t, res, "repo-sets !clients can't be checked")
	})
}
