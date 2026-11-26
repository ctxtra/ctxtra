package config

import (
	"testing"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// TestErrorTable has a case for each row of the table of mistakes that
// parsing the config finds: what each one leaves out.
type row struct {
	name string
	// raw is the whole file; else the lines of each section.
	raw                                       string
	top, sources, collections, sets, bindings string
	mistake                                   yamlx.Mistake
	out                                       yamlx.Out
	check                                     func(t *testing.T, c *Config)
}

func TestErrorTable(t *testing.T) {
	// config is a config with a source, a server, a collection, a repo set
	// and a binding that stay when a row's part is left out, and the row's
	// lines in each section.
	config := func(r row) string {
		return "schema: ctxtra/config/v1\n" + r.top +
			"sources:\n  ok-src: {dir: ~/ok}\n" + r.sources +
			"mcp-servers:\n  ok_srv: {url: 'https://ok.example'}\n" +
			"collections:\n  ok-col: {skills: [a]}\n" + r.collections +
			"repo-sets:\n  ok-set: [github.com/ok/x]\n" + r.sets +
			"bindings:\n  - name: ok\n    skills: [a]\n" + r.bindings
	}
	kept := func(t *testing.T, c *Config) {
		t.Helper()
		for _, broken := range []bool{
			c.Source("ok-src") == nil || c.Source("ok-src").Broken(),
			c.MCPServer("ok_srv") == nil || c.MCPServer("ok_srv").Broken(),
			c.Collection("ok-col") == nil || c.Collection("ok-col").Broken(),
			c.RepoSet("ok-set") == nil || c.RepoSet("ok-set").Broken(),
			c.Binding("ok") == nil || c.Binding("ok").Broken(),
		} {
			if broken {
				t.Fatalf("a part without the mistake is left out: %v", c.Errors)
			}
		}
	}

	binding := func(name, body string, m yamlx.Mistake, out yamlx.Out) row {
		return row{name: name, bindings: "  - name: x\n    " + body + "\n", mistake: m, out: out,
			check: func(t *testing.T, c *Config) {
				if c.Binding("x").Broken() != (out == yamlx.OutPart) {
					t.Errorf("binding x left out: %v", c.Binding("x").Broken())
				}
			}}
	}
	whole := func(name, data string) row {
		return row{name: name, raw: data, mistake: yamlx.Unreadable, out: yamlx.OutFile, check: func(t *testing.T, c *Config) {
			if len(c.Sources)+len(c.Bindings) > 0 {
				t.Error("parts of an unreadable config are used")
			}
		}}
	}
	source := func(name, body string) row {
		return row{name: name, sources: "  s: " + body + "\n", mistake: yamlx.BrokenSource, out: yamlx.OutPart,
			check: func(t *testing.T, c *Config) {
				if !c.Source("s").Broken() {
					t.Error("the source is used")
				}
			}}
	}
	rows := []row{
		whole("the config can't be read: YAML syntax", "schema: ctxtra/config/v1\nbindings: [\n"),
		whole("the config can't be read: a wrong schema", "schema: ctxtra/lock/v1\n"),
		whole("the config can't be read: a missing schema", "bindings: []\n"),
		whole("the config can't be read: more than one document", "schema: ctxtra/config/v1\n---\nschema: ctxtra/config/v1\n"),
		whole("the config can't be read: an anchor", "schema: ctxtra/config/v1\nsources:\n  a: &a {dir: ~/a}\n"),
		whole("the config can't be read: an alias", "schema: ctxtra/config/v1\nx: &x [a]\nbindings:\n  - name: b\n    skills: *x\n"),
		whole("the config can't be read: a merge key", "schema: ctxtra/config/v1\nsources:\n  a:\n    <<: {dir: ~/a}\n"),
		{name: "a duplicate key", sources: "  s:\n    dir: ~/a\n    dir: ~/b\n", mistake: yamlx.DuplicateKey, out: yamlx.OutPart,
			check: func(t *testing.T, c *Config) {
				if !c.Source("s").Broken() {
					t.Error("the source is used")
				}
			}},
		{name: "a duplicate top-level key", top: "max-ctx-chars: 1\nmax-ctx-chars: 2\n", mistake: yamlx.DuplicateKey, out: yamlx.OutPart,
			check: func(t *testing.T, c *Config) {
				if c.MaxCtxChars != nil {
					t.Error("the key is used")
				}
			}},
		{name: "an unknown top-level key in the config", top: "include: [x.yaml]\n", mistake: yamlx.UnknownTopKey, out: yamlx.OutPart},
		{name: "a broken source: a bad name", sources: "  9s: {dir: ~/a}\n", mistake: yamlx.BrokenSource, out: yamlx.OutPart,
			check: func(t *testing.T, c *Config) {
				if !c.Source("9s").Broken() {
					t.Error("the source is used")
				}
			}},
		source("a broken source: repo with dir", "{repo: 'git@github.com:a/b.git', branch: main, dir: ~/a}"),
		source("a broken source: two of branch, tag, rev", "{repo: 'git@github.com:a/b.git', branch: main, rev: "+
			"9f8e7d6c5b4a39281706f5e4d3c2b1a098765432}"),
		source("a broken source: a rev that isn't a full commit ID", "{repo: 'git@github.com:a/b.git', rev: 9f8e7d6}"),
		source("a broken source: an unknown key", "{dir: ~/a, git: x}"),
		binding("a broken binding: a YAML tag", "agents:\n      - !cursor\n    skills: [a]", yamlx.Tag, yamlx.OutPart),
		binding("a broken binding: an unknown key", "global: true\n    skills: [a]", yamlx.BrokenBinding, yamlx.OutPart),
		binding("a broken binding: an invalid profile name", "profiles: ['on call']\n    skills: [a]", yamlx.BrokenBinding, yamlx.OutPart),
		binding("a broken binding: an unknown agent name", "agents: [claude]\n    skills: [a]", yamlx.BrokenBinding, yamlx.OutPart),
		{name: "two bindings with the same name", bindings: "  - name: x\n    skills: [a]\n  - name: x\n    skills: [b]\n",
			mistake: yamlx.DuplicateBinding, out: yamlx.OutPart, check: func(t *testing.T, c *Config) {
				if !c.Bindings[1].Broken() || !c.Bindings[2].Broken() {
					t.Error("not both bindings are left out")
				}
			}},
		binding("an entry: a path that leaves its source", "instructions: ['s:../x.md', b.md]", yamlx.BrokenEntry, yamlx.OutEntry),
		binding("an empty list in a condition", "profiles: []\n    skills: [a]", yamlx.EmptyCondition, yamlx.OutPart),
		binding("an empty map in a condition", "location: {}\n    skills: [a]", yamlx.EmptyCondition, yamlx.OutPart),
		binding("null in a condition", "location: {dirs: }\n    skills: [a]", yamlx.EmptyCondition, yamlx.OutPart),
		binding("an empty subdirs", "location: {repos: [{repo: github.com/a/b, subdirs: []}]}\n    skills: [a]", yamlx.EmptyCondition, yamlx.OutPart),
		{name: "an empty repo set matches nothing", sets: "  s: []\n", mistake: yamlx.EmptyRepoSet, out: yamlx.OutNothing},
		binding("a broken positive location entry: a relative folder", "location: {dirs: [work]}\n    skills: [a]", yamlx.BrokenLocation, yamlx.OutNothing),
		binding("a broken positive location entry: a bad pattern", "location: {repos: ['github.com/a/[x']}\n    skills: [a]", yamlx.BrokenLocation, yamlx.OutNothing),
		binding("a broken positive location entry: a bad subdirs pattern", "location: {repos: [{repo: github.com/a/b, subdirs: [/x]}]}\n    skills: [a]", yamlx.BrokenLocation, yamlx.OutNothing),
		binding("a broken positive location entry: a bad set name", "location: {repo-sets: ['s:a b']}\n    skills: [a]", yamlx.BrokenLocation, yamlx.OutNothing),
		binding("a broken ! location entry: a relative folder", "location: {dirs: ['!work']}\n    skills: [a]", yamlx.BrokenExclusion, yamlx.OutPart),
		binding("a broken ! location entry: a bad pattern", "location: {repos: ['!github.com/a/[x']}\n    skills: [a]", yamlx.BrokenExclusion, yamlx.OutPart),
		binding("a broken ! location entry: a bad set name", "location: {repo-sets: ['!s:a b']}\n    skills: [a]", yamlx.BrokenExclusion, yamlx.OutPart),
		{name: "an unknown key in a collection", collections: "  c: {skills: [a], skill: [b]}\n", mistake: yamlx.BrokenGroup, out: yamlx.OutPart,
			check: func(t *testing.T, c *Config) {
				if !c.Collection("c").Broken() {
					t.Error("the collection is used")
				}
			}},
		{name: "an unknown key in a repo set", sets: "  s: [{repo: github.com/a/b, subdir: [x]}]\n", mistake: yamlx.BrokenGroup, out: yamlx.OutPart,
			check: func(t *testing.T, c *Config) {
				if !c.RepoSet("s").Broken() {
					t.Error("the set is used")
				}
			}},
		binding("a number where text is expected", "profiles: [2024]\n    skills: [a]", yamlx.NotText, yamlx.OutPart),
		{name: "true where text is expected", sources: "  s: {repo: 'git@github.com:a/b.git', branch: true}\n", mistake: yamlx.NotText, out: yamlx.OutPart},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			data := r.raw
			if data == "" {
				data = config(r)
			}
			c := Parse([]byte(data), Options{Agents: agents})
			var found *yamlx.Error
			for _, e := range c.Errors {
				if e.Mistake == r.mistake {
					found = e
				} else if e.Mistake != yamlx.DuplicateKey || r.mistake != yamlx.BrokenSource {
					t.Errorf("another error: %v", e)
				}
			}
			if found == nil {
				t.Fatalf("no error of mistake %d in %v", r.mistake, c.Errors)
			}
			if found.Out() != r.out {
				t.Errorf("%v leaves out %d, want %d", found, found.Out(), r.out)
			}
			if r.raw == "" {
				kept(t, c)
			}
			if r.check != nil {
				r.check(t, c)
			}
		})
	}
}
