package config

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
	Name string
	Pos  yamlx.Pos
	// Entries are the set's entries as written. A broken entry has its
	// error and is left out; the set's other entries apply.
	Entries []RepoEntry
	// Errs are the set's errors, its entries' included.
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

// decodeRepoEntry reads one repository entry. A broken entry has its
// error: in a set it is left out, and in a location a positive one matches
// nothing and a ! one leaves out the binding. ok is false when an error
// leaves out the whole part, and the entry with it.
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
	switch n.Kind {
	case yaml.ScalarNode:
		text, ok := r.Text(n, "repos entry")
		if !ok {
			return e, false
		}
		e.Text = text
		e.Repo, e.Not = strings.CutPrefix(text, "!")
	case yaml.MappingNode:
		f, ok := r.Map(n, "a repos entry", "repo", "subdirs")
		if !ok {
			return e, false
		}
		if f["repo"] == nil {
			e.Err = r.EntryError(n, entryErr(false), "a repos entry with subdirs needs repo%s", matchesNothing(inSet, false))
			return e, true
		}
		repoNode = f["repo"]
		if inSet && repoNode.Kind != yaml.ScalarNode {
			e.Err = r.EntryError(repoNode, yamlx.BrokenEntry, "repo must be one repository")
			return e, true
		}
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
			e.Err = r.EntryError(repoNode, m, "repo %s: put ! in subdirs to leave out parts of the repository, or write the repository alone to leave out all of it", text)
			return e, true
		}
		if sn := f["subdirs"]; sn != nil {
			if !decodeSubdirs(r, sn, &e, inSet) {
				return e, false
			}
		}
	default:
		e.Err = r.EntryError(n, entryErr(false), "a repos entry is a repository, or a map with repo and subdirs%s", matchesNothing(inSet, false))
		return e, true
	}
	if e.Err != nil {
		return e, true
	}
	if msg := checkRepoPattern(e.Repo); msg != "" {
		if inSet && (strings.HasPrefix(e.Repo, "/") || strings.HasPrefix(e.Repo, "~")) {
			msg = fmt.Sprintf("%s is a folder; a set lists repositories only", e.Repo)
		}
		e.Err = r.EntryError(repoNode, entryErr(e.Not), "%s%s", msg, matchesNothing(inSet, e.Not))
	}
	return e, true
}

// decodeSubdirs reads the subdirs of e. In a set, a broken subdirs entry,
// or subdirs of another shape, breaks only e. It returns false when an
// error leaves out the whole part.
func decodeSubdirs(r *yamlx.Reader, n *yaml.Node, e *RepoEntry, inSet bool) bool {
	const empty = "subdirs is empty; remove it to use the whole repository"
	if yamlx.IsEmpty(n) {
		if inSet {
			e.Err = r.EntryError(n, yamlx.BrokenEntry, empty)
			return true
		}
		r.Add(n, yamlx.EmptyCondition, empty)
		return false
	}
	if inSet && n.Kind != yaml.SequenceNode {
		e.Err = r.EntryError(n, yamlx.BrokenEntry, "subdirs must be a list of folders")
		return true
	}
	items, ok := r.List(n, "subdirs")
	if !ok {
		return false
	}
	for _, it := range items {
		if inSet && it.Kind != yaml.ScalarNode {
			s := Entry{Pos: yamlx.At(it), Err: r.EntryError(it, yamlx.BrokenEntry, "a subdirs entry must be one folder")}
			if e.Err == nil {
				e.Err = s.Err
			}
			e.Subdirs = append(e.Subdirs, s)
			continue
		}
		text, ok := r.Text(it, "subdirs entry")
		if !ok {
			return false
		}
		s := Entry{Text: text, Pos: yamlx.At(it)}
		s.Value, s.Not = strings.CutPrefix(text, "!")
		if msg := checkSubdir(s.Value); msg != "" {
			switch {
			case inSet:
				s.Err = r.EntryError(it, yamlx.BrokenEntry, "%s", msg)
				if e.Err == nil {
					e.Err = s.Err
				}
			case s.Not:
				s.Err = r.EntryError(it, yamlx.BrokenExclusion, "%s", msg)
			default:
				s.Err = r.EntryError(it, yamlx.BrokenLocation, "%s%s", msg, matchesNothing(false, false))
			}
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
