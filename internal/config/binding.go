package config

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Binding is an entry of the config's bindings: conditions, and what it
// adds where they hold.
type Binding struct {
	// Name is the binding's name; "" when it has none, which is an error.
	Name string
	Pos  yamlx.Pos
	// Location is where a session starts; nil applies everywhere.
	Location *Location
	// Profiles and Agents are the conditions' entries as written; nil when
	// the binding has no such condition.
	Profiles []Entry
	Agents   []Entry
	Content
	// Errs are the binding's errors, its entries' included.
	Errs []*yamlx.Error
}

// Broken reports whether an error leaves out the binding.
func (b *Binding) Broken() bool { return broken(b.Errs) }

// Location is a binding's place condition: the folder check (Dirs) and the
// repository check (Repos with RepoSets).
type Location struct {
	Pos      yamlx.Pos
	Dirs     []Entry
	Repos    []RepoEntry
	RepoSets []Entry
}

// emptyCondition is the advice for a condition without entries.
const emptyCondition = "%s is empty; remove location to apply everywhere, or remove the binding"

func decodeBinding(d *yamlx.Doc, n *yaml.Node, index int, agents []string) *Binding {
	b := &Binding{Pos: yamlx.At(n)}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "name" && v.Kind == yaml.ScalarNode && v.ShortTag() == "!!str" {
				b.Name = v.Value
				break
			}
		}
	}
	part := yamlx.Part{Kind: "binding", Name: b.Name}
	if b.Name == "" {
		part.Name = fmt.Sprintf("#%d", index+1)
	}
	r := yamlx.NewReader(d, part, yamlx.BrokenBinding, n)
	defer func() { b.Errs = r.Errs }()
	if r.Broken() {
		return b
	}
	f, _ := r.Map(n, "a binding", slices.Concat([]string{"name", "location", "profiles", "agents"}, contentKeys)...)
	if f == nil {
		return b
	}
	if v := f["name"]; v == nil {
		r.Fail(n, "a binding needs a name")
	} else if b.Name, _ = r.Text(v, "name"); b.Name != "" {
		if err := CheckName(b.Name); err != nil {
			r.Fail(v, "%v", err)
		}
	}
	if v := f["location"]; v != nil {
		b.Location = decodeLocation(r, v)
	}
	if v := f["profiles"]; v != nil {
		b.Profiles = decodeNames(r, v, "profiles", func(e Entry) string {
			if err := CheckName(e.Value); err != nil {
				return fmt.Sprintf("profile %q: %v", e.Value, err)
			}
			return ""
		})
	}
	if v := f["agents"]; v != nil {
		b.Agents = decodeNames(r, v, "agents", func(e Entry) string {
			if slices.ContainsFunc(agents, func(a string) bool { return strings.EqualFold(a, e.Value) }) {
				return ""
			}
			return fmt.Sprintf("unknown agent %q (known: %s)", e.Value, strings.Join(agents, ", "))
		})
	}
	b.Content = decodeContent(r, f, InConfig)
	if !slices.ContainsFunc(contentKeys, func(k string) bool { return f[k] != nil && !yamlx.IsEmpty(f[k]) }) {
		r.Fail(n, "the binding adds nothing; it needs instructions, skills, collections or mcp-servers")
	}
	checkExclusions(r, f, b.Content)
	return b
}

// decodeNames reads the entries of profiles or agents; check says what is
// wrong with a name. Any wrong name leaves out the binding.
func decodeNames(r *yamlx.Reader, n *yaml.Node, key string, check func(Entry) string) []Entry {
	if yamlx.IsEmpty(n) {
		r.Add(n, yamlx.EmptyCondition, emptyCondition, key)
		return nil
	}
	items, ok := r.List(n, key)
	if !ok {
		return nil
	}
	var out []Entry
	for _, it := range items {
		text, ok := r.Text(it, key+" entry")
		if !ok {
			continue
		}
		e := Entry{Text: text, Pos: yamlx.At(it)}
		e.Value, e.Not = strings.CutPrefix(text, "!")
		if msg := check(e); msg != "" {
			r.Fail(it, "%s", msg)
		}
		out = append(out, e)
	}
	return out
}

func decodeLocation(r *yamlx.Reader, n *yaml.Node) *Location {
	l := &Location{Pos: yamlx.At(n)}
	if yamlx.IsEmpty(n) {
		r.Add(n, yamlx.EmptyCondition, emptyCondition, "location")
		return l
	}
	f, ok := r.Map(n, "location", "dirs", "repos", "repo-sets")
	if !ok {
		return l
	}
	items := func(key string) []*yaml.Node {
		v := f[key]
		if v == nil {
			return nil
		}
		if yamlx.IsEmpty(v) {
			r.Add(v, yamlx.EmptyCondition, emptyCondition, key)
			return nil
		}
		list, _ := r.List(v, key)
		return list
	}
	// entryErr adds the error of a broken entry: a positive one matches
	// nothing, a ! one leaves out the binding.
	entryErr := func(n *yaml.Node, not bool, msg string) *yamlx.Error {
		if not {
			return r.EntryError(n, yamlx.BrokenExclusion, "%s", msg)
		}
		return r.EntryError(n, yamlx.BrokenLocation, "%s%s", msg, matchesNothing(false, false))
	}
	for _, it := range items("dirs") {
		text, ok := r.Text(it, "dirs entry")
		if !ok {
			continue
		}
		e := Entry{Text: text, Pos: yamlx.At(it)}
		e.Value, e.Not = strings.CutPrefix(text, "!")
		if msg := checkDir(e.Value); msg != "" {
			e.Err = entryErr(it, e.Not, msg)
		}
		l.Dirs = append(l.Dirs, e)
	}
	for _, it := range items("repos") {
		if e, ok := decodeRepoEntry(r, it, false); ok {
			l.Repos = append(l.Repos, e)
		}
	}
	for _, it := range items("repo-sets") {
		text, ok := r.Text(it, "repo-sets entry")
		if !ok {
			continue
		}
		e := splitEntry(text)
		e.Pos = yamlx.At(it)
		msg := checkSource(e)
		if err := CheckName(e.Value); msg == "" && err != nil {
			msg = fmt.Sprintf("%s: %v", text, err)
		}
		if msg != "" {
			e.Err = entryErr(it, e.Not, msg)
		}
		l.RepoSets = append(l.RepoSets, e)
	}
	return l
}

// checkBindingNames gives each binding whose name another binding has an
// error that names them all: none of them is used.
func checkBindingNames(bindings []*Binding) {
	byName := map[string][]*Binding{}
	for _, b := range bindings {
		if b.Name != "" {
			byName[b.Name] = append(byName[b.Name], b)
		}
	}
	for _, b := range bindings {
		same := byName[b.Name]
		if len(same) < 2 {
			continue
		}
		lines := make([]int, len(same))
		for i, o := range same {
			lines[i] = o.Pos.Line
		}
		b.Errs = append(b.Errs, &yamlx.Error{Pos: b.Pos, Mistake: yamlx.DuplicateBinding, Part: yamlx.Part{Kind: "binding", Name: b.Name},
			Msg: fmt.Sprintf("%d bindings are named %s, at lines %s; none of them is used", len(same), b.Name, yamlx.Lines(lines))})
	}
}
