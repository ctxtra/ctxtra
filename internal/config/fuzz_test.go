package config

import (
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
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
		{NewFile + "max-ctx-chars: 010\nsession-message:\n  max-skills: 010\n  max-instructions: 8\n", "a", "8", "010", false},
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
		pairs := func(kv ...*yaml.Node) *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Content: kv} }
		// Every key that binding add writes.
		binding := pairs(
			text("location"), pairs(text("dirs"), list(a), text("repos"), &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
				text(b), pairs(text("repo"), text(a), text("subdirs"), list(b, a)),
			}}, text("repo-sets"), list(b)),
			text("profiles"), list(a, b),
			text("agents"), list(b),
			text("collections"), list(a),
			text("instructions"), list(b, a),
			text("skills"), list(a),
			text("mcp-servers"), list(b),
		)
		collection := pairs(text("instructions"), list(a), text("skills"), list(b), text("mcp-servers"), list(a), text("collections"), list(b))
		source := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{text("repo"), text(a), text("branch"), text(b)}}
		server := pairs(text("url"), text(a), text("headers"), pairs(text(a), text(b)), text("scopes"), list(a, b))
		added := map[Section]*yaml.Node{}
		for s, n := range map[Section]*yaml.Node{Bindings: binding, Sources: source, RepoSets: {Kind: yaml.SequenceNode, Content: []*yaml.Node{text(a), pairs(text("repo"), text(b), text("subdirs"), list(a, b))}}, Collections: collection, MCPServers: server} {
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
		// Replacing an added entry in place keeps the config readable too.
		if added[Bindings] != nil {
			swapped := pairs(text("profiles"), list(b), text("skills"), list(a, b))
			if _, _, err := e.Put(Bindings, name, swapped, Place{}); err == nil {
				if _, perr := yamlx.Parse(e.Bytes(), yamlx.ConfigKind); perr != nil {
					t.Fatalf("the strict layer refuses the replaced binding: %v\n%s", perr, e.Bytes())
				}
				got, _ := e.Entry(Bindings, name)
				want := pairs(append([]*yaml.Node{text("name"), text(name)}, swapped.Content...)...)
				if got == nil || !yamlx.Same(got, want) {
					t.Fatalf("binding %q doesn't read back as replaced\n%s", name, e.Bytes())
				}
			}
		}
		for _, s := range []Section{Sources, RepoSets, Collections, Bindings, MCPServers} {
			if added[s] == nil {
				continue
			}
			if err := e.Remove(s, name); err != nil {
				t.Fatalf("remove %s %q: %v\n%s", s.Kind(), name, err, e.Bytes())
			}
		}
		if _, _, err := e.Put(MCPServers, "alias", pairs(text("use"), text(a)), Place{}); err == nil {
			got, _ := e.Entry(MCPServers, "alias")
			if !yamlx.Same(got, pairs(text("use"), text(a))) {
				t.Fatalf("use lost text %q\n%s", a, e.Bytes())
			}
		}
		for _, setting := range []struct{ key, value string }{{"check-releases", a}, {"max-ctx-chars", b}, {"session-message.max-skills", a}, {"session-message.max-instructions", b}} {
			d, err := yamlx.ParseEntry([]byte(setting.value))
			if err != nil {
				continue
			}
			before := slices.Clone(e.Bytes())
			if err := e.Setting(setting.key, d.Root); err != nil {
				if !slices.Equal(e.Bytes(), before) {
					t.Fatal("a refused setting changed the config")
				}
				continue
			}
			if _, perr := yamlx.Parse(e.Bytes(), yamlx.ConfigKind); perr != nil {
				t.Fatalf("written setting: %v\n%s", perr, e.Bytes())
			}
			cfg := Parse(e.Bytes(), Options{})
			top, _, _ := strings.Cut(setting.key, ".")
			if !slices.ContainsFunc(cfg.Errors, func(e *yamlx.Error) bool { return e.Part.Kind == "key" && e.Part.Name == top }) {
				if setting.key == "check-releases" {
					want, err := strconv.ParseBool(d.Root.Value)
					if err != nil || cfg.CheckReleases == nil || *cfg.CheckReleases != want {
						t.Fatalf("setting %s did not become %q\n%s", setting.key, setting.value, e.Bytes())
					}
				} else {
					want, err := strconv.Atoi(d.Root.Value)
					got := cfg.MaxCtxChars
					switch setting.key {
					case "session-message.max-skills":
						got = cfg.SessionMessage.MaxSkills
					case "session-message.max-instructions":
						got = cfg.SessionMessage.MaxInstructions
					}
					if err != nil || got == nil || *got != want {
						t.Fatalf("setting %s did not become decimal %q\n%s", setting.key, setting.value, e.Bytes())
					}
				}
			}
			if slices.Equal(e.Bytes(), before) {
				continue
			}
			if err := e.Setting(setting.key, nil); err != nil {
				t.Fatalf("remove written setting: %v", err)
			}
		}
		if len(added) > 0 {
			if _, err := yamlx.Parse(e.Bytes(), yamlx.ConfigKind); err != nil {
				t.Fatalf("after removing: %v\n%s", err, e.Bytes())
			}
		}
	})
}

// Parse never fails on any input, and what it keeps holds the model's
// rules: a file it can't read has nothing else, numbers are not negative,
// usable sources and bindings have valid names, a source has one of repo
// and dir, and a usable SOURCE:PATH entry stays in its source.
func FuzzConfig(f *testing.F) {
	demo, err := os.ReadFile("testdata/demo.yaml")
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{
		string(demo), "", NewFile, NewFile + "max-ctx-chars: -1\n",
		NewFile + "session-message: {max-skills: -2}\n",
		NewFile + "sources: {team: {repo: 'https://example.com/team.git', branch: main}}\n",
		NewFile + "sources: {team: {repo: x, dir: ~/x}}\n",
		NewFile + "bindings: [{name: x, instructions: ['team:../x.md', 'team:/etc/passwd', 'team:a/../../b']}]\n",
		NewFile + "bindings: &a [{name: x}]\nsources: *a\n",
		NewFile + "---\n" + NewFile,
		"schema: ctxtra/v1\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		c := Parse(data, Options{Agents: []string{"claude-code", "codex"}})
		for _, e := range c.Errors {
			if e.Out() == yamlx.OutFile && (len(c.Sources)+len(c.Bindings)+len(c.MCPServers) > 0 || len(c.Errors) != 1) {
				t.Fatalf("a file that can't be read kept parts: %v", c.Errors)
			}
		}
		for _, n := range []*int{c.MaxCtxChars, c.SessionMessage.MaxSkills, c.SessionMessage.MaxInstructions} {
			if n != nil && *n < 0 {
				t.Fatalf("negative number accepted: %d", *n)
			}
		}
		for _, s := range c.Sources {
			if s.Broken() {
				continue
			}
			if CheckName(s.Name) != nil || (s.Repo == "") == (s.Dir == "") {
				t.Fatalf("source %q accepted: repo %q dir %q", s.Name, s.Repo, s.Dir)
			}
		}
		for _, b := range c.Bindings {
			if b.Broken() {
				continue
			}
			if CheckName(b.Name) != nil {
				t.Fatalf("binding name %q accepted", b.Name)
			}
			for _, e := range slices.Concat(b.Instructions, b.Skills) {
				if e.Err != nil || !e.InSource {
					continue
				}
				p := path.Clean(e.Value)
				if CheckName(e.Source) != nil || p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(p) {
					t.Fatalf("entry %q accepted: source %q, path %q", e.Text, e.Source, p)
				}
			}
		}
	})
}
