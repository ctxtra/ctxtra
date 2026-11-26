package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/paths"
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
}

func (u *used) has(p string) bool {
	return u.files[p] || slices.ContainsFunc(u.dirs, func(d string) bool { return d == "." || strings.HasPrefix(p, d+"/") })
}

// usedFrom is what the bindings of env take from source name in cat, under
// any conditions: its manifest, and every file and skill folder that their
// entries name in it.
func usedFrom(env *resolve.Env, cat *catalog.Catalog, name string) *used {
	u := &used{files: map[string]bool{manifest.FileName: true}}
	for _, b := range env.Config.Bindings {
		for _, k := range []struct {
			list []config.Entry
			kind catalog.Kind
		}{{b.Instructions, catalog.Files}, {b.Skills, catalog.Skills}} {
			for _, e := range k.list {
				if e.Not || e.Err != nil || !e.InSource || e.Source != name {
					continue
				}
				items, errs := cat.Paths(e, k.kind)
				for _, it := range items {
					if k.kind == catalog.Files {
						u.files[it.Rel] = true
					} else {
						u.dirs = append(u.dirs, it.Rel)
					}
				}
				for _, err := range errs {
					u.errors = append(u.errors, fmt.Sprintf("binding %s: %s: %v", b.Name, e.Text, err))
				}
			}
		}
	}
	return u
}

// review shows what changes in source cs from its accepted commit old, or
// from nothing when old is "", to commit: files the config uses (with the
// accepted commit or the new one) in full, every other changed file by
// name, new errors of the entries that use the source, its MCP servers,
// and content warnings for the added lines shown. With s.full, every
// changed file in full. Both commits must be in the store.
func (s *sources) review(env *resolve.Env, r *source.Repo, cs *catalog.Source, old, commit string) error {
	name := cs.Name
	base := old
	if base == "" {
		base = emptyTree(commit)
	}
	changes, err := r.Changes(s.ctx, base, commit)
	if err != nil {
		return err
	}
	newTop := source.GenDir(r.URL, commit)
	after := env.Catalog.WithTop(name, newTop)
	u := usedFrom(env, after, name)
	var before *catalog.Catalog
	oldTop := ""
	if old != "" {
		oldTop = source.GenDir(r.URL, old)
		if e := env.Lock.Entry(name); e != nil && e.Commit == old {
			oldTop = catalog.CommitDir(e)
		}
		before = env.Catalog.WithTop(name, oldTop)
		bu := usedFrom(env, before, name)
		for f := range bu.files {
			u.files[f] = true
		}
		u.dirs = append(u.dirs, bu.dirs...)
		for _, e := range u.errors {
			if !slices.Contains(bu.errors, strings.ReplaceAll(e, paths.Tilde(newTop), paths.Tilde(oldTop))) {
				fmt.Fprintf(s.Out, "  error: %s\n", source.Sanitize(strings.ReplaceAll(e, paths.Tilde(newTop)+"/", name+":")))
			}
		}
	} else {
		for _, e := range u.errors {
			fmt.Fprintf(s.Out, "  error: %s\n", source.Sanitize(strings.ReplaceAll(e, paths.Tilde(newTop)+"/", name+":")))
		}
	}
	s.mcpDefinitionWarnings(name, before, after)

	var shown, other []source.Change
	for _, c := range changes {
		if s.full || u.has(c.Path) {
			shown = append(shown, c)
		} else {
			other = append(other, c)
		}
	}
	if len(shown) == 0 {
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
	if old != "" {
		inShown := map[string]bool{}
		for _, c := range shown {
			inShown[c.Path] = true
		}
		cc.known(oldTop, func(p string) bool { return u.has(p) || inShown[p] })
	}
	// In batches, so that a large skill doesn't run git once per file.
	for batch := range slices.Chunk(shown, 100) {
		var ps []string
		for _, c := range batch {
			ps = append(ps, c.Path)
		}
		d, err := r.Diff(s.ctx, base, commit, ps...)
		if err != nil {
			return err
		}
		cc.printDiff(s.Out, d, newTop, check)
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
