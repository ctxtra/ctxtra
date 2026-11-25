package cfg

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

var agents = []string{"claude-code", "codex", "cursor", "github-copilot", "opencode", "pi", "antigravity", "devin", "muse", "dsh", "hermes-agent"}

func parse(t *testing.T, body string) *Config {
	t.Helper()
	return Parse([]byte("schema: ctxtra/config/v1\n"+body), Options{Agents: agents})
}

// only returns the config's one error, and fails unless each of want is in
// it.
func only(t *testing.T, c *Config, want ...string) *yamlx.Error {
	t.Helper()
	if len(c.Errors) != 1 {
		t.Fatalf("errors %v, want one", c.Errors)
	}
	for _, w := range want {
		if !strings.Contains(c.Errors[0].Error(), w) {
			t.Errorf("error %q doesn't say %q", c.Errors[0], w)
		}
	}
	return c.Errors[0]
}

func noErrors(t *testing.T, c *Config) {
	t.Helper()
	for _, e := range c.Errors {
		t.Errorf("error: %v", e)
	}
}

func texts(list []Entry) []string {
	var out []string
	for _, e := range list {
		out = append(out, e.Text)
	}
	return out
}

func TestDemoConfig(t *testing.T) {
	data, err := os.ReadFile("testdata/demo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c := Parse(data, Options{Agents: agents})
	noErrors(t, c)

	var names []string
	for _, s := range c.Sources {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, " "); got != "iam-team obs-team security go-style iam-dev billing-ctx dotfiles" {
		t.Errorf("sources %s", got)
	}
	if s := c.Source("obs-team"); s.Repo != "https://github.com/acme/observability-ctx.git" || s.Ref != (Ref{Tag, "v1.4.0"}) {
		t.Errorf("obs-team %+v", s)
	}
	if s := c.Source("security"); s.Ref != (Ref{Rev, "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432"}) {
		t.Errorf("security %+v", s)
	}
	if s := c.Source("go-style"); s.CheckUpdates == nil || *s.CheckUpdates {
		t.Errorf("go-style check-updates %v", s.CheckUpdates)
	}
	if s := c.Source("billing-ctx"); s.Subdir != "teams/billing" || s.Ref != (Ref{Branch, "main"}) {
		t.Errorf("billing-ctx %+v", s)
	}
	if s := c.Source("dotfiles"); s.Dir != "~/dev/dotfiles" || s.Repo != "" {
		t.Errorf("dotfiles %+v", s)
	}

	names = nil
	for _, b := range c.Bindings {
		names = append(names, b.Name)
	}
	if got := strings.Join(names, " "); got != "me iam iam-repos obs billing security review go go-work iam-preview tracker" {
		t.Errorf("bindings in the order of the list: %s", got)
	}
	me := c.Binding("me")
	if me.Location != nil || me.Profiles != nil || me.Agents != nil {
		t.Errorf("me has conditions: %+v", me)
	}
	if got := me.Instructions; len(got) != 3 || got[0].Source != "" || got[1].Source != "dotfiles" || got[1].Value != "agents/*.md" ||
		!got[2].Not || got[2].Source != "dotfiles" || got[2].Value != "agents/draft-*.md" {
		t.Errorf("me instructions %+v", got)
	}
	iam := c.Binding("iam")
	if iam.Collections[0].Source != "iam-team" || iam.Collections[0].Value != "essentials" || !iam.Instructions[0].Not {
		t.Errorf("iam %+v", iam)
	}
	repos := c.Binding("iam-repos").Location
	if repos.RepoSets[0].Source != "iam-team" || repos.RepoSets[0].Value != "services" || repos.Repos[0].Not || !repos.Repos[1].Not ||
		repos.Repos[1].Repo != "github.com/acme/iam-legacy" {
		t.Errorf("iam-repos %+v", repos)
	}
	billing := c.Binding("billing").Location.Repos[0]
	if billing.Repo != "github.com/acme/mono" || texts(billing.Subdirs)[0] != "services/billing/**" {
		t.Errorf("billing %+v", billing)
	}
	security := c.Binding("security").Location
	if len(security.Dirs) != 2 || !security.Dirs[1].Not || security.Dirs[1].Value != "~/work/clients/**" || len(security.Repos) != 1 {
		t.Errorf("security %+v", security)
	}
	tracker := c.Binding("tracker")
	if !tracker.Profiles[0].Not || tracker.Profiles[0].Value != "oncall" || !tracker.Agents[0].Not || tracker.MCPServers[0].Value != "linear" {
		t.Errorf("tracker %+v", tracker)
	}
	if kit := c.Collection("go-kit"); kit == nil || texts(kit.Instructions)[0] != "go-notes.md" || kit.Instructions[1].Source != "go-style" {
		t.Errorf("go-kit %+v", kit)
	}
	if set := c.RepoSet("acme"); set == nil || set.Entries[0].Repo != "github.com/acme/**" {
		t.Errorf("acme %+v", set)
	}
	if s := c.MCPServer("linear"); s == nil || s.URL != "https://mcp.linear.app/mcp" || s.Notes != "Choose the work workspace" {
		t.Errorf("linear %+v", s)
	}
	if *c.MaxCtxChars != 40000 || !*c.CheckReleases || *c.SessionMessage.MaxSkills != 0 || c.SessionMessage.MaxInstructions != nil {
		t.Errorf("settings %v %v %+v", *c.MaxCtxChars, *c.CheckReleases, c.SessionMessage)
	}
}

func TestSources(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string // the error; none when the source is fine
	}{
		{name: "a name that starts with a digit", body: "2024-notes: {dir: ~/notes}", want: []string{"doesn't start with a letter"}},
		{name: "a name with a space", body: "'my notes': {dir: ~/notes}", want: []string{"names are 1 to 64"}},
		{name: "a name of 65 characters", body: strings.Repeat("a", 65) + ": {dir: ~/notes}", want: []string{"longer than 64"}},
		{name: "a short rev", body: "s: {repo: 'git@github.com:acme/x.git', rev: 9f8e7d6}",
			want: []string{"rev 9f8e7d6 is not a full commit ID", "40 hex characters, or 64"}},
		{name: "a rev of a SHA-256 repository", body: "s: {repo: 'git@github.com:acme/x.git', rev: " + strings.Repeat("ab", 32) + "}"},
		{name: "branch and tag together", body: "s: {repo: 'git@github.com:acme/x.git', branch: main, tag: v1.4.0}",
			want: []string{"not branch and tag"}},
		{name: "no branch, tag or rev", body: "s: {repo: 'git@github.com:acme/x.git'}", want: []string{"needs one of branch, tag and rev"}},
		{name: "a folder with a branch", body: "s: {dir: ~/dev/dotfiles, branch: main}",
			want: []string{"a dir source has no branch: it is read as it is on disk"}},
		{name: "a folder with subdir", body: "s: {dir: ~/dev/dotfiles, subdir: x}", want: []string{"dir already names a folder"}},
		{name: "a folder with check-updates", body: "s: {dir: ~/dev/dotfiles, check-updates: false}", want: []string{"no check-updates"}},
		{name: "a relative folder", body: "s: {dir: dev/dotfiles}", want: []string{"absolute or start with ~"}},
		{name: "repo and dir", body: "s: {repo: 'git@github.com:acme/x.git', branch: main, dir: ~/x}", want: []string{"repo or dir, not both"}},
		{name: "neither repo nor dir", body: "s: {branch: main}", want: []string{"needs repo"}},
		{name: "an unknown key", body: "s: {dir: ~/x, comit: abc}", want: []string{`unknown key "comit"`}},
		{name: "a tag that looks like a number", body: "s: {repo: 'git@github.com:acme/x.git', tag: 1.10}",
			want: []string{"YAML reads 1.10 as a number", "tag: '1.10'"}},
		{name: "a branch of true", body: "s: {repo: 'git@github.com:acme/x.git', branch: true}", want: []string{"branch: 'true'"}},
		{name: "a key twice", body: "s:\n    repo: 'git@github.com:acme/x.git'\n    branch: main\n    branch: next",
			want: []string{"branch is there twice, lines 5 and 6"}},
		{name: "a subdir that leaves the repository", body: "s: {repo: 'git@github.com:acme/x.git', branch: main, subdir: ../x}",
			want: []string{"a folder inside the repository"}},
		{name: "an https address", body: "s: {repo: 'https://github.com/acme/x', branch: main}"},
		{name: "an ssh address with a port", body: "s: {repo: 'ssh://git@host:2222/acme/x.git', branch: main}"},
		{name: "a local clone", body: "s: {repo: ~/dev/iam-ctx, branch: my-change}"},
		{name: "an absolute local clone", body: "s: {repo: /srv/git/iam-ctx, branch: main}"},
		{name: "an http address", body: "s: {repo: 'http://github.com/acme/x', branch: main}", want: []string{"is not a git address"}},
		{name: "a relative path", body: "s: {repo: dev/x, branch: main}", want: []string{"is not a git address"}},
		{name: "a subdir of a repository", body: "s: {repo: 'git@github.com:acme/x.git', branch: main, subdir: teams/billing}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := parse(t, "sources:\n  "+tt.body+"\n  other: {dir: ~/other}\n")
			if c.Source("other") == nil || c.Source("other").Broken() {
				t.Errorf("another source is left out: %v", c.Errors)
			}
			if tt.want == nil {
				noErrors(t, c)
				return
			}
			err := only(t, c, tt.want...)
			if err.Out() != yamlx.OutPart || err.Part.Kind != "source" {
				t.Errorf("error %v leaves out %v %d, want the source", err, err.Part, err.Out())
			}
			if len(c.Sources) == 2 && !c.Sources[0].Broken() {
				t.Error("the source is used")
			}
		})
	}
}

func TestSourceNameTwice(t *testing.T) {
	c := parse(t, "sources:\n  a: {dir: ~/a}\n  b: {dir: ~/b}\n  a: {dir: ~/c}\n")
	only(t, c, "a is there twice, lines 3 and 5", "source a is left out")
	if c.Source("a") != nil || c.Source("b") == nil {
		t.Errorf("sources %+v", c.Sources)
	}
}

func TestRepoID(t *testing.T) {
	t.Run("from ssh to https", func(t *testing.T) {
		forms := []string{"git@github.com:acme/iam-ctx.git", "https://github.com/acme/iam-ctx", "ssh://git@github.com:22/Acme/iam-ctx.git/",
			"https://user@github.com/acme/iam-ctx.git"}
		for _, f := range forms {
			if got := RepoID(f); got != "github.com/acme/iam-ctx" {
				t.Errorf("RepoID(%s) = %s", f, got)
			}
		}
		if RepoID("git@github.com:other/iam-ctx.git") == RepoID(forms[0]) {
			t.Error("another repository is the same")
		}
	})
	t.Run("a local path as its real path", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "real")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		if RepoID(link) != RepoID(real+"/") {
			t.Errorf("RepoID(%s) = %s, RepoID(%s) = %s", link, RepoID(link), real, RepoID(real))
		}
	})
}

func TestBindings(t *testing.T) {
	tests := []struct {
		name string
		body string
		// want is the error, none when the binding is fine.
		want []string
		// out is what the error leaves out.
		out yamlx.Out
		// check checks the binding x.
		check func(t *testing.T, b *Binding)
	}{
		{name: "profile only", body: "profiles: [iam]\n    skills: [a]", check: func(t *testing.T, b *Binding) {
			if b.Location != nil || texts(b.Profiles)[0] != "iam" {
				t.Errorf("%+v", b)
			}
		}},
		{name: "a broken binding", body: "agents: [claude-cod]\n    skills: [a]", out: yamlx.OutPart,
			want: []string{`unknown agent "claude-cod"`, "known: claude-code, codex"}},
		{name: "unknown agent after !", body: "agents: ['!curser']\n    skills: [a]", out: yamlx.OutPart,
			want: []string{`unknown agent "curser"`, "known: claude-code"}},
		{name: "agents compared without case", body: "agents: [Claude-Code, '!CURSOR']\n    skills: [a]"},
		{name: "invalid name", body: "profiles: [iam, \"on call\"]\n    skills: [a]", out: yamlx.OutPart, want: []string{`profile "on call"`}},
		{name: "case", body: "profiles: [IAM, iam]\n    skills: [a]"},
		{name: "unquoted on its own line", body: "agents:\n      - !cursor\n      - codex\n    skills: [a]", out: yamlx.OutPart,
			want: []string{"'!cursor'"}},
		{name: "a broken folder entry", body: "location: {dirs: ['work/**']}\n    mcp-servers: [jira]", out: yamlx.OutNothing,
			want: []string{"work/** is a relative folder", "the entry matches nothing"}, check: func(t *testing.T, b *Binding) {
				if b.Location.Dirs[0].Err == nil {
					t.Error("the entry has no error, so it would match")
				}
			}},
		{name: "a bad folder pattern", body: "location: {dirs: ['~/work/[x']}\n    skills: [a]", out: yamlx.OutNothing,
			want: []string{"not a valid pattern"}},
		{name: "a broken folder exclusion", body: "location: {dirs: ['!clients/**']}\n    mcp-servers: [jira]", out: yamlx.OutPart,
			want: []string{"clients/** is a relative folder"}},
		{name: "the last repository removed by hand", body: "location: {repos: []}\n    mcp-servers: [jira]", out: yamlx.OutPart,
			want: []string{"repos is empty; remove location to apply everywhere, or remove the binding"}},
		{name: "an empty content list", body: "instructions: []\n    skills: ['dotfiles:skills/*']", check: func(t *testing.T, b *Binding) {
			if b.Instructions != nil || len(b.Skills) != 1 {
				t.Errorf("%+v", b.Content)
			}
		}},
		{name: "a null content list", body: "instructions:\n    skills: [a]"},
		{name: "a binding that adds nothing", body: "profiles: [iam]\n    instructions: []", out: yamlx.OutPart, want: []string{"adds nothing"}},
		{name: "! on the repo of an entry with subdirs", body: "location:\n      repos: [{repo: '!github.com/acme/mono', subdirs: [services/**]}]\n    skills: [a]",
			out: yamlx.OutPart, want: []string{"put ! in subdirs", "write the repository alone"}},
		{name: "two parts of a monorepo", body: "location:\n      repos: [{repo: github.com/acme/mono, subdirs: [services/iam/**, '!services/iam/old/**']}]\n    skills: [a]",
			check: func(t *testing.T, b *Binding) {
				if s := b.Location.Repos[0].Subdirs; len(s) != 2 || s[0].Not || !s[1].Not || s[1].Value != "services/iam/old/**" {
					t.Errorf("%+v", s)
				}
			}},
		{name: "a subdir that leaves the repository", body: "location:\n      repos: [{repo: github.com/acme/mono, subdirs: [../x]}]\n    skills: [a]",
			out: yamlx.OutNothing, want: []string{"leaves the repository"}, check: func(t *testing.T, b *Binding) {
				if r := b.Location.Repos[0]; r.Err != nil || r.Subdirs[0].Err == nil {
					t.Error("the subdirs entry would match, or the repository entry has its error")
				}
			}},
		{name: "a folder in repos", body: "location: {repos: [~/work/**]}\n    skills: [a]", out: yamlx.OutNothing, want: []string{"folders go in dirs"}},
		{name: "a repository address in repos", body: "location: {repos: ['git@github.com:acme/api.git', 'https://github.com/acme/*']}\n    skills: [a]"},
		{name: "only ! in collections", body: "collections: ['!iam-team:team-legacy']", out: yamlx.OutPart,
			want: []string{"collections has only ! entries"}},
		{name: "only ! in instructions, without collections", body: "instructions: ['!a.md']", out: yamlx.OutPart,
			want: []string{"instructions has only ! entries", "no collections"}},
		{name: "a file left out of a collection", body: "collections: ['iam-team:essentials']\n    instructions: ['!iam-team:docs/testing.md']"},
		{name: "collections by pattern, one left out", body: "collections: ['iam-team:team-*', '!iam-team:team-legacy']",
			check: func(t *testing.T, b *Binding) {
				if c := b.Collections; c[0].Value != "team-*" || !c[1].Not || c[1].Source != "iam-team" {
					t.Errorf("%+v", c)
				}
			}},
		{name: "a file name with a colon", body: "instructions: ['./draft:notes.md', '/tmp/draft:notes.md', 'draft:notes.md']",
			check: func(t *testing.T, b *Binding) {
				if got := b.Instructions; got[0].Source != "" || got[1].Source != "" || got[2].Source != "draft" || got[2].Value != "notes.md" {
					t.Errorf("%+v", got)
				}
			}},
		{name: "a raw file from a source", body: "instructions: ['iam-team:instructions/*.md']", check: func(t *testing.T, b *Binding) {
			if e := b.Instructions[0]; e.Source != "iam-team" || e.Value != "instructions/*.md" {
				t.Errorf("%+v", e)
			}
		}},
		{name: "leaving the source", body: "instructions: ['iam-team:../secrets.md', ~/notes/me.md]", out: yamlx.OutEntry,
			want: []string{"iam-team:../secrets.md: the path leaves its source"}, check: func(t *testing.T, b *Binding) {
				if b.Instructions[0].Err == nil || b.Instructions[1].Err != nil {
					t.Errorf("%+v", b.Instructions)
				}
			}},
		{name: "an absolute path in a source", body: "instructions: ['iam-team:/etc/x.md', a.md]", out: yamlx.OutEntry,
			want: []string{"relative to the top of the source"}},
		{name: "a bad source name", body: "skills: ['1team:x', a]", out: yamlx.OutEntry, want: []string{"names no source"}},
		{name: "a bad server name", body: "mcp-servers: [Jira]", out: yamlx.OutEntry, want: []string{`server name "Jira"`}},
		{name: "servers by pattern", body: "mcp-servers: ['iam-team:*', '!iam-team:deploy']"},
		{name: "a typo in a key", body: "locaton: {dirs: [~/work/**]}\n    skills: [a]", out: yamlx.OutPart, want: []string{`unknown key "locaton"`}},
		{name: "match is not a key", body: "match: {global: true}\n    skills: [a]", out: yamlx.OutPart, want: []string{`unknown key "match"`}},
		{name: "a number in a content list", body: "instructions: [1.10]", out: yamlx.OutPart, want: []string{"quote it: '1.10'"}},
		{name: "a repo set of a source and my own", body: "location: {repo-sets: ['iam-team:services', acme, '!corp:clients']}\n    skills: [a]",
			check: func(t *testing.T, b *Binding) {
				if s := b.Location.RepoSets; s[0].Source != "iam-team" || s[1].Source != "" || !s[2].Not || s[2].Source != "corp" {
					t.Errorf("%+v", s)
				}
			}},
		{name: "a broken repo set entry", body: "location: {repo-sets: ['iam-team:a/b']}\n    skills: [a]", out: yamlx.OutNothing, want: []string{"iam-team:a/b"}},
		{name: "a broken excluded repo set", body: "location: {repo-sets: ['!iam-team:a/b']}\n    skills: [a]", out: yamlx.OutPart, want: []string{"iam-team:a/b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := parse(t, "bindings:\n  - name: x\n    "+tt.body+"\n  - name: other\n    skills: [b]\n")
			if o := c.Binding("other"); o == nil || o.Broken() {
				t.Errorf("another binding is left out: %v", c.Errors)
			}
			b := c.Binding("x")
			if b == nil {
				t.Fatal("no binding x")
			}
			if tt.want == nil {
				noErrors(t, c)
			} else {
				err := only(t, c, tt.want...)
				if err.Out() != tt.out {
					t.Errorf("error %v leaves out %d, want %d", err, err.Out(), tt.out)
				}
				if got := b.Broken(); got != (tt.out == yamlx.OutPart) {
					t.Errorf("the binding is left out: %v", got)
				}
			}
			if tt.check != nil {
				tt.check(t, b)
			}
		})
	}
}

func TestBindingOrderAndNames(t *testing.T) {
	t.Run("order of text", func(t *testing.T) {
		c := parse(t, "bindings:\n  - name: me\n    skills: [a]\n  - name: iam\n    skills: [b]\n")
		noErrors(t, c)
		if c.Bindings[0].Name != "me" || c.Bindings[1].Name != "iam" {
			t.Errorf("bindings %v, %v", c.Bindings[0].Name, c.Bindings[1].Name)
		}
	})
	t.Run("duplicate name", func(t *testing.T) {
		c := parse(t, "bindings:\n  - name: iam\n    skills: [a]\n  - name: me\n    skills: [b]\n  - name: iam\n    skills: [c]\n")
		if len(c.Errors) != 2 {
			t.Fatalf("errors %v", c.Errors)
		}
		for _, e := range c.Errors {
			if !strings.Contains(e.Error(), "2 bindings are named iam, at lines 3 and 7") || e.Mistake != yamlx.DuplicateBinding {
				t.Errorf("error %v", e)
			}
		}
		if !c.Bindings[0].Broken() || c.Bindings[1].Broken() || !c.Bindings[2].Broken() {
			t.Error("not both bindings are left out, or the other one is")
		}
	})
	t.Run("a binding without a name", func(t *testing.T) {
		c := parse(t, "bindings:\n  - skills: [a]\n")
		only(t, c, "binding #1: a binding needs a name")
	})
}

func TestBindingsBelongToTheUser(t *testing.T) {
	c := parse(t, "include: [bindings.yaml]\nbindings:\n  - name: me\n    skills: [a]\n")
	err := only(t, c, `unknown key "include"`)
	if err.Mistake != yamlx.UnknownTopKey || err.Part != (yamlx.Part{Kind: "key", Name: "include"}) {
		t.Errorf("error %v", err)
	}
	if len(c.Bindings) != 1 || c.Bindings[0].Broken() {
		t.Error("the config's own bindings don't apply")
	}
}

func TestServers(t *testing.T) {
	t.Run("two teams' jira", func(t *testing.T) {
		c := parse(t, "mcp-servers:\n  jira_iam: {use: 'iam-team:jira'}\n  jira_obs: {use: 'obs-team:jira'}\n")
		noErrors(t, c)
		if s := c.MCPServer("jira_iam"); s.Use() != "iam-team:jira" || s.UseSource != "iam-team" || s.UseServer != "jira" {
			t.Errorf("%+v", s)
		}
	})
	t.Run("two names for one server", func(t *testing.T) {
		c := parse(t, "mcp-servers:\n  a: {use: 'iam-team:jira'}\n  b: {use: 'iam-team:jira'}\n  c: {use: 'obs-team:jira'}\n")
		if len(c.Errors) != 2 || !strings.Contains(c.Errors[0].Error(), "as b does") || !strings.Contains(c.Errors[1].Error(), "as a does") {
			t.Fatalf("errors %v", c.Errors)
		}
		if !c.MCPServer("a").Broken() || !c.MCPServer("b").Broken() || c.MCPServer("c").Broken() {
			t.Error("wrong servers left out")
		}
	})
	tests := []struct{ name, body, want string }{
		{"a local server", "s: {command: [deploy-mcp, --all], env: {A: '1'}, pass-env: [HOME], secret-env: [TOKEN]}", ""},
		{"a remote server", "s: {url: 'https://x.example/mcp', headers: {X-Team: a}, secret-headers: [Authorization], client-id: abc, scopes: [read]}", ""},
		{"env-pass is not a key", "s: {command: [x], env-pass: [HOME]}", `unknown key "env-pass"`},
		{"use with other keys", "s: {use: 'a:b', url: 'https://x.example'}", "a server with use has no other keys"},
		{"use without a source", "s: {use: jira}", "SOURCE:SERVER"},
		{"a bad server name", "Jira: {url: 'https://x.example'}", `server name "Jira"`},
		{"url and command", "s: {url: 'https://x.example', command: [x]}", "url or command, not both"},
		{"http to another machine", "s: {url: 'http://x.example'}", "need https"},
		{"a relative program", "s: {command: [./x]}", "not a relative path"},
		{"a local key on a remote server", "s: {url: 'https://x.example', env: {A: b}}", "env doesn't apply to a remote server"},
		{"a variable set twice", "s: {command: [x], env: {A: b}, pass-env: [A]}", "the variable A is set twice"},
		{"a number in env", "s: {command: [x], env: {PORT: 8080}}", "quote it: env.PORT: '8080'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := parse(t, "mcp-servers:\n  "+tt.body+"\n")
			if tt.want == "" {
				noErrors(t, c)
				return
			}
			if err := only(t, c, tt.want); err.Out() != yamlx.OutPart || !c.MCPServers[0].Broken() {
				t.Errorf("the server is used: %v", err)
			}
		})
	}
}

func TestConfigCollectionsAndRepoSets(t *testing.T) {
	t.Run("one set, two places", func(t *testing.T) {
		c := parse(t, "collections:\n  go-kit:\n    instructions: [go-notes.md, 'go-style:go/*.md']\n"+
			"bindings:\n  - name: go\n    location: {dirs: [~/dev/**]}\n    collections: [go-kit]\n"+
			"  - name: go-work\n    location: {repos: [github.com/acme/**]}\n    collections: [go-kit]\n")
		noErrors(t, c)
		for _, b := range c.Bindings {
			if e := b.Collections[0]; e.Source != "" || e.Value != "go-kit" {
				t.Errorf("%s: %+v", b.Name, e)
			}
		}
	})
	t.Run("my own set", func(t *testing.T) {
		c := parse(t, "repo-sets: {acme: [github.com/acme/**]}\n")
		noErrors(t, c)
	})
	t.Run("a folder in a set", func(t *testing.T) {
		c := parse(t, "repo-sets:\n  acme: [github.com/acme/api, ~/work/**, github.com/acme/web]\n")
		err := only(t, c, "~/work/** is a folder; a set lists repositories only")
		if err.Out() != yamlx.OutEntry {
			t.Errorf("error %v leaves out %d", err, err.Out())
		}
		if s := c.RepoSet("acme"); s.Broken() || len(s.Entries) != 2 {
			t.Errorf("the other entries don't apply: %+v", s)
		}
	})
	t.Run("an empty repo set", func(t *testing.T) {
		c := parse(t, "repo-sets:\n  acme: []\n")
		err := only(t, c, "matches nothing")
		if err.Out() != yamlx.OutNothing || c.RepoSet("acme") == nil || c.RepoSet("acme").Broken() {
			t.Errorf("error %v, set %+v", err, c.RepoSet("acme"))
		}
	})
	t.Run("a set of two monorepo parts", func(t *testing.T) {
		c := parse(t, "repo-sets:\n  products: [{repo: github.com/acme/a, subdirs: [api/**]}, {repo: github.com/acme/b, subdirs: [web/**]}]\n")
		noErrors(t, c)
		if s := c.RepoSet("products"); len(s.Entries) != 2 || s.Entries[1].Subdirs[0].Value != "web/**" {
			t.Errorf("%+v", s)
		}
	})
	t.Run("an excluded repository in a set", func(t *testing.T) {
		c := parse(t, "repo-sets:\n  acme: [github.com/acme/**, '!github.com/acme/legacy']\n")
		noErrors(t, c)
		if e := c.RepoSet("acme").Entries[1]; !e.Not || e.Repo != "github.com/acme/legacy" {
			t.Errorf("%+v", e)
		}
	})
}

func TestSettings(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"turning off release checks", "check-releases: false", ""},
		{"no limit", "max-ctx-chars: 0", ""},
		{"yes is text in YAML 1.2", "check-releases: yes", "check-releases must be true or false"},
		{"a negative limit", "max-ctx-chars: -1", "a whole number of 0 or more"},
		{"a limit as text", "max-ctx-chars: lots", "a whole number of 0 or more"},
		{"show-skills is not a key", "session-message: {show-skills: false}", `unknown key "show-skills"`},
		{"budget is not a key", "budget: {max-chars: 1}", `unknown key "budget"`},
		{"check-updates is a source's key", "check-updates: false", `unknown key "check-updates"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := parse(t, tt.body+"\n")
			if tt.want == "" {
				noErrors(t, c)
				return
			}
			err := only(t, c, tt.want)
			if err.Out() != yamlx.OutPart || err.Part.Kind != "key" {
				t.Errorf("error %v leaves out %v", err, err.Part)
			}
			if c.MaxCtxChars != nil || c.CheckReleases != nil || c.SessionMessage != (SessionMessage{}) {
				t.Error("a wrong value is used")
			}
		})
	}
}

func TestCheckName(t *testing.T) {
	for _, ok := range []string{"a", "iam.essentials", "IAM", "a_b-c.1", strings.Repeat("x", 64)} {
		if err := CheckName(ok); err != nil {
			t.Errorf("CheckName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1a", "-a", "a b", "a,b", "a:b", "a/b", "!a", "+a", "é", strings.Repeat("x", 65)} {
		if CheckName(bad) == nil {
			t.Errorf("CheckName(%q) = nil", bad)
		}
	}
}

func TestErrorsInFileOrder(t *testing.T) {
	c := parse(t, "bindings:\n  - name: b\n    agents: [nope]\n    skills: [a]\nsources:\n  1x: {dir: ~/x}\nmax-ctx-chars: -1\n")
	var lines []int
	for _, e := range c.Errors {
		lines = append(lines, e.Pos.Line)
	}
	if !slices.IsSorted(lines) || len(lines) != 3 {
		t.Errorf("lines %v", lines)
	}
}
