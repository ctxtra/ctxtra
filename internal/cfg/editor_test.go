package cfg

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// node is the YAML text s as a node.
func node(t *testing.T, s string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(s), &n); err != nil {
		t.Fatal(err)
	}
	return n.Content[0]
}

func editor(t *testing.T, data string) *Editor {
	t.Helper()
	e, err := NewEditor([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func demoEditor(t *testing.T) (*Editor, string) {
	t.Helper()
	data, err := os.ReadFile("testdata/demo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return editor(t, string(data)), string(data)
}

// bindingNames are the names of the bindings that e's config has, in order.
func bindingNames(t *testing.T, e *Editor) []string {
	t.Helper()
	var names []string
	for _, b := range check(t, e).Bindings {
		names = append(names, b.Name)
	}
	return names
}

// check parses e's config and fails on errors in what e changed.
func check(t *testing.T, e *Editor) *Config {
	t.Helper()
	if err := e.Check(Options{Agents: agents}); err != nil {
		t.Fatalf("%v\n%s", err, e.Bytes())
	}
	return Parse(e.Bytes(), Options{Agents: agents})
}

// onlyAdded fails unless after is before with the lines of added put in at
// one place.
func onlyAdded(t *testing.T, before, after, added string) {
	t.Helper()
	i := strings.Index(after, added)
	if i < 0 || after[:i]+after[i+len(added):] != before {
		t.Errorf("want only these lines added:\n%s\ngot:\n%s", added, after)
	}
}

func TestEditorBindings(t *testing.T) {
	t.Run("before another binding", func(t *testing.T) {
		e := editor(t, NewFile+"bindings:\n  - name: me\n    skills: [a]\n  - name: go\n    skills: [b]\n")
		before := string(e.Bytes())
		if _, err := e.Add(Bindings, "iam", node(t, "{profiles: [iam], collections: ['iam-team:essentials']}"), Place{Before: "go"}); err != nil {
			t.Fatal(err)
		}
		if got := bindingNames(t, e); !slices.Equal(got, []string{"me", "iam", "go"}) {
			t.Errorf("bindings %v", got)
		}
		onlyAdded(t, before, string(e.Bytes()), "  - name: iam\n    profiles: [iam]\n    collections: ['iam-team:essentials']\n")
	})
	t.Run("after another binding", func(t *testing.T) {
		e, _ := demoEditor(t)
		if _, err := e.Add(Bindings, "iam-work", node(t, "{collections: ['iam-team:essentials']}"), Place{After: "iam"}); err != nil {
			t.Fatal(err)
		}
		names := bindingNames(t, e)
		if i := slices.Index(names, "iam-work"); i < 1 || names[i-1] != "iam" || names[i+1] != "iam-repos" {
			t.Errorf("bindings %v", names)
		}
	})
	t.Run("last by default, apart by a blank line as the others", func(t *testing.T) {
		e, before := demoEditor(t)
		if _, err := e.Add(Bindings, "new", node(t, "{skills: ['dotfiles:skills/x']}"), Place{}); err != nil {
			t.Fatal(err)
		}
		if got := bindingNames(t, e); got[len(got)-1] != "new" {
			t.Errorf("bindings %v", got)
		}
		onlyAdded(t, before, string(e.Bytes()), "\n  - name: new\n    skills: ['dotfiles:skills/x']\n")
	})
	t.Run("changing a binding in place", func(t *testing.T) {
		e := editor(t, NewFile+"bindings:\n  - name: me\n    skills: [a]\n  # observability\n  - name: obs\n    skills: [b] # old\n  - name: go\n    skills: [c]\n")
		if err := e.Replace(Bindings, "obs", node(t, "{profiles: [oncall], skills: [d]}")); err != nil {
			t.Fatal(err)
		}
		want := NewFile + "bindings:\n  - name: me\n    skills: [a]\n  # observability\n  - name: obs\n    profiles: [oncall]\n    skills: [d]\n  - name: go\n    skills: [c]\n"
		if got := string(e.Bytes()); got != want {
			t.Errorf("got\n%s\nwant\n%s", got, want)
		}
	})
	t.Run("the binding name is taken", func(t *testing.T) {
		e, before := demoEditor(t)
		_, err := e.Add(Bindings, "iam", node(t, "{location: {repo-sets: ['iam-team:services']}, collections: ['iam-team:essentials']}"), Place{})
		var exists *ExistsError
		if !errors.As(err, &exists) || err.Error() != "a binding named iam already exists" {
			t.Errorf("err %v", err)
		}
		if string(e.Bytes()) != before {
			t.Error("the config changed")
		}
	})
	t.Run("the same entry written differently", func(t *testing.T) {
		e := editor(t, NewFile+"bindings:\n  - name: iam\n    profiles: [iam]\n    collections: ['iam-team:essentials']\n")
		before := string(e.Bytes())
		added, err := e.Add(Bindings, "iam", node(t, "collections:\n  - iam-team:essentials\nprofiles:\n  - \"iam\"\n"), Place{Before: "nothing"})
		if err != nil || added {
			t.Errorf("added %v, err %v", added, err)
		}
		if string(e.Bytes()) != before || e.Changed() {
			t.Error("the config changed")
		}
	})
	t.Run("a place that isn't there", func(t *testing.T) {
		e, _ := demoEditor(t)
		_, err := e.Add(Bindings, "x", node(t, "{skills: [a]}"), Place{After: "nothing"})
		var nf *NotFoundError
		if !errors.As(err, &nf) || err.Error() != "no binding named nothing in the config" {
			t.Errorf("err %v", err)
		}
	})
	t.Run("a binding with a name key", func(t *testing.T) {
		e, _ := demoEditor(t)
		if _, err := e.Add(Bindings, "x", node(t, "{name: x, skills: [a]}"), Place{}); err == nil {
			t.Error("no error")
		}
	})
	t.Run("one part of a monorepo", func(t *testing.T) {
		e, _ := demoEditor(t)
		b := node(t, "{location: {repos: [{repo: github.com/acme/mono, subdirs: [services/billing/**]}]}, instructions: ['iam-team:services/billing.md']}")
		if _, err := e.Add(Bindings, "billing2", b, Place{}); err != nil {
			t.Fatal(err)
		}
		got := check(t, e).Binding("billing2")
		if r := got.Location.Repos; len(r) != 1 || r[0].Repo != "github.com/acme/mono" || r[0].Subdirs[0].Text != "services/billing/**" {
			t.Errorf("location %+v", got.Location)
		}
	})
	t.Run("removing a binding keeps the others' comments", func(t *testing.T) {
		e, before := demoEditor(t)
		if err := e.Remove(Bindings, "iam-preview"); err != nil {
			t.Fatal(err)
		}
		removed := "  # Trying my clone instead of the team's version: CTXTRA_PROFILES=iam-preview claude\n" +
			"  # In team repos, iam-repos above still adds iam-team:base: bindings only add.\n" +
			"  - name: iam-preview\n    profiles: [iam-preview]\n    collections: ['iam-dev:essentials']\n\n"
		onlyAdded(t, string(e.Bytes()), before, removed)
	})
	t.Run("removing the last binding removes the key", func(t *testing.T) {
		e := editor(t, NewFile+"bindings:\n  - name: me\n    skills: [a]\nmax-ctx-chars: 100\n")
		if err := e.Remove(Bindings, "me"); err != nil {
			t.Fatal(err)
		}
		if got, want := string(e.Bytes()), NewFile+"max-ctx-chars: 100\n"; got != want {
			t.Errorf("got\n%s\nwant\n%s", got, want)
		}
	})
}

func TestEditorMaps(t *testing.T) {
	t.Run("a source added last", func(t *testing.T) {
		e, before := demoEditor(t)
		if _, err := e.Add(Sources, "team", node(t, "{repo: 'git@github.com:acme/team.git', branch: main}"), Place{}); err != nil {
			t.Fatal(err)
		}
		onlyAdded(t, before, string(e.Bytes()), "\n  team:\n    repo: git@github.com:acme/team.git\n    branch: main\n")
		if s := check(t, e).Source("team"); s == nil || s.Repo != "git@github.com:acme/team.git" || s.Ref.Name != "main" {
			t.Errorf("source %+v", s)
		}
	})
	t.Run("my own set from the command line", func(t *testing.T) {
		e := editor(t, "")
		if _, err := e.Add(RepoSets, "acme", node(t, "['github.com/acme/**']"), Place{}); err != nil {
			t.Fatal(err)
		}
		if got, want := string(e.Bytes()), NewFile+"repo-sets:\n  acme:\n    - 'github.com/acme/**'\n"; got != want {
			t.Errorf("got\n%s\nwant\n%s", got, want)
		}
	})
	t.Run("a set of two monorepo parts", func(t *testing.T) {
		e, _ := demoEditor(t)
		set := node(t, "[{repo: github.com/acme/a, subdirs: [api/**]}, {repo: github.com/acme/b, subdirs: [web/**]}]")
		if _, err := e.Add(RepoSets, "products", set, Place{}); err != nil {
			t.Fatal(err)
		}
		got, _ := e.Entry(RepoSets, "products")
		if !yamlx.Same(got, set) {
			t.Errorf("the set isn't as written:\n%s", e.Bytes())
		}
		check(t, e)
	})
	t.Run("a source's server under another name", func(t *testing.T) {
		e, _ := demoEditor(t)
		if _, err := e.Add(MCPServers, "jira_iam", node(t, "{use: 'iam-team:jira'}"), Place{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(e.Bytes()), "  jira_iam:\n    use: iam-team:jira\n") {
			t.Errorf("got\n%s", e.Bytes())
		}
		if s := check(t, e).MCPServer("jira_iam"); s == nil || s.Use() != "iam-team:jira" {
			t.Errorf("server %+v", s)
		}
	})
	t.Run("a new key goes in the config's order", func(t *testing.T) {
		e := editor(t, "# my config\nschema: ctxtra/config/v1\n\nsources:\n  a: {dir: ~/a}\n\nbindings:\n  - {name: me, skills: ['a:x']}\n")
		if _, err := e.Add(Collections, "kit", node(t, "{skills: ['a:y']}"), Place{}); err != nil {
			t.Fatal(err)
		}
		want := "# my config\nschema: ctxtra/config/v1\n\nsources:\n  a: {dir: ~/a}\n\ncollections:\n  kit:\n    skills: ['a:y']\n\nbindings:\n  - {name: me, skills: ['a:x']}\n"
		if got := string(e.Bytes()); got != want {
			t.Errorf("got\n%s\nwant\n%s", got, want)
		}
	})
	t.Run("replacing a source keeps its place and comment", func(t *testing.T) {
		e, before := demoEditor(t)
		if err := e.Replace(Sources, "obs-team", node(t, "{repo: 'https://github.com/acme/observability-ctx.git', tag: v1.5.0}")); err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(before, "tag: v1.4.0", "tag: v1.5.0", 1)
		if got := string(e.Bytes()); got != want {
			t.Errorf("got\n%s", got)
		}
	})
	t.Run("removing a source", func(t *testing.T) {
		e, before := demoEditor(t)
		if err := e.Remove(Sources, "security"); err != nil {
			t.Fatal(err)
		}
		removed := "  # Frozen at one commit. Nothing to review until you edit rev.\n  security:\n" +
			"    repo: git@github.com:acme/security-guidelines.git\n    rev: 9f8e7d6c5b4a39281706f5e4d3c2b1a098765432\n\n"
		onlyAdded(t, string(e.Bytes()), before, removed)
	})
	t.Run("removing the last source", func(t *testing.T) {
		e := editor(t, NewFile+"\n# mine\nsources:\n  a:\n    dir: ~/a\n\nmax-ctx-chars: 1\n")
		if err := e.Remove(Sources, "a"); err != nil {
			t.Fatal(err)
		}
		if got, want := string(e.Bytes()), NewFile+"\nmax-ctx-chars: 1\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("a name that isn't there", func(t *testing.T) {
		e, _ := demoEditor(t)
		var nf *NotFoundError
		if err := e.Remove(Collections, "nothing"); !errors.As(err, &nf) {
			t.Errorf("remove: %v", err)
		}
		if err := e.Replace(Sources, "nothing", node(t, "{dir: ~/x}")); !errors.As(err, &nf) {
			t.Errorf("replace: %v", err)
		}
	})
	t.Run("a map entry has no place", func(t *testing.T) {
		e, _ := demoEditor(t)
		if _, err := e.Add(Sources, "x", node(t, "{dir: ~/x}"), Place{Before: "dotfiles"}); err == nil {
			t.Error("no error")
		}
	})
}

// Sections that the text edit can't handle are written again as a whole,
// and the result is still what was intended.
func TestEditorSectionStyles(t *testing.T) {
	tests := []struct{ name, data string }{
		{"empty flow map", NewFile + "sources: {} # none yet\nbindings: []\n"},
		{"null", NewFile + "sources:\nbindings:\n"},
		{"flow map with entries", NewFile + "sources: {a: {dir: ~/a}}\nbindings: [{name: me, skills: ['a:x']}]\n"},
		{"flow map over lines", NewFile + "sources: {\n  a: {dir: ~/a},\n}\nbindings: [\n  {name: me, skills: ['a:x']},\n]\n"},
		{"a flow config", "{schema: ctxtra/config/v1, sources: {a: {dir: ~/a}}}\n"},
		{"items at the key's indentation", NewFile + "sources:\n  a:\n    dir: ~/a\nbindings:\n- name: me\n  skills: ['a:x']\n"},
		{"a dash on its own line", NewFile + "bindings:\n  -\n    name: me\n    skills: ['a:x']\n"},
		{"a block scalar", NewFile + "sources:\n  a:\n    dir: |-\n      ~/a\nbindings:\n  - name: me\n    skills:\n      - |+\n        a:x\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := editor(t, tt.data)
			if _, err := e.Add(Sources, "b", node(t, "{repo: 'https://x.example/b.git', branch: main, check-updates: false}"), Place{}); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Add(Bindings, "first", node(t, "{skills: ['b:y']}"), Place{Before: "me"}); err != nil && !strings.Contains(tt.data, "name: me") {
				// a config without the binding me
			} else if err != nil {
				t.Fatal(err)
			}
			if _, err := e.Add(Bindings, "last", node(t, "{skills: ['b:z']}"), Place{}); err != nil {
				t.Fatal(err)
			}
			c := check(t, e)
			if s := c.Source("b"); s == nil || s.Repo != "https://x.example/b.git" || s.CheckUpdates == nil || *s.CheckUpdates {
				t.Errorf("source b %+v\n%s", s, e.Bytes())
			}
			if err := e.Remove(Bindings, "last"); err != nil {
				t.Fatal(err)
			}
			if err := e.Remove(Sources, "b"); err != nil {
				t.Fatal(err)
			}
			check(t, e)
		})
	}
}

func TestEditorRefuses(t *testing.T) {
	t.Run("a config YAML can't read", func(t *testing.T) {
		_, err := NewEditor([]byte(NewFile + "bindings:\n  - name: me\n   skills: [a]\n"))
		if err == nil || !strings.Contains(err.Error(), "fix it first") {
			t.Errorf("err %v", err)
		}
	})
	t.Run("a section of the wrong kind", func(t *testing.T) {
		e := editor(t, NewFile+"sources: [a]\n")
		if _, err := e.Add(Sources, "x", node(t, "{dir: ~/x}"), Place{}); err == nil || !strings.Contains(err.Error(), "sources in the config must be a map") {
			t.Errorf("err %v", err)
		}
	})
	t.Run("a name twice", func(t *testing.T) {
		e := editor(t, NewFile+"bindings:\n  - {name: a, skills: [x]}\n  - {name: a, skills: [y]}\n")
		if err := e.Remove(Bindings, "a"); err == nil || !strings.Contains(err.Error(), "binding a twice") {
			t.Errorf("err %v", err)
		}
	})
	t.Run("an unquoted exclusion from a file", func(t *testing.T) {
		e, before := demoEditor(t)
		var n yaml.Node
		if err := yaml.Unmarshal([]byte("agents:\n  - !cursor\n  - codex\nskills: [a]\n"), &n); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Add(Bindings, "x", &n, Place{}); err == nil || !strings.Contains(err.Error(), "quote") {
			t.Errorf("err %v", err)
		}
		if string(e.Bytes()) != before {
			t.Error("the config changed")
		}
	})
}

func TestEditorCheck(t *testing.T) {
	e := editor(t, NewFile+"bindings:\n  - name: old\n    agents: [nobody]\n    skills: [a]\n")
	if err := e.Check(Options{Agents: agents}); err != nil {
		t.Errorf("an error the editor didn't write: %v", err)
	}
	if _, err := e.Add(Bindings, "new", node(t, "{agents: [nobody], skills: [a]}"), Place{}); err != nil {
		t.Fatal(err)
	}
	err := e.Check(Options{Agents: agents})
	if err == nil || !strings.Contains(err.Error(), "binding new: unknown agent") || strings.Contains(err.Error(), "binding old") {
		t.Errorf("err %v", err)
	}
	// A text value that YAML would read as a number is written quoted, so
	// the source has no error.
	if _, err := e.Add(Sources, "s", node(t, "{repo: 'https://x.example/s.git', tag: '1.10'}"), Place{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(e.Bytes()), "tag: '1.10'") || strings.Contains(e.Check(Options{Agents: agents}).Error(), "source s") {
		t.Errorf("got\n%s", e.Bytes())
	}
}

// Every value that needs quotes, through every list of a binding and the
// keys of a source, reads back as written.
func TestEditorQuotes(t *testing.T) {
	values := []string{
		"!cursor", "!docs/draft-*.md", "1.10", "on", "yes", "no", "null", "~", "a: b", "a:", "a #b", "#a", "*x", "&x",
		" lead", "trail ", "'q'", `"dq"`, "x\ty", "x\ny", "@x", "%x", "?x", "- x", "[x]", "{x}", "a,b", "2001-01-01", "0x1F",
		"iam-team:essentials", "!iam-team:legacy",
	}
	keys := []string{"instructions", "skills", "collections", "mcp-servers", "profiles", "agents"}
	for _, v := range values {
		for _, flow := range []bool{false, true} {
			e, before := demoEditor(t)
			b := &yaml.Node{Kind: yaml.MappingNode}
			for _, k := range keys {
				list := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: v}}}
				if flow {
					list.Style = yaml.FlowStyle
				}
				b.Content = append(b.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, list)
			}
			if _, err := e.Add(Bindings, "x", b, Place{After: "me"}); err != nil {
				t.Fatalf("%q: %v", v, err)
			}
			src := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "repo"}, {Kind: yaml.ScalarNode, Value: v},
				{Kind: yaml.ScalarNode, Value: "branch"}, {Kind: yaml.ScalarNode, Value: v},
			}}
			if _, err := e.Add(Sources, "x", src, Place{}); err != nil {
				t.Fatalf("%q: %v", v, err)
			}
			d, perr := yamlx.Parse(e.Bytes(), yamlx.ConfigKind)
			if perr != nil {
				t.Fatalf("%q: %v", v, perr)
			}
			if errs := d.Check(yamlx.Part{}, d.Root); len(errs) > 0 {
				t.Errorf("%q: %v", v, errs)
			}
			got, _ := e.Entry(Bindings, "x")
			for i := 2; i+1 < len(got.Content); i += 2 {
				if n := got.Content[i+1].Content[0]; n.ShortTag() != "!!str" || n.Value != v {
					t.Errorf("%q in %s reads as %s %q", v, got.Content[i].Value, n.ShortTag(), n.Value)
				}
			}
			s, _ := e.Entry(Sources, "x")
			if s.Content[1].Value != v || s.Content[3].Value != v || s.Content[1].ShortTag() != "!!str" {
				t.Errorf("%q in a source reads as %q", v, s.Content[1].Value)
			}
			if e.Remove(Sources, "x") != nil || e.Remove(Bindings, "x") != nil || string(e.Bytes()) != before {
				t.Errorf("%q: removing what was added doesn't give back the file:\n%s", v, e.Bytes())
			}
		}
	}
}
