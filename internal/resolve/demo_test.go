package resolve

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/gitrepo"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// The demo config, lock and manifest of the cfg, lock and manifest tests,
// with the files they name, and a session in a clone of github.com/acme/mono
// under ~/work, with observability on, in Claude Code.

// demo is a home with the demo's files.
type demo struct {
	home, session string
}

// text is a file of about 2,000 characters.
func text(name string) string {
	return strings.Repeat("Text of "+name+". ", 2000/(len(name)+10)) + "\n"
}

func skillText(name string) string {
	return "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n\n" + text(name)
}

func newDemo(tb testing.TB) demo {
	tb.Helper()
	h := testenv.Home(tb)
	write := func(p, content string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	data := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			tb.Fatal(err)
		}
		return string(b)
	}
	files := func(dir string, names ...string) {
		for _, n := range names {
			if strings.HasSuffix(n, "/") {
				write(filepath.Join(dir, n, "SKILL.md"), skillText(filepath.Base(n)))
			} else {
				write(filepath.Join(dir, n), text(n))
			}
		}
	}
	gens := map[string]string{}
	for _, e := range lock.Parse([]byte(data("../lock/testdata/demo.lock"))).Entries {
		gens[e.Name] = source.GenDir(e.Repo, e.Subdir, e.Commit)
	}
	write(filepath.Join(gens["iam-team"], "ctxtra-source.yaml"), data("../manifest/testdata/ctxtra-source.yaml"))
	files(gens["iam-team"], "docs/conventions/naming.md", "docs/conventions/errors.md", "docs/overview.md", "docs/testing.md",
		"docs/draft-auth.md", "skills/login/", "skills/tokens/", "services/billing.md")
	files(gens["obs-team"], "docs/conventions/metrics.md", "docs/conventions/logs.md", "docs/conventions/traces.md",
		"runbooks/alerts.md", "runbooks/paging.md", "runbooks/latency.md", "runbooks/errors.md", "runbooks/capacity.md")
	files(gens["security"], "guidelines/secrets.md", "guidelines/deps.md", "guidelines/review.md")
	files(gens["go-style"], "go/style.md", "go/testing.md")
	files(gens["billing-ctx"], "skills/invoice/", "skills/refund/")
	files(filepath.Join(h, "dev", "dotfiles"), "agents/me-work.md", "agents/tools.md", "agents/draft-new.md",
		"skills/notes/", "skills/commit/", "skills/go-test/", "skills/review/go/", "skills/review/sql/")
	files(h, "agent-context/me.md", ".config/ctxtra/go-notes.md")

	d := demo{home: h, session: filepath.Join(h, "work", "mono", "services", "billing")}
	write(filepath.Join(d.session, "main.go"), "package main\n")
	mono := filepath.Join(h, "work", "mono")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"remote", "add", "origin", "git@github.com:acme/mono.git"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = mono
		if out, err := cmd.CombinedOutput(); err != nil {
			tb.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write(filepath.Join(h, ".config", "ctxtra", "ctxtra.lock"), data("../lock/testdata/demo.lock"))
	return d
}

// config writes a config file named name and returns its path.
func (d demo) config(tb testing.TB, name, content string) string {
	tb.Helper()
	p := filepath.Join(d.home, ".config", "ctxtra", name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		tb.Fatal(err)
	}
	return p
}

// inlined is the demo config with its collections and repo sets written
// out in the bindings that use them, so it adds the same files without
// either.
const inlined = `schema: ctxtra/config/v1
sources:
  iam-team: {repo: 'git@github.com:acme/iam-ctx.git', branch: main}
  obs-team: {repo: 'https://github.com/acme/observability-ctx.git', tag: v1.4.0}
  security: {repo: 'git@github.com:acme/security-guidelines.git', rev: 9f8e7d6c5b4a39281706f5e4d3c2b1a098765432}
  go-style: {repo: 'https://github.com/someone/go-agent-guides.git', branch: main, check-updates: false}
  iam-dev: {repo: ~/dev/iam-ctx, branch: my-change}
  billing-ctx: {repo: 'git@github.com:acme/agent-context.git', branch: main, subdir: teams/billing}
  dotfiles: {dir: ~/dev/dotfiles}
mcp-servers:
  linear: {url: 'https://mcp.linear.app/mcp'}
bindings:
  - name: me
    instructions: [~/agent-context/me.md, 'dotfiles:agents/*.md', '!dotfiles:agents/draft-*.md']
    skills: ['dotfiles:skills/*']
  - name: iam
    profiles: [iam]
    instructions: ['iam-team:docs/conventions/*.md', 'iam-team:docs/*.md', '!iam-team:docs/draft-*.md', '!iam-team:docs/testing.md']
    skills: ['iam-team:skills/*']
    mcp-servers: ['iam-team:jira']
  - name: iam-repos
    location:
      repos: [github.com/acme/iam-*, {repo: github.com/acme/mono, subdirs: [services/iam/**]}, github.com/me/iam-playground, '!github.com/acme/iam-legacy']
    instructions: ['iam-team:docs/conventions/*.md']
  - name: obs
    location: {repos: [github.com/acme/**, '!github.com/acme/legacy']}
    profiles: [observability, oncall]
    instructions: ['obs-team:docs/conventions/*.md', 'obs-team:runbooks/*.md']
  - name: billing
    location: {repos: [{repo: github.com/acme/mono, subdirs: [services/billing/**]}]}
    instructions: ['iam-team:services/billing.md']
    skills: ['billing-ctx:skills/*']
  - name: security
    location: {dirs: [~/work/**, '!~/work/clients/**'], repos: [github.com/acme/**]}
    instructions: ['security:guidelines/*.md']
  - name: review
    agents: [claude-code, codex]
    skills: ['dotfiles:skills/review/*']
  - name: go
    location: {dirs: [~/dev/**]}
    profiles: [go-deep]
    agents: [claude-code]
    instructions: [go-notes.md, 'go-style:go/*.md']
    skills: ['dotfiles:skills/go-*']
  - name: go-work
    location: {repos: [github.com/acme/**]}
    instructions: [go-notes.md, 'go-style:go/*.md']
    skills: ['dotfiles:skills/go-*']
  - name: tracker
    location: {dirs: [~/work/**]}
    profiles: ['!oncall']
    agents: ['!cursor']
    mcp-servers: [linear]
max-ctx-chars: 400000
`

var demoProfiles = profile.Active{Names: []string{"observability"}}

// contentChars counts the text of the files and skills of res.
func contentChars(res *Result) int {
	n := 0
	for _, h := range slices.Concat(res.Files, res.Skills) {
		n += Chars(h.Content)
	}
	return n
}

func TestDemoConfig(t *testing.T) {
	d := newDemo(t)
	demoFile, err := os.ReadFile("../config/testdata/demo.yaml")
	if err != nil {
		t.Fatal(err)
	}

	// The demo's files are longer than its max-ctx-chars allows: with
	// room for them, the demo shows what each binding adds.
	roomy := strings.Replace(string(demoFile), "max-ctx-chars: 40000", "max-ctx-chars: 400000", 1)
	in := Input{Dir: d.session, Agent: "claude-code", Profiles: demoProfiles}
	allFiles := []string{"me.md", "me-work.md", "tools.md", "logs.md", "metrics.md", "traces.md", "alerts.md", "capacity.md",
		"errors.md", "latency.md", "paging.md", "billing.md", "deps.md", "review.md", "secrets.md", "go-notes.md", "style.md", "testing.md"}

	t.Run("the demo as written", func(t *testing.T) {
		res := Load(d.config(t, "config.yaml", roomy), agentNames).Resolve(in)
		eq(t, "applied", applied(res), "me", "obs", "billing", "security", "review", "go-work", "tracker")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), allFiles...)
		eq(t, "skills", skillNames(res), "commit", "go-test", "notes", "invoice", "refund", "go", "sql")
		eq(t, "servers", served(res), "linear")
		if s := Load(d.config(t, "config.yaml", roomy), agentNames).Catalog.Source("iam-dev"); s.Why != "~/dev/iam-ctx is not on this machine" {
			t.Errorf("iam-dev: %s", s.Why)
		}
	})

	t.Run("max-ctx-chars leaves out the last files", func(t *testing.T) {
		full := Load(d.config(t, "config.yaml", roomy), agentNames).Resolve(in)
		// The limit is the rendered text without go-work's last two files.
		short := *full
		short.Files = full.Files[:len(full.Files)-2]
		limit := Chars(short.Render())
		res := Load(d.config(t, "config.yaml", strings.Replace(roomy, "max-ctx-chars: 400000", fmt.Sprintf("max-ctx-chars: %d", limit), 1)), agentNames).Resolve(in)
		var errs []string
		for _, e := range res.Errors {
			errs = append(errs, e.Err.Msg)
		}
		eq(t, "errors", errs, "the text is over max-ctx-chars; 2 files of bindings go-work are left out")
		eq(t, "files", bases(res.Files), allFiles[:len(allFiles)-2]...)
		eq(t, "dropped", bases(res.Dropped), "style.md", "testing.md")
	})

	t.Run("the iam profile", func(t *testing.T) {
		in := Input{Dir: d.home, Agent: "codex", Profiles: profile.Active{Names: []string{"iam"}}}
		res := Load(d.config(t, "config.yaml", string(demoFile)), agentNames).Resolve(in)
		// iam-team:jira, a repo source's server, needs approval on this machine.
		for _, m := range res.MCPServers {
			if err := creds.Open().DecideApproval(m.Server, true); err != nil {
				t.Fatal(err)
			}
		}
		res = Load(d.config(t, "config.yaml", string(demoFile)), agentNames).Resolve(in)
		eq(t, "applied", applied(res), "me", "iam", "review")
		noErrors(t, res)
		// essentials: base's conventions, then docs/*.md without drafts,
		// and without testing.md, which the binding leaves out.
		eq(t, "files", bases(res.Files), "me.md", "me-work.md", "tools.md", "errors.md", "naming.md", "overview.md")
		eq(t, "skills", skillNames(res), "commit", "go-test", "notes", "login", "tokens", "go", "sql")
		eq(t, "servers", served(res), "jira")
	})

	t.Run("iam's part of the monorepo", func(t *testing.T) {
		dir := filepath.Join(d.home, "work", "mono", "services", "iam")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, c := range []string{string(demoFile), inlined} {
			res := Load(d.config(t, "config.yaml", c), agentNames).Resolve(Input{Dir: dir, Agent: "codex"})
			eq(t, "applied", applied(res), "me", "iam-repos", "security", "review", "go-work", "tracker")
		}
	})

	t.Run("inlined", func(t *testing.T) {
		res := Load(d.config(t, "config.yaml", inlined), agentNames).Resolve(Input{Dir: d.session, Agent: "claude-code", Profiles: demoProfiles})
		eq(t, "applied", applied(res), "me", "obs", "billing", "security", "review", "go-work", "tracker")
		noErrors(t, res)
		eq(t, "files", bases(res.Files), "me.md", "me-work.md", "tools.md", "logs.md", "metrics.md", "traces.md", "alerts.md", "capacity.md",
			"errors.md", "latency.md", "paging.md", "billing.md", "deps.md", "review.md", "secrets.md", "go-notes.md", "style.md", "testing.md")
		eq(t, "skills", skillNames(res), "commit", "go-test", "notes", "invoice", "refund", "go", "sql")
		if n := contentChars(res); n < 20000 {
			t.Errorf("%d characters", n)
		}
		if errs := errorsOf(res, yamlx.OverCapacity); len(errs) != 0 {
			t.Errorf("over capacity: %v", errs)
		}
	})
}

// BenchmarkResolve is what the hook does for a session start: load the
// config and resolve it for the session folder, here a clone of
// github.com/acme/mono. Both run git to identify the repository, which
// git-only measures alone; repository-known leaves it out.
func BenchmarkResolve(b *testing.B) {
	d := newDemo(b)
	demoFile, err := os.ReadFile("../config/testdata/demo.yaml")
	if err != nil {
		b.Fatal(err)
	}
	in := Input{Dir: d.session, Agent: "claude-code", Profiles: demoProfiles}
	for _, c := range []struct {
		name, config string
	}{{"demo", string(demoFile)}, {"inlined", inlined}} {
		b.Run(c.name, func(b *testing.B) {
			path := d.config(b, c.name+".yaml", c.config)
			for b.Loop() {
				if res := Load(path, agentNames).Resolve(in); len(res.Files) == 0 {
					b.Fatal("no files")
				}
			}
		})
		b.Run(c.name+"/repository-known", func(b *testing.B) {
			path := d.config(b, c.name+".yaml", c.config)
			in := in
			in.Dir = filepath.Join(d.home, "work", "mono", "services", "billing")
			in.Repo = Load(path, agentNames).Resolve(in).Repo
			for b.Loop() {
				Load(path, agentNames).Resolve(in)
			}
		})
	}
	b.Run("git-only", func(b *testing.B) {
		for b.Loop() {
			gitrepo.Identify(d.session)
		}
	})
}
