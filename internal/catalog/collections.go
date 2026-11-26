package catalog

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Expansion is what a list of collections adds, nested collections
// included, each file, skill folder and server once, at its first
// position.
type Expansion struct {
	Files, Skills []Item
	Servers       []Server
	// Entered are the sources' collections that the list or the config's
	// own collections name, as SOURCE:NAME: where it enters each source.
	Entered []string
	// Used are every collection it went through, nested ones included,
	// after ! entries, as NAME or SOURCE:NAME.
	Used []string
	// Manifests are the sources whose manifests it read.
	Manifests []string
	// Unavailable are the sources it names whose content can't be used:
	// what they would add is left out.
	Unavailable []string
	// Errors are the errors in the collections it went through, each once.
	Errors []*Error
}

func (x *Expansion) merge(o *Expansion) {
	x.Files = addItems(x.Files, o.Files)
	x.Skills = addItems(x.Skills, o.Skills)
	ids := map[string]bool{}
	for _, s := range x.Servers {
		ids[s.ID] = true
	}
	for _, s := range o.Servers {
		if !ids[s.ID] {
			ids[s.ID] = true
			x.Servers = append(x.Servers, s)
		}
	}
	x.Entered = union(x.Entered, o.Entered...)
	x.Used = union(x.Used, o.Used...)
	x.Manifests = union(x.Manifests, o.Manifests...)
	x.Unavailable = union(x.Unavailable, o.Unavailable...)
	x.Errors = addErrors(x.Errors, o.Errors)
}

func addErrors(list, more []*Error) []*Error {
	for _, e := range more {
		if !slices.Contains(list, e) {
			list = append(list, e)
		}
	}
	return list
}

func addItem(list []Item, it Item) []Item { return addItems(list, []Item{it}) }

// addItems adds the items of more that list doesn't have, by path.
func addItems(list, more []Item) []Item {
	have := make(map[string]bool, len(list))
	for _, it := range list {
		have[it.Path] = true
	}
	for _, it := range more {
		if !have[it.Path] {
			have[it.Path] = true
			list = append(list, it)
		}
	}
	return list
}

func union(list []string, more ...string) []string {
	have := make(map[string]bool, len(list))
	for _, s := range list {
		have[s] = true
	}
	for _, s := range more {
		if !have[s] {
			have[s] = true
			list = append(list, s)
		}
	}
	return list
}

// EntryError is the error of one entry of a binding's list.
type EntryError struct {
	Entry config.Entry
	Err   error
}

// coll is a collection of the config or of a source's manifest, as this
// catalog reads it: its entries are read once, and its expansion is reused.
type coll struct {
	// source is the source whose manifest has it; nil for the config's own.
	source *Source
	def    *config.Collection
	// file is the file it is in, for its errors.
	file string
	// nested are its nested collections, in list order.
	nested []*coll
	// own is what its own positive entries add.
	own Expansion
	// not are its ! entries, as the config would write them.
	notFiles, notSkills, notServers, notColls []config.Entry
	errs                                      []*Error
	// cycle: it is in a cycle, and adds nothing.
	cycle bool

	state      int // unvisited, onStack or done
	index, low int
	selfLoop   bool
	// memos are its expansions, by the set of ! entries of the
	// collections that contain it that name a collection nested in it.
	memos map[string]*Expansion
	// reach are the collections nested in it, at any depth.
	reach     []*coll
	reachSet  map[*coll]bool
	reachDone bool
	// nests caches whether a ! entry names a collection in reach.
	nests map[string]bool
	// recorded: the errors of its own ! entries are in errs.
	recorded bool
}

// maxExpansions bounds the expansions of collections that one evaluation,
// a binding's collections or one collection, makes: one per collection and
// set of ! entries of the collections that contain it. Scoped ! entries in
// many nested collections could make their number grow with every level.
var maxExpansions = 10000

const (
	unvisited = iota
	onStack
	done
)

// ref is how the config names it: NAME for the config's own, SOURCE:NAME.
func (k *coll) ref() string {
	if k.source == nil {
		return k.def.Name
	}
	return k.source.Name + ":" + k.def.Name
}

// entry is e of k's lists as the config would write it: a manifest's
// paths and names are in its own source.
func (k *coll) entry(e config.Entry) config.Entry {
	if k.source != nil {
		e.InSource, e.Source = true, k.source.Name
	}
	return e
}

func (k *coll) fail(pos yamlx.Pos, m yamlx.Mistake, format string, args ...any) {
	k.errs = append(k.errs, &Error{File: k.file, Err: &yamlx.Error{Pos: pos, Mistake: m,
		Part: yamlx.Part{Kind: "collection", Name: k.def.Name}, Msg: fmt.Sprintf(format, args...)}})
}

// entryFailed records err of entry e of k.
func (k *coll) entryFailed(e config.Entry, err error) {
	var u *Unavailable
	if errors.As(err, &u) {
		k.own.Unavailable = union(k.own.Unavailable, u.Source.Name)
		return
	}
	if m := EntryMistake(err); m != yamlx.BrokenEntry {
		k.fail(e.Pos, m, "%s: %v", e.Text, err)
		return
	}
	k.fail(e.Pos, yamlx.BrokenEntry, "%v", err)
}

// EntryMistake is the mistake of what a content entry couldn't add: a
// file the trust rules refuse, one that can't be read, or a broken entry.
func EntryMistake(err error) yamlx.Mistake {
	switch {
	case IsUntrusted(err):
		return yamlx.UntrustedFile
	case errors.As(err, new(*Unreadable)):
		return yamlx.UnreadableFile
	}
	return yamlx.BrokenEntry
}

// matches reports whether the ! entry e of a collections list, as the
// config would write it, names k.
func matches(e config.Entry, k *coll) bool {
	if e.InSource != (k.source != nil) || e.InSource && e.Source != k.source.Name {
		return false
	}
	return nameMatch(e.Value, k.def.Name)
}

// Expand is what a binding's collections list adds: its positive entries'
// collections in list order, each fully expanded, minus the collections
// that its ! entries name, wherever they are nested. A collection that an
// entry matches by pattern comes in name order. errs are the errors of the
// list's own entries.
func (c *Catalog) Expand(list []config.Entry) (*Expansion, []EntryError) {
	c.evaluate()
	x := &Expansion{}
	var errs []EntryError
	var not []config.Entry
	for _, e := range list {
		if e.Not && e.Err == nil {
			not = append(not, e)
		}
	}
	var reached []*coll
	for _, e := range list {
		if e.Not || e.Err != nil {
			continue
		}
		found, manifests, err := c.lookup(e, nil)
		x.Manifests = union(x.Manifests, manifests...)
		if err != nil {
			var u *Unavailable
			if errors.As(err, &u) {
				x.Unavailable = union(x.Unavailable, u.Source.Name)
			} else {
				errs = append(errs, EntryError{e, err})
			}
			continue
		}
		for _, k := range found {
			reached = append(reached, k)
			reached = append(reached, k.reached()...)
			if slices.ContainsFunc(not, func(n config.Entry) bool { return matches(n, k) }) {
				continue
			}
			if k.source != nil {
				x.Entered = union(x.Entered, k.ref())
			}
			x.merge(c.flat(k, not))
		}
	}
	for _, n := range not {
		if n.InSource && slices.Contains(x.Unavailable, n.Source) {
			continue
		}
		if !slices.ContainsFunc(reached, func(k *coll) bool { return matches(n, k) }) {
			errs = append(errs, EntryError{n, fmt.Errorf("%s excludes nothing", n.Text)})
		}
	}
	return x, errs
}

// Collection is what collection name of source src adds by itself, the
// config's own when src is "", and whether it exists.
func (c *Catalog) Collection(src, name string) (*Expansion, bool) {
	c.evaluate()
	e := config.Entry{Text: name, Value: name}
	if src != "" {
		e.Text, e.InSource, e.Source = src+":"+name, true, src
	}
	found, _, err := c.lookup(e, nil)
	if err != nil || len(found) != 1 {
		return nil, false
	}
	return c.flat(found[0], nil), true
}

// lookup is the collections that entry e of a collections list names, as
// the config would write it, read and expanded; from is the collection
// whose entry it is, nil for a binding's. manifests are the sources whose
// manifests it read.
func (c *Catalog) lookup(e config.Entry, from *coll) (found []*coll, manifests []string, err error) {
	var defs []*config.Collection
	var src *Source
	where := "the config"
	if e.InSource {
		src = c.sources[e.Source]
		switch {
		case src == nil:
			return nil, nil, fmt.Errorf("%s: the config has no source %s", e.Text, e.Source)
		case src.State != Active:
			return nil, nil, &Unavailable{src}
		}
		manifests = []string{src.Name}
		m, _, err := c.ManifestWithin(src, nil)
		switch {
		case err != nil:
			return nil, manifests, fmt.Errorf("%s: %w", e.Text, err)
		case m == nil:
			return nil, manifests, fmt.Errorf("%s: source %s has no %s, which declares its collections", e.Text, src.Name, manifest.FileName)
		}
		defs, where = m.Collections, "source "+src.Name
	} else {
		defs = c.conf.Collections
	}
	pattern, matched := hasMeta(e.Value), false
	for _, d := range defs {
		if !nameMatch(e.Value, d.Name) {
			continue
		}
		matched = true
		if d.Broken() {
			if !pattern {
				return nil, manifests, fmt.Errorf("%s: the collection %s of %s has errors", e.Text, d.Name, where)
			}
			continue
		}
		found = append(found, c.coll(src, d))
	}
	if !matched {
		if pattern {
			return nil, manifests, fmt.Errorf("%s matches no collection of %s", e.Text, where)
		}
		return nil, manifests, fmt.Errorf("%s has no collection %s", where, e.Value)
	}
	slices.SortFunc(found, func(a, b *coll) int { return strings.Compare(a.def.Name, b.def.Name) })
	if from == nil {
		for _, k := range found {
			c.build(k)
		}
	}
	return found, manifests, nil
}

// coll is the catalog's collection d of source src, nil for the config's.
func (c *Catalog) coll(src *Source, d *config.Collection) *coll {
	if c.colls == nil {
		c.colls = map[*config.Collection]*coll{}
	}
	k := c.colls[d]
	if k == nil {
		k = &coll{source: src, def: d, file: c.file}
		if src != nil {
			k.file = src.Name + ":" + manifest.FileName
		}
		c.colls[d] = k
	}
	return k
}

// build reads k and the collections it nests, if not read yet. Nested
// collections that contain each other form a cycle: each is an error, and
// adds nothing.
func (c *Catalog) build(k *coll) {
	if k.state != unvisited {
		return
	}
	b := &builder{c: c}
	b.visit(k)
	for _, n := range b.visited {
		if !n.cycle {
			c.readOwn(n)
		}
	}
	for _, n := range b.visited {
		c.flat(n, nil)
	}
}

// builder finds cycles of nested collections, as strongly connected
// components (Tarjan).
type builder struct {
	c       *Catalog
	index   int
	stack   []*coll
	visited []*coll
}

func (b *builder) visit(k *coll) {
	k.state, k.index, k.low = onStack, b.index, b.index
	b.index++
	b.stack = append(b.stack, k)
	b.visited = append(b.visited, k)
	for _, e := range k.def.Collections {
		if e.Not || e.Err != nil {
			continue
		}
		found, manifests, err := b.c.lookup(k.entry(e), k)
		k.own.Manifests = union(k.own.Manifests, manifests...)
		if err != nil {
			k.entryFailed(e, err)
			continue
		}
		for _, n := range found {
			k.nested = append(k.nested, n)
			switch n.state {
			case unvisited:
				b.visit(n)
				k.low = min(k.low, n.low)
			case onStack:
				k.low = min(k.low, n.index)
			}
			k.selfLoop = k.selfLoop || n == k
		}
	}
	if k.low != k.index {
		return
	}
	i := slices.Index(b.stack, k)
	scc := slices.Clone(b.stack[i:])
	b.stack = b.stack[:i]
	for _, n := range scc {
		n.state = done
	}
	if len(scc) == 1 && !k.selfLoop {
		return
	}
	slices.SortFunc(scc, func(a, b *coll) int { return strings.Compare(a.def.Name, b.def.Name) })
	var names []string
	for _, n := range scc {
		names = append(names, n.def.Name)
	}
	msg := fmt.Sprintf("collection %s nests itself, so it adds nothing", names[0])
	if len(names) > 1 {
		msg = fmt.Sprintf("collections %s and %s nest each other, so they add nothing",
			strings.Join(names[:len(names)-1], ", "), names[len(names)-1])
	}
	err := &Error{File: scc[0].file, Err: &yamlx.Error{Pos: scc[0].def.Pos, Mistake: yamlx.CollectionCycle, Msg: msg}}
	for _, n := range scc {
		n.cycle = true
		n.errs = append(n.errs, err)
	}
}

// readOwn reads what k's own entries add.
func (c *Catalog) readOwn(k *coll) {
	for _, l := range []struct {
		list []config.Entry
		kind Kind
		add  *[]Item
		not  *[]config.Entry
	}{{k.def.Instructions, Files, &k.own.Files, &k.notFiles}, {k.def.Skills, Skills, &k.own.Skills, &k.notSkills}} {
		for _, e := range l.list {
			switch {
			case e.Err != nil:
			case e.Not:
				*l.not = append(*l.not, k.entry(e))
			default:
				found, errs := c.Paths(k.entry(e), l.kind)
				for _, err := range errs {
					k.entryFailed(e, err)
				}
				for _, it := range found {
					*l.add = addItem(*l.add, it)
				}
			}
		}
	}
	for _, e := range k.def.MCPServers {
		switch {
		case e.Err != nil:
		case e.Not:
			k.notServers = append(k.notServers, k.entry(e))
		default:
			ce := k.entry(e)
			k.own.Manifests = union(k.own.Manifests, c.ManifestSources(ce)...)
			found, errs := c.Servers(ce)
			for _, err := range errs {
				k.entryFailed(e, err)
			}
			k.own.merge(&Expansion{Servers: found})
		}
	}
	for _, e := range k.def.Collections {
		if e.Not && e.Err == nil {
			k.notColls = append(k.notColls, k.entry(e))
		}
	}
}

// reached are the collections nested in k, at any depth.
func (k *coll) reached() []*coll {
	if k.reachDone || k.cycle {
		return k.reach
	}
	k.reachDone = true
	k.reachSet = map[*coll]bool{}
	for _, n := range k.nested {
		for _, m := range append([]*coll{n}, n.reached()...) {
			if !k.reachSet[m] {
				k.reachSet[m] = true
				k.reach = append(k.reach, m)
			}
		}
	}
	return k.reach
}

// exclusionKey identifies a ! entry of a collections list, as the config
// would write it, by what it matches.
func exclusionKey(e config.Entry) string {
	if e.InSource {
		return e.Source + ":" + e.Value
	}
	return ":" + e.Value
}

// nestsMatch reports whether ! entry e names a collection nested in k.
func (k *coll) nestsMatch(e config.Entry) bool {
	key := exclusionKey(e)
	if hit, ok := k.nests[key]; ok {
		return hit
	}
	if k.nests == nil {
		k.nests = map[string]bool{}
	}
	hit := slices.ContainsFunc(k.reached(), func(n *coll) bool { return matches(e, n) })
	k.nests[key] = hit
	return hit
}

// flat is what k adds when the ! entries not of the binding and of the
// collections that contain k, as the config would write them, leave out
// the collections they name. Only the entries that name a collection
// nested in k count, and k is expanded once for each set of them: a
// collection repeated in a graph, or reached by many paths, is reused. The
// expansion without such entries records the errors of k's ! entries.
func (c *Catalog) flat(k *coll, not []config.Entry) *Expansion {
	var keys []string
	var count []config.Entry
	for _, e := range not {
		if key := exclusionKey(e); !slices.Contains(keys, key) && k.nestsMatch(e) {
			keys = append(keys, key)
			count = append(count, e)
		}
	}
	slices.Sort(keys)
	key := strings.Join(keys, "\x00")
	if x, ok := k.memos[key]; ok {
		return x
	}
	if k.memos == nil {
		k.memos = map[string]*Expansion{}
	}
	if key != "" {
		// k's own diagnostics first: an expansion copies them.
		c.flat(k, nil)
	}
	if c.expansions >= maxExpansions {
		if c.tooMany == nil {
			c.tooMany = &Error{File: k.file, Err: &yamlx.Error{Pos: k.def.Pos, Mistake: yamlx.BrokenGroup,
				Part: yamlx.Part{Kind: "collection", Name: k.def.Name},
				Msg:  fmt.Sprintf("its nested collections and their ! entries need more than %d expansions", maxExpansions)}}
		}
		return &Expansion{Errors: []*Error{c.tooMany}}
	}
	c.expansions++
	x := c.expand(k, count, key == "")
	// What an evaluation past its budget expanded is not kept: the next
	// evaluation has a budget of its own.
	if c.tooMany == nil {
		k.memos[key] = x
	}
	return x
}

// evaluate starts an evaluation of collections, with its own budget of
// expansions. Expansions kept from earlier ones are reused.
func (c *Catalog) evaluate() { c.expansions, c.tooMany = 0, nil }

// expand is flat without reuse: k's nested collections, in list order,
// then its own entries, minus its ! entries. With record, ! entries that
// remove nothing are errors of k.
func (c *Catalog) expand(k *coll, not []config.Entry, record bool) *Expansion {
	x := &Expansion{Errors: slices.Clone(k.errs), Used: []string{k.ref()}}
	if k.cycle {
		return x
	}
	not = slices.Concat(not, k.notColls)
	for _, n := range k.nested {
		if slices.ContainsFunc(not, func(e config.Entry) bool { return matches(e, n) }) {
			continue
		}
		if k.source == nil && n.source != nil {
			x.Entered = union(x.Entered, n.ref())
		}
		x.merge(c.flat(n, not))
	}
	x.merge(&k.own)
	removed := map[string]bool{}
	drop := func(e config.Entry, hit bool) bool {
		if hit {
			removed[e.Text] = true
		}
		return hit
	}
	for _, l := range []struct {
		items *[]Item
		not   []config.Entry
	}{{&x.Files, k.notFiles}, {&x.Skills, k.notSkills}} {
		*l.items = slices.DeleteFunc(*l.items, func(it Item) bool {
			return slices.ContainsFunc(l.not, func(e config.Entry) bool { return drop(e, c.Excludes(e, it)) })
		})
	}
	x.Servers = slices.DeleteFunc(x.Servers, func(s Server) bool {
		return slices.ContainsFunc(k.notServers, func(e config.Entry) bool { return drop(e, ServerExcluded(e, s)) })
	})
	if !record || k.recorded || c.tooMany != nil {
		return x
	}
	k.recorded = true
	for _, e := range slices.Concat(k.notFiles, k.notSkills, k.notServers) {
		if !removed[e.Text] {
			k.fail(e.Pos, yamlx.BrokenEntry, "%s excludes nothing", e.Text)
		}
	}
	for _, e := range k.notColls {
		if !k.nestsMatch(e) {
			k.fail(e.Pos, yamlx.BrokenEntry, "%s excludes nothing", e.Text)
		}
	}
	x.Errors = addErrors(x.Errors, k.errs)
	return x
}

// Excludes reports whether ! entry e, as the config writes it, matches
// it: a SOURCE:PATH pattern by its path in that source, a path on disk by
// where it is.
func (c *Catalog) Excludes(e config.Entry, it Item) bool {
	if e.InSource {
		return it.Source == e.Source && globMatch(path.Clean(e.Value), it.Rel)
	}
	return globMatch(c.DiskPattern(e.Value), filepath.ToSlash(it.Path))
}

// ServerExcluded reports whether ! entry e of an mcp-servers list, as the
// config writes it, matches one of s's names.
func ServerExcluded(e config.Entry, s Server) bool {
	pat := e.Value
	if e.InSource {
		pat = e.Source + ":" + e.Value
	}
	return slices.ContainsFunc(s.Refs, func(ref string) bool { return globMatch(pat, ref) })
}

func globMatch(pattern, name string) bool {
	ok, err := doublestar.Match(pattern, name)
	return err == nil && ok
}

// Without is x without what source name adds.
func (x *Expansion) Without(name string) *Expansion {
	out := *x
	out.Files = slices.DeleteFunc(slices.Clone(x.Files), func(it Item) bool { return it.Source == name })
	out.Skills = slices.DeleteFunc(slices.Clone(x.Skills), func(it Item) bool { return it.Source == name })
	out.Servers = slices.DeleteFunc(slices.Clone(x.Servers), func(s Server) bool { return s.Source == name })
	return &out
}
