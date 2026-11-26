package cfg

import (
	"os"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// The writer never writes YAML that the strict layer refuses: random
// entries added to random configs either fail with an error or read back
// as written, and the config still reads after they are removed.
func FuzzEditor(f *testing.F) {
	demo, err := os.ReadFile("testdata/demo.yaml")
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []struct {
		config     string
		name, a, b string
		flow       bool
	}{
		{string(demo), "x", "!cursor", "1.10", false},
		{string(demo), "iam", "iam-team:essentials", "on", true},
		{"", "a", "a: b", " lead", false},
		{NewFile + "sources: {}\nbindings: []\n", "b", "*x", "#a", true},
		{NewFile + "bindings:\n- name: me\n  skills: [a]\n", "c", "x\ny", "'q'", false},
		{NewFile + "sources:\n  a:\n    dir: |-\n      ~/a\n", "d", "2001-01-01", "~", true},
	} {
		f.Add([]byte(seed.config), seed.name, seed.a, seed.b, seed.flow)
	}
	f.Fuzz(func(t *testing.T, config []byte, name, a, b string, flow bool) {
		e, err := NewEditor(config)
		if err != nil {
			return
		}
		text := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
		list := func(vs ...string) *yaml.Node {
			l := &yaml.Node{Kind: yaml.SequenceNode}
			if flow {
				l.Style = yaml.FlowStyle
			}
			for _, v := range vs {
				l.Content = append(l.Content, text(v))
			}
			return l
		}
		binding := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			text("location"), {Kind: yaml.MappingNode, Content: []*yaml.Node{text("dirs"), list(a), text("repos"), list(b)}},
			text("profiles"), list(a, b),
			text("instructions"), list(b, a),
		}}
		source := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{text("repo"), text(a), text("branch"), text(b)}}
		added := map[Section]*yaml.Node{}
		for s, n := range map[Section]*yaml.Node{Bindings: binding, Sources: source, RepoSets: list(a, b)} {
			ok, err := e.Add(s, name, n, Place{})
			if err != nil || !ok {
				continue
			}
			added[s] = n
			d, perr := yamlx.Parse(e.Bytes(), yamlx.ConfigKind)
			if perr != nil {
				t.Fatalf("the strict layer refuses the written config: %v\n%s", perr, e.Bytes())
			}
			got, err := e.Entry(s, name)
			if err != nil || got == nil {
				t.Fatalf("%s %q isn't there: %v\n%s", s.Kind(), name, err, e.Bytes())
			}
			if errs := d.Check(yamlx.Part{}, got); len(errs) > 0 {
				t.Fatalf("%s %q: %v\n%s", s.Kind(), name, errs, e.Bytes())
			}
			want := n
			if s == Bindings {
				want = &yaml.Node{Kind: yaml.MappingNode, Content: append([]*yaml.Node{text("name"), text(name)}, n.Content...)}
			}
			if !yamlx.Same(got, want) {
				t.Fatalf("%s %q doesn't read back as written\n%s", s.Kind(), name, e.Bytes())
			}
		}
		for _, s := range []Section{Sources, RepoSets, Bindings} {
			if added[s] == nil {
				continue
			}
			if err := e.Remove(s, name); err != nil {
				t.Fatalf("remove %s %q: %v\n%s", s.Kind(), name, err, e.Bytes())
			}
		}
		if len(added) > 0 {
			if _, err := yamlx.Parse(e.Bytes(), yamlx.ConfigKind); err != nil {
				t.Fatalf("after removing: %v\n%s", err, e.Bytes())
			}
		}
	})
}
