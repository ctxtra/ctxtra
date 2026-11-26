package config

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
	t.Run("true or false written differently", func(t *testing.T) {
		e := editor(t, NewFile+"sources:\n  a:\n    repo: https://x.example/a.git\n    branch: main\n    check-updates: FALSE\n")
		added, err := e.Add(Sources, "a", node(t, "{repo: 'https://x.example/a.git', branch: main, check-updates: false}"), Place{})
		if err != nil || added || e.Changed() {
			t.Errorf("added %v, err %v", added, err)
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

// Empty sections get their first entry on the key's line, with the line's
// comment kept.
func TestEditorEmptySections(t *testing.T) {
	tests := []struct{ name, data, want string }{
		{"empty flow map and list", NewFile + "sources: {} # none yet\nbindings: []\n",
			NewFile + "sources: # none yet\n  b:\n    dir: ~/b\nbindings:\n  - name: me\n    skills: ['b:x']\n"},
		{"null", NewFile + "sources:\nbindings: ~\nmax-ctx-chars: 1\n",
			NewFile + "sources:\n  b:\n    dir: ~/b\nbindings:\n  - name: me\n    skills: ['b:x']\nmax-ctx-chars: 1\n"},
		{"no section yet", "# mine\nschema: ctxtra/config/v1\nmax-ctx-chars: 1\n",
			"# mine\nschema: ctxtra/config/v1\nsources:\n  b:\n    dir: ~/b\nbindings:\n  - name: me\n    skills: ['b:x']\nmax-ctx-chars: 1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := editor(t, tt.data)
			if _, err := e.Add(Sources, "b", node(t, "{dir: ~/b}"), Place{}); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Add(Bindings, "me", node(t, "{skills: ['b:x']}"), Place{}); err != nil {
				t.Fatal(err)
			}
			if got := string(e.Bytes()); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
			check(t, e)
		})
	}
}

// Layouts that can't be changed line by line are refused: the text stays
// as it is, and the error says what to write in block style.
func TestEditorRefusesLayouts(t *testing.T) {
	tests := []struct {
		name, data string
		s          Section
		want       string
	}{
		{"a flow section", NewFile + "sources: {a: {dir: ~/a}}\n", Sources, "write sources in block style"},
		{"a flow section over lines", NewFile + "bindings: [\n  {name: me, skills: ['a:x']},\n]\n", Bindings, "write bindings in block style"},
		{"an empty section over lines", NewFile + "sources: {\n}\n", Sources, "write sources in block style"},
		{"a flow config", "{schema: ctxtra/config/v1, sources: {a: {dir: ~/a}}}\n", Sources, "top-level keys in block style"},
		{"a flow config over lines", "{\n  schema: ctxtra/config/v1,\n  bindings: []\n}\n", Bindings, "top-level keys in block style"},
		{"a dash on its own line", NewFile + "bindings:\n  -\n    name: me\n    skills: ['a:x']\n", Bindings, "start each entry on a line of its own (- name: ...)"},
		{"a block scalar that keeps its last line break", NewFile + "bindings:\n  - name: me\n    skills:\n      - |+\n        a:x\n\n", Bindings, "make the change by hand"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := editor(t, tt.data)
			before := string(e.Bytes())
			entry := "{dir: ~/b}"
			if tt.s == Bindings {
				entry = "{skills: ['b:y']}"
			}
			_, err := e.Add(tt.s, "b", node(t, entry), Place{})
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "nothing changed") {
				t.Errorf("err %v", err)
			}
			if string(e.Bytes()) != before || e.Changed() {
				t.Errorf("the text changed:\n%s", e.Bytes())
			}
		})
	}
}

// An entry that the user wrote with an unquoted ! keeps its tag, and so
// stays left out, whatever the editor writes next to it.
func TestEditorKeepsUntouchedTags(t *testing.T) {
	old := NewFile + "bindings:\n  - name: old\n    instructions: [! ~/old.md]\n"
	e := editor(t, old)
	if _, err := e.Add(Bindings, "new", node(t, "{instructions: [~/new.md]}"), Place{}); err != nil {
		t.Fatal(err)
	}
	onlyAdded(t, old, string(e.Bytes()), "  - name: new\n    instructions: [~/new.md]\n")
	c := Parse(e.Bytes(), Options{Agents: agents})
	if b := c.Binding("old"); b == nil || !b.Broken() {
		t.Errorf("binding old: %+v\n%s", b, e.Bytes())
	}
	// In a flow section the editor can't add next to it without writing it
	// again, so it refuses.
	flow := NewFile + "bindings: [{name: old, instructions: [! ~/old.md]}]\n"
	e = editor(t, flow)
	if _, err := e.Add(Bindings, "new", node(t, "{instructions: [~/new.md]}"), Place{}); err == nil || string(e.Bytes()) != flow {
		t.Errorf("err %v\n%s", err, e.Bytes())
	}
}

// Every line the editor doesn't change stays byte for byte as it was:
// comments of each kind, alignment, blank lines and odd spacing.
func TestEditorKeepsBytes(t *testing.T) {
	data := "# my config\nschema:   ctxtra/config/v1   # the kind\n\n" +
		"sources:\n" +
		"  # the team\n" +
		"  a: # keep this explanation\n" +
		"    repo:   'https://x.example/a.git'    # quoted on purpose\n" +
		"    branch: \"main\"\n" +
		"  b:\n    dir: ~/b\n" +
		"  # c is gone for now\n" +
		"\n" +
		"bindings:\n" +
		"- name: me          # mine\n" +
		"  skills: [ 'b:x' ]\n" +
		"\n" +
		"- name: go\n" +
		"  instructions:\n" +
		"    - b:go.md       # aligned\n" +
		"    - '!b:draft.md'\n" +
		"# the end\n"
	t.Run("replacing an entry keeps the comment on its key line", func(t *testing.T) {
		e := editor(t, data)
		if err := e.Replace(Sources, "a", node(t, "{repo: 'https://x.example/a.git', tag: v2}")); err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(data, "  a: # keep this explanation\n    repo:   'https://x.example/a.git'    # quoted on purpose\n    branch: \"main\"\n",
			"  a: # keep this explanation\n    repo: https://x.example/a.git\n    tag: v2\n", 1)
		if got := string(e.Bytes()); got != want {
			t.Errorf("got\n%s\nwant\n%s", got, want)
		}
		if err := e.Replace(Bindings, "me", node(t, "{skills: ['b:y']}")); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(e.Bytes()), "- name: me # mine\n  skills: ['b:y']\n") {
			t.Errorf("got\n%s", e.Bytes())
		}
	})
	t.Run("adding and removing leave the rest as it is", func(t *testing.T) {
		e := editor(t, data)
		if _, err := e.Add(Sources, "c", node(t, "{dir: ~/c}"), Place{}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Add(Bindings, "mid", node(t, "{skills: ['c:x']}"), Place{After: "me"}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Add(RepoSets, "acme", node(t, "['github.com/acme/**']"), Place{}); err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(data, "    dir: ~/b\n", "    dir: ~/b\n  c:\n    dir: ~/c\n", 1)
		want = strings.Replace(want, "  skills: [ 'b:x' ]\n\n", "  skills: [ 'b:x' ]\n\n- name: mid\n  skills: ['c:x']\n\n", 1)
		want = strings.Replace(want, "\nbindings:", "\nrepo-sets:\n  acme:\n    - 'github.com/acme/**'\n\nbindings:", 1)
		if got := string(e.Bytes()); got != want {
			t.Errorf("got\n%s\nwant\n%s", got, want)
		}
		for _, r := range []struct {
			s    Section
			name string
		}{{RepoSets, "acme"}, {Bindings, "mid"}, {Sources, "c"}} {
			if err := e.Remove(r.s, r.name); err != nil {
				t.Fatal(err)
			}
		}
		if got := string(e.Bytes()); got != data {
			t.Errorf("removing what was added doesn't give back the file:\n%s", got)
		}
	})
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

// An entry parsed from other text, as from --file, has positions in that
// text: they never count against the config's own lines.
func TestEditorEntryFromOtherText(t *testing.T) {
	data := "# Note: ! entries need quotes\nschema: ctxtra/config/v1\n"
	e := editor(t, data)
	n := node(t, "skills: [x]")
	if n.Line != 1 || n.Column != 1 {
		t.Fatalf("the parsed entry is at %d:%d; this test needs line 1", n.Line, n.Column)
	}
	if _, err := e.Add(Bindings, "me", n, Place{}); err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(Bindings, "me", node(t, "skills: [z]")); err != nil {
		t.Fatal(err)
	}
	if got, want := string(e.Bytes()), data+"bindings:\n  - name: me\n    skills: [z]\n"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A # inside a quoted value isn't a comment, also after a quote written
// twice; the real comment after it is kept.
func TestEditorReplaceKeepsCommentAfterQuotedHash(t *testing.T) {
	e := editor(t, NewFile+"sources:\n  a: {dir: '~/it''s # files'} # keep\n  b: {dir: \"~/b \\\" # x\"} # also\n")
	if err := e.Replace(Sources, "a", node(t, "{dir: ~/a}")); err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(Sources, "b", node(t, "{dir: ~/b}")); err != nil {
		t.Fatal(err)
	}
	want := NewFile + "sources:\n  a: # keep\n    dir: ~/a\n  b: # also\n    dir: ~/b\n"
	if got := string(e.Bytes()); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A file whose last line has no line break keeps that line as it is; the
// break is added only when an entry goes after it.
func TestEditorLastLineWithoutBreak(t *testing.T) {
	data := NewFile + "max-ctx-chars: 1"
	e := editor(t, data)
	if e.Changed() {
		t.Error("reading the file changed it")
	}
	if _, err := e.Add(Sources, "a", node(t, "{dir: ~/a}"), Place{}); err != nil {
		t.Fatal(err)
	}
	if got, want := string(e.Bytes()), NewFile+"sources:\n  a:\n    dir: ~/a\nmax-ctx-chars: 1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	data = NewFile + "bindings:\n  - name: a\n    skills: [x]"
	e = editor(t, data)
	if _, err := e.Add(Bindings, "b", node(t, "{skills: [z]}"), Place{}); err != nil {
		t.Fatal(err)
	}
	if got, want := string(e.Bytes()), data+"\n  - name: b\n    skills: [z]\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := e.Remove(Bindings, "b"); err != nil {
		t.Fatal(err)
	}
	if got := string(e.Bytes()); got != data+"\n" {
		t.Errorf("got %q", got)
	}
}
