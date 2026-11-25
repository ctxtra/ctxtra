package resolve

import (
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/caserule"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// withCaseRule makes every volume compare names by rule during the test.
func withCaseRule(t *testing.T, rule caserule.Rule) {
	old := caseRuleOf
	caseRuleOf = func(string) caserule.Rule { return rule }
	t.Cleanup(func() { caseRuleOf = old })
}

// Conditions combine with AND, entries with OR, and ! entries leave out what
// they match.
func TestConditions(t *testing.T) {
	t.Run("profile only", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: iam, profiles: [iam], instructions: [~/me.md]}\n")
		plain := h.mkdir("notes")
		eq(t, "with iam", applied(start(env, plain, "codex", "iam")), "iam")
		eq(t, "without", applied(start(env, plain, "codex")))
	})

	t.Run("location and profile", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		svc := h.repo("svc", "git@github.com:acme/svc.git")
		other := h.repo("other", "git@github.com:other/x.git")
		env := h.load("bindings:\n  - {name: obs, location: {repos: [github.com/acme/**]}, profiles: [iam, observability], instructions: [~/me.md]}\n")
		eq(t, "acme with observability", applied(start(env, svc, "", "observability")), "obs")
		eq(t, "acme without a listed profile", applied(start(env, svc, "", "oncall")))
		eq(t, "outside acme", applied(start(env, other, "", "observability")))
	})

	t.Run("dirs or repos", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		work := h.mkdir("work")
		svc := h.repo("dev/svc", "https://github.com/acme/svc")
		env := h.load("bindings:\n  - {name: b, location: {dirs: [~/work/**], repos: [github.com/acme/**]}, instructions: [~/me.md]}\n")
		eq(t, "in ~/work", applied(start(env, work, "")), "b")
		eq(t, "an acme clone outside ~/work", applied(start(env, svc, "")), "b")
		eq(t, "elsewhere", applied(start(env, h.mkdir("dev/notes"), "")))
	})

	t.Run("all three conditions", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		api := h.mkdir("dev/api")
		env := h.load("bindings:\n  - {name: go, location: {dirs: [~/dev/**]}, profiles: [go-deep], agents: [claude-code], instructions: [~/me.md]}\n")
		eq(t, "codex", applied(start(env, api, "codex", "go-deep")))
		eq(t, "claude code", applied(start(env, api, "claude-code", "go-deep")), "go")
	})

	t.Run("not while a profile is on", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: tracker, profiles: ['!oncall'], instructions: [~/me.md]}\n")
		res := start(env, h.dir, "", "oncall")
		eq(t, "oncall", applied(res))
		if c := trace(t, res, "tracker").Conditions[0]; c.Not != "!oncall" {
			t.Errorf("condition %+v doesn't name !oncall", c)
		}
		eq(t, "none", applied(start(env, h.dir, "")), "tracker")
		eq(t, "iam", applied(start(env, h.dir, "", "iam")), "tracker")
	})

	t.Run("every agent but one", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, agents: ['!cursor'], instructions: [~/me.md]}\n")
		eq(t, "cursor", applied(start(env, h.dir, "cursor")))
		eq(t, "codex", applied(start(env, h.dir, "codex")), "b")
	})

	t.Run("only some agents", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, agents: [claude-code, codex], instructions: [~/me.md]}\n")
		eq(t, "cursor", applied(start(env, h.dir, "cursor")))
		eq(t, "codex", applied(start(env, h.dir, "codex")), "b")
		eq(t, "names without regard to case", applied(start(env, h.dir, "Codex")), "b")
	})

	t.Run("unknown agent name", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: a, agents: [claude-cod], instructions: [~/me.md]}\n  - {name: b, agents: ['!curser'], instructions: [~/me.md]}\n  - {name: c, instructions: [~/me.md]}\n")
		res := start(env, h.dir, "claude-code")
		eq(t, "applied", applied(res), "c")
		if len(res.Errors) != 2 || !strings.Contains(res.Errors[0].Error(), "claude-cod") || !strings.Contains(res.Errors[1].Error(), "curser") {
			t.Errorf("errors %v", res.Errors)
		}
	})

	t.Run("case", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: upper, profiles: [IAM], instructions: [~/me.md]}\n  - {name: lower, profiles: [iam], instructions: [~/me.md]}\n")
		eq(t, "applied", applied(start(env, h.dir, "", "iam")), "lower")
	})

	t.Run("all of a team's repos but one", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		legacy := h.repo("legacy", "git@github.com:acme/legacy.git")
		svc := h.repo("svc", "git@github.com:acme/svc.git")
		env := h.load("bindings:\n  - {name: b, location: {repos: [github.com/acme/**, '!github.com/acme/legacy']}, instructions: [~/me.md]}\n")
		res := start(env, legacy, "")
		eq(t, "legacy", applied(res))
		if c := trace(t, res, "b").Conditions[0]; c.Not != "!github.com/acme/legacy" {
			t.Errorf("condition %+v doesn't name the ! entry", c)
		}
		eq(t, "svc", applied(start(env, svc, "")), "b")
	})

	t.Run("a repository matches through any of its remotes", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		fork := h.repo("fork", "git@github.com:me/svc.git")
		testenv.Run(t, fork, "git", "remote", "add", "upstream", "https://github.com/ACME/svc.git")
		env := h.load("bindings:\n  - {name: b, location: {repos: [github.com/acme/svc]}, instructions: [~/me.md]}\n")
		eq(t, "applied", applied(start(env, fork, "")), "b")
	})

	t.Run("everywhere but one folder", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		foo := h.repo("clients/foo", "git@github.com:client/foo.git")
		env := h.load("bindings:\n  - {name: b, location: {dirs: ['!~/clients/**']}, instructions: [~/me.md]}\n")
		eq(t, "clients/foo", applied(start(env, foo, "")))
		eq(t, "dev/api", applied(start(env, h.mkdir("dev/api"), "")), "b")
	})

	t.Run("a folder exclusion doesn't stop the repository check", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		notes := h.mkdir("work/clients/notes")
		api := h.repo("work/clients/api", "git@github.com:acme/api.git")
		env := h.load("bindings:\n  - {name: b, location: {dirs: [~/work/**, '!~/work/clients/**'], repos: [github.com/acme/**]}, instructions: [~/me.md]}\n")
		eq(t, "notes", applied(start(env, notes, "")))
		eq(t, "acme clone", applied(start(env, api, "")), "b")
	})

	t.Run("a worktree elsewhere counts as its main checkout", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		svc := h.repo("dev/svc", "")
		wt := h.path("wt")
		testenv.Run(t, svc, "git", "worktree", "add", "-q", wt)
		env := h.load("bindings:\n  - {name: b, location: {dirs: [~/dev/**]}, instructions: [~/me.md]}\n")
		eq(t, "worktree", applied(start(env, wt, "")), "b")
	})

	t.Run("a broken folder entry", func(t *testing.T) {
		h := newHome(t)
		env := h.load("mcp-servers: {jira: {url: https://jira.example.com/mcp}}\nbindings:\n  - {name: b, location: {dirs: ['work/**']}, mcp-servers: [jira]}\n")
		res := start(env, h.mkdir("work"), "")
		eq(t, "applied", applied(res))
		oneError(t, res, "work/** is a relative folder", "matches nothing")
	})

	t.Run("a broken folder exclusion", func(t *testing.T) {
		h := newHome(t)
		env := h.load("mcp-servers: {jira: {url: https://jira.example.com/mcp}}\nbindings:\n  - {name: b, location: {dirs: ['!clients/**']}, mcp-servers: [jira]}\n")
		res := start(env, h.mkdir("work"), "")
		eq(t, "applied", applied(res))
		oneError(t, res, "clients/** is a relative folder", "binding b is left out")
	})

	t.Run("the last repository removed by hand", func(t *testing.T) {
		h := newHome(t)
		env := h.load("mcp-servers: {jira: {url: https://jira.example.com/mcp}}\nbindings:\n  - {name: b, location: {repos: []}, mcp-servers: [jira]}\n")
		res := start(env, h.dir, "")
		eq(t, "applied", applied(res))
		oneError(t, res, "remove location to apply everywhere")
	})

	t.Run("two parts of a monorepo", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		mono := h.repo("mono", "git@github.com:acme/mono.git")
		env := h.load("bindings:\n  - {name: b, location: {repos: [{repo: github.com/acme/mono, subdirs: [services/iam/**, services/billing/**]}]}, instructions: [~/me.md]}\n")
		for dir, want := range map[string]bool{"services/iam/api": true, "services/billing": true, "web": false} {
			got := len(applied(start(env, h.mkdir("mono/"+dir), ""))) == 1
			if got != want {
				t.Errorf("in %s: applied %v, want %v", dir, got, want)
			}
		}
		_ = mono
	})

	t.Run("one part of a monorepo left out", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		h.repo("mono", "git@github.com:acme/mono.git")
		env := h.load("bindings:\n  - {name: b, location: {repos: [{repo: github.com/acme/mono, subdirs: ['!services/legacy/**']}]}, instructions: [~/me.md]}\n")
		eq(t, "legacy", applied(start(env, h.mkdir("mono/services/legacy/api"), "")))
		eq(t, "billing", applied(start(env, h.mkdir("mono/services/billing"), "")), "b")
	})

	t.Run("a part left out, and the folder check", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		h.repo("dev/mono", "git@github.com:acme/mono.git")
		other := h.path("tmp/mono")
		testenv.Run(t, h.dir, "git", "clone", "-q", h.path("dev/mono"), other)
		testenv.Run(t, other, "git", "remote", "set-url", "origin", "git@github.com:acme/mono.git")
		env := h.load("bindings:\n  - {name: b, location: {dirs: [~/dev/**], repos: [{repo: github.com/acme/mono, subdirs: ['!services/legacy/**']}]}, instructions: [~/me.md]}\n")
		eq(t, "in ~/dev/mono", applied(start(env, h.mkdir("dev/mono/services/legacy/api"), "")), "b")
		eq(t, "elsewhere", applied(start(env, h.mkdir("tmp/mono/services/legacy/api"), "")))
	})

	t.Run("a missing set", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		svc := h.repo("svc", "git@github.com:acme/iam-api.git")
		env := h.load("sources: {iam-team: {dir: ~/iam}}\nbindings:\n  - {name: b, location: {repo-sets: ['iam-team:services']}, instructions: [~/me.md]}\n")
		h.mkdir("iam")
		res := start(env, svc, "")
		eq(t, "applied", applied(res))
		e := oneError(t, res, "repo-sets iam-team:services can't be read", "matches nothing")
		if e.Err.Mistake != yamlx.BrokenLocation {
			t.Errorf("mistake %v", e.Err.Mistake)
		}
	})

	t.Run("a missing excluded set", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		h.mkdir("corp")
		env := h.load("sources: {corp: {dir: ~/corp}}\nbindings:\n  - {name: b, location: {repo-sets: ['!corp:clients']}, instructions: [~/me.md]}\n")
		res := start(env, h.dir, "")
		eq(t, "applied", applied(res))
		oneError(t, res, "repo-sets !corp:clients can't be read", "binding b is left out")
	})
}

// location.dirs compares folders as the session folder's volume does.
func TestCaseRule(t *testing.T) {
	t.Run("case on macOS", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		withCaseRule(t, caserule.Insensitive)
		env := h.load("bindings:\n  - {name: b, location: {dirs: ['!~/Clients/**']}, instructions: [~/me.md]}\n")
		eq(t, "applied", applied(start(env, h.mkdir("clients/acme"), "")))
	})

	t.Run("a case-sensitive volume", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		withCaseRule(t, caserule.Sensitive)
		env := h.load("bindings:\n  - {name: b, location: {dirs: ['!~/Clients/**']}, instructions: [~/me.md]}\n")
		eq(t, "applied", applied(start(env, h.mkdir("clients/acme"), "")), "b")
	})

	t.Run("a positive entry on a volume that ignores case", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		withCaseRule(t, caserule.Insensitive)
		env := h.load("bindings:\n  - {name: b, location: {dirs: ['~/Work/**']}, instructions: [~/me.md]}\n")
		eq(t, "applied", applied(start(env, h.mkdir("work/x"), "")), "b")
	})

	t.Run("an unknown rule never widens a binding", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		withCaseRule(t, caserule.Unknown)
		env := h.load("bindings:\n  - {name: in, location: {dirs: ['~/Clients/**']}, instructions: [~/me.md]}\n  - {name: out, location: {dirs: ['!~/Clients/**']}, instructions: [~/me.md]}\n")
		res := start(env, h.mkdir("clients/acme"), "")
		eq(t, "applied", applied(res))
		if res.CaseRule != caserule.Unknown || !strings.Contains(trace(t, res, "in").Reason, "case rule is unknown") {
			t.Errorf("rule %v, reason %q", res.CaseRule, trace(t, res, "in").Reason)
		}
		noErrors(t, res)
	})

	t.Run("this machine's volume", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, location: {dirs: ['!~/Clients/**']}, instructions: [~/me.md]}\n")
		res := start(env, h.mkdir("clients/acme"), "")
		want := map[caserule.Rule][]string{caserule.Insensitive: nil, caserule.Sensitive: {"b"}, caserule.Unknown: nil}[caserule.Of(h.dir)]
		eq(t, "applied on a "+caserule.Of(h.dir).String()+" volume", applied(res), want...)
	})
}

// What the trace says about each condition.
func TestExplainConditions(t *testing.T) {
	t.Run("skipped for a profile", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, location: {dirs: ['~/**']}, profiles: [iam, observability], instructions: [~/me.md]}\n")
		res := start(env, h.dir, "", "oncall")
		tr := trace(t, res, "b")
		if tr.Matched || len(tr.Conditions) != 2 || !tr.Conditions[1].Held {
			t.Fatalf("trace %+v", tr)
		}
		if c := tr.Conditions[0]; c.Key != "profiles" || c.Held || c.Why != "needs iam, observability; active: oncall" {
			t.Errorf("profiles %+v", c)
		}
	})

	t.Run("agent preview", func(t *testing.T) {
		h := newHome(t)
		h.write("me.md", "me")
		env := h.load("bindings:\n  - {name: b, agents: [codex], instructions: [~/me.md]}\n")
		res := start(env, h.dir, "")
		if c := trace(t, res, "b").Conditions[0]; c.Held || c.Why != "for codex; no agent given" {
			t.Errorf("without an agent %+v", c)
		}
		eq(t, "with codex", applied(start(env, h.dir, "codex")), "b")
	})

	t.Run("MCP servers for one agent", func(t *testing.T) {
		h := newHome(t)
		env := h.load("mcp-servers: {linear: {url: https://mcp.linear.app/mcp}}\nbindings:\n  - {name: b, agents: [claude-code], mcp-servers: [linear]}\n")
		eq(t, "codex", served(start(env, h.dir, "codex")))
		eq(t, "claude code", served(start(env, h.dir, "claude-code")), "linear")
	})
}
