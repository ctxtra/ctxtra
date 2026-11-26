package cli

import (
	"fmt"
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
	u := &used{files: map[string]bool{manifest.FileName: true}}
	alone := *env
	alone.Catalog = cat
	for _, b := range env.Config.Bindings {
		if b.Broken() {
			continue
		}
		files, skills, _, errs := alone.Referenced(b, name)
		for _, it := range files {
			u.files[it.Rel] = true
		}
		for _, it := range skills {
			u.dirs = append(u.dirs, it.Rel)
		}
		for _, e := range errs {
			msg := e.Err.Error()
			if e.File != env.Path {
				msg = e.Error()
			}
			u.errors = append(u.errors, strings.ReplaceAll(msg, reviewTop(name)+string(filepath.Separator), name+":"))
		}
	}
	return u
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
	var before *catalog.Catalog
	if old != "" {
		before = env.Catalog.WithFiles(name, was, top)
		bu = usedFrom(env, before, name)
	}
	for f := range bu.files {
		u.files[f] = true
	}
	u.dirs = append(u.dirs, bu.dirs...)
	s.printErrors(u.errors, bu.errors)
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
