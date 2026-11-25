package cfg

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Content is what a binding or collection adds, each list as written.
type Content struct {
	Collections  []Entry
	Instructions []Entry
	Skills       []Entry
	MCPServers   []Entry
}

var contentKeys = []string{"collections", "instructions", "skills", "mcp-servers"}

// decodeContent reads the content lists in f.
func decodeContent(r *yamlx.Reader, f map[string]*yaml.Node, owner Owner) Content {
	list := func(key string, kind listKind) []Entry {
		n := f[key]
		if n == nil {
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
			e, msg := contentEntry(text, kind, owner)
			e.Pos = yamlx.At(it)
			if msg != "" {
				e.Err = r.EntryError(it, yamlx.BrokenEntry, "%s", msg)
			}
			out = append(out, e)
		}
		return out
	}
	return Content{
		Collections:  list("collections", nameList),
		Instructions: list("instructions", pathList),
		Skills:       list("skills", pathList),
		MCPServers:   list("mcp-servers", serverList),
	}
}

// checkExclusions reports content lists of only ! entries when there is
// nothing for them to remove from: no collection adds anything.
func checkExclusions(r *yamlx.Reader, f map[string]*yaml.Node, c Content) {
	positive := func(list []Entry) bool { return slices.ContainsFunc(list, func(e Entry) bool { return !e.Not }) }
	if len(c.Collections) > 0 && !positive(c.Collections) {
		r.Fail(f["collections"], "collections has only ! entries: there is no collection for them to remove")
	}
	if positive(c.Collections) {
		return
	}
	for _, k := range []struct {
		key  string
		list []Entry
	}{{"instructions", c.Instructions}, {"skills", c.Skills}, {"mcp-servers", c.MCPServers}} {
		if len(k.list) > 0 && !positive(k.list) {
			r.Fail(f[k.key], "%s has only ! entries, which remove what collections add, and there are no collections", k.key)
		}
	}
}

// Collection is a named set of what a binding adds, in the config or a
// manifest.
type Collection struct {
	Name string
	Pos  yamlx.Pos
	Content
	// Errs are the collection's errors, its entries' included.
	Errs []*yamlx.Error
}

// Broken reports whether an error leaves out the collection.
func (c *Collection) Broken() bool { return broken(c.Errs) }

// DecodeCollection reads the collection in p.
func DecodeCollection(d *yamlx.Doc, p yamlx.Pair, owner Owner) *Collection {
	c := &Collection{Name: p.Key, Pos: yamlx.At(p.K)}
	r := yamlx.NewReader(d, yamlx.Part{Kind: "collection", Name: p.Key}, yamlx.BrokenGroup, p.K, p.V)
	defer func() { c.Errs = r.Errs }()
	if err := CheckName(p.Key); err != nil {
		r.Fail(p.K, "%v", err)
	}
	if r.Broken() {
		return c
	}
	f, _ := r.Map(p.V, "a collection", contentKeys...)
	c.Content = decodeContent(r, f, owner)
	checkExclusions(r, f, c.Content)
	return c
}

// RepoSet is a named list of repositories, in the config or a manifest.
type RepoSet struct {
	Name    string
	Pos     yamlx.Pos
	Entries []RepoEntry
	// Errs are the set's errors. A set with broken entries keeps the others.
	Errs []*yamlx.Error
}

// Broken reports whether an error leaves out the set.
func (s *RepoSet) Broken() bool { return broken(s.Errs) }

// DecodeRepoSet reads the repo set in p.
func DecodeRepoSet(d *yamlx.Doc, p yamlx.Pair) *RepoSet {
	s := &RepoSet{Name: p.Key, Pos: yamlx.At(p.K)}
	r := yamlx.NewReader(d, yamlx.Part{Kind: "repo set", Name: p.Key}, yamlx.BrokenGroup, p.K, p.V)
	defer func() { s.Errs = r.Errs }()
	if err := CheckName(p.Key); err != nil {
		r.Fail(p.K, "%v", err)
	}
	if r.Broken() {
		return s
	}
	items, ok := r.List(p.V, "a repo set")
	if !ok {
		return s
	}
	if len(items) == 0 {
		r.Add(p.V, yamlx.EmptyRepoSet, "the set has no repositories, so it matches nothing")
		return s
	}
	for _, it := range items {
		if e, ok := decodeRepoEntry(r, it, true); ok {
			s.Entries = append(s.Entries, e)
		}
	}
	return s
}

// RepoEntry is a repository entry of location.repos or a repo set: a
// pattern, or a repository with the parts of it that count.
type RepoEntry struct {
	// Text is the repository as written, with its !.
	Text string
	Not  bool
	// Repo is the repository pattern, without !.
	Repo string
	// Subdirs are the parts of the repository, relative to its root; nil
	// for the whole repository.
	Subdirs []Entry
	Pos     yamlx.Pos
	// Err is why the entry can't be used, nil when it can.
	Err *yamlx.Error
}

// decodeRepoEntry reads one repository entry. In a set, a broken entry is
// left out; in a location, a broken positive entry stays and matches
// nothing, and a broken ! entry leaves out the binding. ok is false when
// the entry isn't kept.
func decodeRepoEntry(r *yamlx.Reader, n *yaml.Node, inSet bool) (RepoEntry, bool) {
	e := RepoEntry{Pos: yamlx.At(n)}
	// entryErr is the mistake of a broken entry.
	entryErr := func(not bool) yamlx.Mistake {
		switch {
		case inSet:
			return yamlx.BrokenEntry
		case not:
			return yamlx.BrokenExclusion
		}
		return yamlx.BrokenLocation
	}
	repoNode := n
	if n.Kind == yaml.MappingNode {
		f, ok := r.Map(n, "a repos entry", "repo", "subdirs")
		if !ok {
			return e, false
		}
		if f["repo"] == nil {
			e.Err = r.EntryError(n, entryErr(false), "a repos entry with subdirs needs repo%s", matchesNothing(inSet, false))
			return e, !inSet
		}
		repoNode = f["repo"]
		text, ok := r.Text(repoNode, "repo")
		if !ok {
			return e, false
		}
		e.Text, e.Repo = text, text
		if strings.HasPrefix(text, "!") {
			m := yamlx.BrokenBinding
			if inSet {
				m = yamlx.BrokenEntry
			}
			r.Add(repoNode, m, "repo %s: put ! in subdirs to leave out parts of the repository, or write the repository alone to leave out all of it", text)
			return e, false
		}
		if sn := f["subdirs"]; sn != nil {
			if !decodeSubdirs(r, sn, &e, inSet) {
				return e, false
			}
		}
	} else {
		text, ok := r.Text(n, "repos entry")
		if !ok {
			return e, false
		}
		e.Text = text
		e.Repo, e.Not = strings.CutPrefix(text, "!")
	}
	if msg := checkRepoPattern(e.Repo); msg != "" {
		if inSet && (strings.HasPrefix(e.Repo, "/") || strings.HasPrefix(e.Repo, "~")) {
			msg = fmt.Sprintf("%s is a folder; a set lists repositories only", e.Repo)
		}
		e.Err = r.EntryError(repoNode, entryErr(e.Not), "%s%s", msg, matchesNothing(inSet, e.Not))
		return e, !inSet
	}
	return e, true
}

// decodeSubdirs reads the subdirs of e. It returns false when the entry
// isn't kept.
func decodeSubdirs(r *yamlx.Reader, n *yaml.Node, e *RepoEntry, inSet bool) bool {
	if yamlx.IsEmpty(n) {
		if inSet {
			r.Add(n, yamlx.BrokenEntry, "subdirs is empty; remove it to use the whole repository")
		} else {
			r.Add(n, yamlx.EmptyCondition, "subdirs is empty; remove it to use the whole repository")
		}
		return false
	}
	items, ok := r.List(n, "subdirs")
	if !ok {
		return false
	}
	for _, it := range items {
		text, ok := r.Text(it, "subdirs entry")
		if !ok {
			return false
		}
		s := Entry{Text: text, Pos: yamlx.At(it)}
		s.Value, s.Not = strings.CutPrefix(text, "!")
		if msg := checkSubdir(s.Value); msg != "" {
			m := yamlx.BrokenLocation
			switch {
			case inSet:
				r.Add(it, yamlx.BrokenEntry, "%s", msg)
				return false
			case s.Not:
				m = yamlx.BrokenExclusion
			}
			s.Err = r.EntryError(it, m, "%s%s", msg, matchesNothing(false, s.Not))
		}
		e.Subdirs = append(e.Subdirs, s)
	}
	return true
}

// matchesNothing says what a broken positive location entry does.
func matchesNothing(inSet, not bool) string {
	if inSet || not {
		return ""
	}
	return "; the entry matches nothing"
}

func broken(errs []*yamlx.Error) bool {
	return slices.ContainsFunc(errs, func(e *yamlx.Error) bool { return e.Out() >= yamlx.OutPart })
}
