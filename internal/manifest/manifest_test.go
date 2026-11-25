package manifest

import (
	"os"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

func parse(body string) *Manifest {
	return Parse([]byte("schema: ctxtra/source/v1\n" + body))
}

func TestDemoManifest(t *testing.T) {
	data, err := os.ReadFile("testdata/" + FileName)
	if err != nil {
		t.Fatal(err)
	}
	m := Parse(data)
	for _, e := range m.Errors {
		t.Errorf("error: %v", e)
	}
	ess := m.Collection("essentials")
	if ess == nil || ess.Collections[0].Value != "base" || ess.Collections[0].Source != "" || !ess.Instructions[1].Not ||
		ess.MCPServers[0].Value != "jira" || ess.Skills[0].Value != "skills/*" {
		t.Errorf("essentials %+v", ess)
	}
	if s := m.MCPServer("jira"); s == nil || s.URL != "https://mcp.atlassian.com/v1/mcp" || s.Notes == "" {
		t.Errorf("jira %+v", s)
	}
	set := m.RepoSet("services")
	if set == nil || len(set.Entries) != 3 || !set.Entries[1].Not || set.Entries[2].Repo != "github.com/acme/mono" ||
		set.Entries[2].Subdirs[0].Value != "services/iam/**" {
		t.Errorf("services %+v", set)
	}
}

func TestWholeManifest(t *testing.T) {
	tests := []struct{ name, data, want string }{
		{"bindings in a manifest", "schema: ctxtra/source/v1\ncollections: {a: {skills: [x]}}\nbindings:\n  - name: x\n    skills: [a]\n",
			"a manifest has no bindings: a source provides content, and the user's config binds it"},
		{"sources in a manifest", "schema: ctxtra/source/v1\nsources: {a: {dir: ~/a}}\n", "a manifest has no sources"},
		{"an alias in a manifest", "schema: ctxtra/source/v1\ncollections:\n  base: &b {instructions: [docs/*.md]}\n  essentials: *b\n",
			"the anchor &b"},
		{"a config saved as a manifest", "schema: ctxtra/config/v1\n", "this is a config, not a source manifest"},
		// YAML can't read this entry at all, so the whole manifest is left
		// out, not only the collection.
		{"unquoted in a flow list", "schema: ctxtra/source/v1\ncollections:\n  a: {skills: [x]}\n  essentials:\n    instructions: [docs/*.md, !docs/draft-*.md]\n",
			"quote the entry that starts with !: '!docs/draft-*.md'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Parse([]byte(tt.data))
			if len(m.Errors) != 1 || m.Errors[0].Out() != yamlx.OutFile || !strings.Contains(m.Errors[0].Error(), tt.want) {
				t.Fatalf("errors %v, want %q", m.Errors, tt.want)
			}
			if len(m.Collections)+len(m.MCPServers)+len(m.RepoSets) > 0 {
				t.Error("something from the manifest is used")
			}
		})
	}
}

func TestEntries(t *testing.T) {
	tests := []struct {
		name string
		body string // the collection a, next to the collection ok
		want string // the error, "" for none
		out  yamlx.Out
	}{
		{name: "another source in a manifest", body: "{collections: ['org:base', ok]}",
			want: "a manifest references only its own collections", out: yamlx.OutEntry},
		{name: "a file in another source", body: "{instructions: ['org:x.md', docs/a.md]}", want: "only files in its own source", out: yamlx.OutEntry},
		{name: "another source's server", body: "{mcp-servers: ['org:jira']}", want: "only its own servers", out: yamlx.OutEntry},
		{name: "an absolute path", body: "{instructions: [/etc/x.md, docs/a.md]}", want: "relative to the top of this source", out: yamlx.OutEntry},
		{name: "a home path", body: "{instructions: [~/x.md, docs/a.md]}", want: "relative to the top of this source", out: yamlx.OutEntry},
		{name: "a path that leaves the source", body: "{skills: [../x, skills/*]}", want: "the path leaves its source", out: yamlx.OutEntry},
		{name: "a path that stays in the source", body: "{skills: [skills/../other/x]}"},
		{name: "an empty source name", body: "{instructions: [':x.md', docs/a.md]}", want: "a manifest references only files in its own source", out: yamlx.OutEntry},
		{name: "a path that starts with ./", body: "{instructions: ['./draft:notes.md']}"},
		{name: "unquoted on its own line", body: "\n    instructions:\n      - docs/*.md\n      - !docs/draft-*.md",
			want: "'!docs/draft-*.md'", out: yamlx.OutPart},
		{name: "an unknown key", body: "{instructions: [a.md], description: x}", want: `unknown key "description"`, out: yamlx.OutPart},
		{name: "only ! entries without nested collections", body: "{instructions: ['!a.md']}", want: "only ! entries", out: yamlx.OutPart},
		{name: "only ! entries with nested collections", body: "{collections: [ok], instructions: ['!a.md']}"},
		{name: "a team set on top of an org set", body: "{collections: [ok], instructions: [iam/*.md, '!org/legacy.md']}"},
		{name: "a name with a dot", body: "{collections: [iam.essentials]}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := parse("collections:\n  ok: {instructions: [docs/*.md]}\n  a: " + tt.body + "\n")
			if c := m.Collection("ok"); c == nil || c.Broken() {
				t.Error("another collection is left out")
			}
			if tt.want == "" {
				for _, e := range m.Errors {
					t.Errorf("error: %v", e)
				}
				return
			}
			if len(m.Errors) != 1 || !strings.Contains(m.Errors[0].Error(), tt.want) || m.Errors[0].Out() != tt.out {
				t.Fatalf("errors %v, want %q leaving out %d", m.Errors, tt.want, tt.out)
			}
			if m.Collection("a").Broken() != (tt.out == yamlx.OutPart) {
				t.Error("the collection is left out, or not")
			}
		})
	}
}

func TestRepoSets(t *testing.T) {
	t.Run("a folder in a set", func(t *testing.T) {
		m := parse("repo-sets:\n  services: [github.com/acme/iam-api, ~/work/**]\n")
		if len(m.Errors) != 1 || !strings.Contains(m.Errors[0].Error(), "a set lists repositories only") || m.Errors[0].Out() != yamlx.OutEntry {
			t.Fatalf("errors %v", m.Errors)
		}
		if s := m.RepoSet("services"); s.Broken() || len(s.Entries) != 2 || s.Entries[0].Err != nil || s.Entries[1].Err == nil {
			t.Errorf("the other entries don't apply, or the broken one isn't kept with its error: %+v", s)
		}
	})
	t.Run("! on the repo of an entry with subdirs", func(t *testing.T) {
		m := parse("repo-sets:\n  s: [{repo: '!github.com/acme/mono', subdirs: [x/**]}, github.com/acme/api]\n")
		if s := m.RepoSet("s"); len(m.Errors) != 1 || m.Errors[0].Out() != yamlx.OutEntry || s.Broken() || s.Entries[0].Err == nil || s.Entries[1].Err != nil {
			t.Fatalf("errors %v, set %+v", m.Errors, m.RepoSet("s"))
		}
	})
	t.Run("malformed fields in a set entry", func(t *testing.T) {
		for _, item := range []string{"{repo: [github.com/acme/web]}", "{repo: github.com/acme/web, subdirs: src/**}", "{repo: github.com/acme/web, subdirs: [[]]}"} {
			m := parse("repo-sets:\n  services: [github.com/acme/api, " + item + "]\n")
			s := m.RepoSet("services")
			if len(m.Errors) != 1 || m.Errors[0].Out() != yamlx.OutEntry || s.Broken() || len(s.Entries) != 2 ||
				s.Entries[0].Repo != "github.com/acme/api" || s.Entries[0].Err != nil || s.Entries[1].Err == nil {
				t.Errorf("%s: errors %v, set %+v", item, m.Errors, s)
			}
		}
	})
	t.Run("an unknown key in a set entry", func(t *testing.T) {
		m := parse("repo-sets:\n  s: [{repo: github.com/acme/mono, paths: [x/**]}]\n  ok: [github.com/acme/api]\n")
		if len(m.Errors) != 1 || !m.RepoSet("s").Broken() || m.RepoSet("ok").Broken() {
			t.Fatalf("errors %v", m.Errors)
		}
	})
}

func TestServers(t *testing.T) {
	t.Run("a server declared in a manifest", func(t *testing.T) {
		m := parse("mcp-servers:\n  deploy: {command: [deploy-mcp, --all], pass-env: [HOME]}\n")
		if len(m.Errors) > 0 || m.MCPServer("deploy").Command[1] != "--all" {
			t.Fatalf("errors %v", m.Errors)
		}
	})
	t.Run("use only in the config", func(t *testing.T) {
		m := parse("mcp-servers:\n  jira2: {use: 'other:jira'}\n")
		if len(m.Errors) != 1 || !strings.Contains(m.Errors[0].Error(), `unknown key "use"`) || !m.MCPServer("jira2").Broken() {
			t.Fatalf("errors %v", m.Errors)
		}
	})
}

func TestUnknownTopLevelKey(t *testing.T) {
	m := parse("collections: {a: {skills: [x]}}\nsuggestions: [x]\n")
	if len(m.Errors) != 1 || m.Errors[0].Mistake != yamlx.UnknownTopKey || m.Collection("a") == nil {
		t.Fatalf("errors %v", m.Errors)
	}
}
