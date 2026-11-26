package cli

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/source"
)

// used is what the config takes from one commit of a source: files and
// skill folders, relative to the source's top, slash-separated.
type used struct {
	files map[string]bool
	dirs  []string
	// errors are the errors of the entries that read the source.
	errors []string
	// named are the bindings that name each of the source's collections,
	// directly or in the config's own collections, and using those that
	// use each, nested ones included; by the collection's name.
	named, using useMap
	// sets are the bindings that use each of the source's repo sets in
	// location, by the set's name; excluding those that use it in a !
	// entry.
	sets, excluding useMap
}

// useMap is the bindings that use each collection, by its name.
type useMap map[string][]string

// add records that binding uses colls.
func (m useMap) add(colls []string, binding string) {
	for _, c := range colls {
		if !slices.Contains(m[c], binding) {
			m[c] = append(m[c], binding)
		}
	}
}

func (u *used) has(p string) bool {
	return u.files[p] || slices.ContainsFunc(u.dirs, func(d string) bool { return d == "." || strings.HasPrefix(p, d+"/") })
}

// reviewTop is the folder that the paths of source name start from in the
// review's messages, the same for both commits; it doesn't exist.
func reviewTop(name string) string {
	return filepath.Join(string(filepath.Separator), "ctxtra-review", name)
}

// usedFrom is what the bindings of env take from source name in cat, under
// any conditions: its manifest, and every file and skill folder that their
// entries of the source reference after their ! entries, before content
// reads and limits filter them, with the errors a session would report for
// those entries.
func usedFrom(env *resolve.Env, cat *catalog.Catalog, name string) *used {
	u := &used{files: map[string]bool{manifest.FileName: true}, named: useMap{}, using: useMap{}, sets: useMap{}, excluding: useMap{}}
	alone := *env
	alone.Catalog = cat
	for _, b := range env.Config.Bindings {
		if b.Broken() {
			continue
		}
		ref := alone.Referenced(b, name)
		if b.Location != nil {
			for _, e := range b.Location.RepoSets {
				switch {
				case e.Err != nil || !e.InSource || e.Source != name:
				case e.Not:
					u.excluding.add([]string{e.Value}, b.Name)
				default:
					u.sets.add([]string{e.Value}, b.Name)
				}
			}
		}
		u.named.add(ref.Named, b.Name)
		u.using.add(ref.Used, b.Name)
		for _, it := range ref.Files {
			u.files[it.Rel] = true
		}
		for _, it := range ref.Skills {
			u.dirs = append(u.dirs, it.Rel)
		}
		for _, e := range ref.Errors {
			u.errors = append(u.errors, reviewError(env, name, e))
		}
	}
	return u
}

// reviewError is e in the review of source name: the config's own errors
// without its file name, and paths in the source as SOURCE:PATH.
func reviewError(env *resolve.Env, name string, e *catalog.Error) string {
	msg := e.Err.Error()
	if e.File != env.Path {
		msg = e.Error()
	}
	return strings.ReplaceAll(msg, reviewTop(name)+string(filepath.Separator), name+":")
}

// addOffered adds what the collections of source name's manifest in cat
// offer, each read by itself, and their errors.
func (u *used) addOffered(env *resolve.Env, cat *catalog.Catalog, name string) {
	m, _ := cat.Manifest(cat.Source(name))
	if m == nil {
		return
	}
	for _, c := range m.Collections {
		x, ok := cat.Collection(name, c.Name)
		if !ok {
			continue
		}
		for _, it := range x.Files {
			if it.Source == name {
				u.files[it.Rel] = true
			}
		}
		for _, it := range x.Skills {
			if it.Source == name {
				u.dirs = append(u.dirs, it.Rel)
			}
		}
		for _, e := range x.Errors {
			if msg := reviewError(env, name, e); !slices.Contains(u.errors, msg) {
				u.errors = append(u.errors, msg)
			}
		}
	}
}

// printErrors prints the errors of now that before doesn't have.
func (s *sources) printErrors(now, before []string) {
	for _, e := range now {
		if !slices.Contains(before, e) {
			fmt.Fprintf(s.Out, "  error: %s\n", source.Sanitize(e))
		}
	}
}

// review shows what changes in source cs from its accepted commit old to
// commit, whose files are in was and now, or from nothing when old is "":
// files the config uses (with either commit) in full, every other changed
// file by name, new errors of the entries that use the source, its MCP
// servers, and content warnings for the added lines shown. With s.full,
// every changed file in full. It reads only ctxtra's copy of the
// repository in the cache.
func (s *sources) review(env *resolve.Env, r *source.Repo, cs *catalog.Source, old, commit string, was, now *source.Snapshot) error {
	name, subdir := cs.Name, config.CleanSubdir(cs.Subdir)
	base, head := emptyTree(commit), source.Treeish(commit, subdir)
	if old != "" {
		base = source.Treeish(old, subdir)
	}
	changes, err := r.Changes(s.ctx, base, head)
	if err != nil {
		return err
	}
	top := reviewTop(name)
	after := env.Catalog.WithFiles(name, now, top)
	u, bu := usedFrom(env, after, name), &used{}
	if old == "" {
		// Reviewed from nothing, as source add shows a source.
		u.addOffered(env, after, name)
	}
	var before *catalog.Catalog
	if old != "" {
		before = env.Catalog.WithFiles(name, was, top)
		bu = usedFrom(env, before, name)
	}
	for f := range bu.files {
		u.files[f] = true
	}
	for c, bs := range bu.named {
		for _, b := range bs {
			u.named.add([]string{c}, b)
		}
	}
	for c, bs := range bu.using {
		for _, b := range bs {
			u.using.add([]string{c}, b)
		}
	}
	u.dirs = append(u.dirs, bu.dirs...)
	s.printErrors(u.errors, bu.errors)
	if before != nil {
		s.collectionChanges(name, u, before, after)
	}
	s.setChanges(name, u, before, after)
	s.mcpDefinitionWarnings(name, before, after)

	var shown, other []source.Change
	for _, c := range changes {
		if s.full || u.has(c.Path) {
			shown = append(shown, c)
		} else {
			other = append(other, c)
		}
	}
	switch {
	case len(changes) == 0 && subdir != "":
		fmt.Fprintf(s.Out, "nothing changed under %s\n", source.Sanitize(subdir))
	case len(changes) == 0:
		fmt.Fprintln(s.Out, "no files changed")
	case len(shown) == 0:
		fmt.Fprintln(s.Out, "no changes in files you use")
	}
	// Content warnings for the regular files shown. URLs that the accepted
	// commit has in the files used or shown are not new.
	cc := s.contentCheck(name)
	check := map[string]bool{}
	for _, c := range shown {
		cc.newExecutable(s.Out, c, true)
		check[c.Path] = regular(c.NewMode)
	}
	for _, c := range other {
		cc.newExecutable(s.Out, c, false)
	}
	inShown := map[string]bool{}
	for _, c := range shown {
		inShown[c.Path] = true
	}
	if old != "" {
		cc.known(was, func(p string) bool { return u.has(p) || inShown[p] })
	}
	// In batches, so that a large skill doesn't run git once per file.
	for batch := range slices.Chunk(shown, 100) {
		var ps []string
		for _, c := range batch {
			ps = append(ps, c.Path)
		}
		d, err := r.Diff(s.ctx, base, head, ps...)
		if err != nil {
			return err
		}
		cc.printDiff(s.Out, d, now, check)
	}
	if len(other) > 0 {
		fmt.Fprintln(s.Out, "also changed, not used by you:")
		for _, c := range other {
			fmt.Fprintf(s.Out, "  %s\n", describeChange(c))
		}
		fmt.Fprintln(s.Out, "Accepting accepts these files too: a file you reference later is used without review. ctxtra source update --full shows them, with content warnings.")
	}
	return nil
}

// collectionChanges reports, for each collection of source name that the
// bindings use with either commit, nested ones included, whether the new
// commit removes it, and for each that they name, what it newly adds:
// servers, skills and files.
func (s *sources) collectionChanges(name string, u *used, before, after *catalog.Catalog) {
	m, _ := after.Manifest(after.Source(name))
	by := func(bindings []string) string {
		if len(bindings) > 1 {
			return "bindings " + strings.Join(bindings, ", ") + " use it"
		}
		return "binding " + bindings[0] + " uses it"
	}
	for _, c := range slices.Sorted(maps.Keys(u.using)) {
		if m == nil || m.Collection(c) == nil {
			msg := fmt.Sprintf("collection %s is removed; %s", c, by(u.using[c]))
			fmt.Fprintf(s.Out, "%s\n", source.Sanitize(msg))
			s.diags = append(s.diags, diagnostic{"warning", source.Sanitize(name + ": " + msg)})
		}
	}
	for _, c := range slices.Sorted(maps.Keys(u.named)) {
		// A removed collection is reported above; one with errors, by the
		// review's errors.
		now, ok := after.Collection(name, c)
		if !ok {
			continue
		}
		was, _ := before.Collection(name, c)
		if was == nil {
			was = &catalog.Expansion{}
		}
		var added []string
		for _, srv := range now.Servers {
			if !slices.ContainsFunc(was.Servers, func(o catalog.Server) bool { return o.ID == srv.ID }) {
				added = append(added, "MCP server "+strings.TrimPrefix(srv.ID, name+":"))
			}
		}
		for _, l := range []struct {
			what     string
			now, was []catalog.Item
		}{{"skill", now.Skills, was.Skills}, {"file", now.Files, was.Files}} {
			for _, it := range l.now {
				if !slices.ContainsFunc(l.was, func(o catalog.Item) bool { return o.Ref() == it.Ref() }) {
					added = append(added, l.what+" "+it.Rel)
				}
			}
		}
		if len(added) > 0 {
			msg := fmt.Sprintf("collection %s newly adds %s; %s", c, strings.Join(added, ", "), by(u.named[c]))
			fmt.Fprintf(s.Out, "%s\n", source.Sanitize(msg))
			s.diags = append(s.diags, diagnostic{"notice", source.Sanitize(name + ": " + msg)})
		}
	}
}

// setChanges reports, for each repo set of source name that the bindings
// use in location, whether the new commit removes it, or the entries it
// adds to or removes from it, and warns about added entries that match
// every repository of a host. before is nil for a review from nothing.
func (s *sources) setChanges(name string, u *used, before, after *catalog.Catalog) {
	sets := func(cat *catalog.Catalog) map[string]*config.RepoSet {
		out := map[string]*config.RepoSet{}
		if cat == nil {
			return out
		}
		if m, _ := cat.Manifest(cat.Source(name)); m != nil {
			for _, set := range m.RepoSets {
				out[set.Name] = set
			}
		}
		return out
	}
	was, now := sets(before), sets(after)
	report := func(severity, msg string) {
		fmt.Fprintf(s.Out, "%s\n", source.Sanitize(msg))
		s.diags = append(s.diags, diagnostic{severity, source.Sanitize(name + ": " + msg)})
	}
	names := slices.Sorted(maps.Keys(u.sets))
	for c := range u.excluding {
		if !slices.Contains(names, c) {
			names = append(names, c)
		}
	}
	slices.Sort(names)
	for _, c := range names {
		users := setUsers(u.sets[c], u.excluding[c])
		set := now[c]
		if set == nil {
			var after []string
			if len(u.sets[c]) > 0 {
				after = append(after, "it matches nothing for "+strings.Join(u.sets[c], ", "))
			}
			if len(u.excluding[c]) > 0 {
				after = append(after, strings.Join(u.excluding[c], ", ")+" "+verb(u.excluding[c], "is", "are")+" left out")
			}
			report("warning", fmt.Sprintf("repo set %s is removed; %s; once the commit is accepted, %s", c, users, strings.Join(after, " and ")))
			continue
		}
		// A set with errors that leave it out is reported by the review's
		// errors.
		if set.Broken() {
			continue
		}
		old := setEntryTexts(was[c])
		var added, removed []string
		for _, e := range set.Entries {
			if t := repoEntryText(e); !slices.Contains(old, t) {
				added = append(added, t)
			}
		}
		for _, t := range old {
			if !slices.Contains(setEntryTexts(set), t) {
				removed = append(removed, t)
			}
		}
		var change []string
		if len(added) > 0 {
			change = append(change, "adds "+strings.Join(added, ", "))
		}
		if len(removed) > 0 {
			change = append(change, "removes "+strings.Join(removed, ", "))
		}
		if len(change) > 0 {
			report("notice", fmt.Sprintf("repo set %s %s; %s", c, strings.Join(change, " and "), users))
		}
		for _, e := range set.Entries {
			if host, ok := wholeHost(e); ok && slices.Contains(added, repoEntryText(e)) {
				report("warning", fmt.Sprintf("repo set %s: %s matches every repository on %s; %s", c, e.Text, host, users))
			}
		}
	}
}

// setUsers names the bindings that use a set and those that exclude it.
func setUsers(using, excluding []string) string {
	var parts []string
	if len(using) > 0 {
		parts = append(parts, bindingNames(using)+" "+verb(using, "uses", "use")+" it")
	}
	if len(excluding) > 0 {
		parts = append(parts, bindingNames(excluding)+" "+verb(excluding, "excludes", "exclude")+" it")
	}
	return strings.Join(parts, ", ")
}

func bindingNames(bindings []string) string {
	if len(bindings) > 1 {
		return "bindings " + strings.Join(bindings, ", ")
	}
	return "binding " + bindings[0]
}

func verb(subjects []string, one, many string) string {
	if len(subjects) > 1 {
		return many
	}
	return one
}

// setEntryTexts are the entries of set as repoEntryText writes them; none for
// a nil set.
func setEntryTexts(set *config.RepoSet) []string {
	if set == nil {
		return nil
	}
	var out []string
	for _, e := range set.Entries {
		out = append(out, repoEntryText(e))
	}
	return out
}

// repoEntryText is a set's entry as one line: the repository, with its
// subdirs when it has any.
func repoEntryText(e config.RepoEntry) string {
	if e.Subdirs == nil {
		return e.Text
	}
	var subs []string
	for _, sub := range e.Subdirs {
		subs = append(subs, sub.Text)
	}
	return e.Text + " (subdirs " + strings.Join(subs, ", ") + ")"
}

// wholeHost reports whether e is a positive entry of the form HOST/**,
// which matches every repository of HOST.
func wholeHost(e config.RepoEntry) (string, bool) {
	if e.Err != nil || e.Not {
		return "", false
	}
	host, rest, ok := strings.Cut(config.RepoID(e.Repo), "/")
	return host, ok && rest == "**"
}

// regular reports whether a git mode is a regular file's.
func regular(mode string) bool { return mode == "100644" || mode == "100755" }

// emptyTree is git's empty tree in the object format of commit: the base of
// the review of a source's first commit. Git knows it without the object.
func emptyTree(commit string) string {
	if len(commit) == 64 {
		return "6ef19b41225c5369f1c104d45d8d85efa9b057b53b14b4b9b939dd74decc5321"
	}
	return "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
}

// describeChange is one line about a changed file that isn't shown in full.
func describeChange(c source.Change) string {
	mode := func(m string) string {
		switch m {
		case "100755":
			return "executable"
		case "120000":
			return "symlink (left out)"
		case "160000":
			return "submodule (left out)"
		}
		return ""
	}
	note := ""
	switch c.Status {
	case 'A':
		note = mode(c.NewMode)
		return strings.TrimSpace(fmt.Sprintf("added    %s  %s", c.Path, note))
	case 'D':
		return "removed  " + c.Path
	}
	if c.OldMode != c.NewMode {
		note = fmt.Sprintf("mode %s -> %s", strings.TrimPrefix(c.OldMode, "100"), strings.TrimPrefix(c.NewMode, "100"))
		if m := mode(c.NewMode); m != "" {
			note += ", now " + m
		}
	}
	if c.SameContent {
		return "mode     " + c.Path + "  " + note
	}
	return strings.TrimSpace("changed  " + c.Path + "  " + note)
}
