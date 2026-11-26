package resolve

import (
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Reference is what a binding takes from one source.
type Reference struct {
	// Files and Skills are the files and skill folders that its entries of
	// the source and its collections add.
	Files, Skills []catalog.Item
	// Named are the source's collections that it names, directly or in the
	// config's own collections; Used every collection of the source it
	// uses, nested ones included, after its ! entries. Both by name.
	Named, Used []string
	// Errors are the errors a session reports for those entries.
	Errors []*catalog.Error
}

// Referenced is what binding b takes from source name, whatever its
// conditions: files and skill folders after its ! entries, before content
// reads and limits filter them, its collections, and the errors a session
// reports for those entries: what a positive entry can't find, a ! entry
// that excludes nothing, an error in a collection, a repo set that can't
// be read, and a SKILL.md that can't be read or parsed. Of other sources,
// only what the config's own collections name is read.
func (env *Env) Referenced(b *config.Binding, name string) Reference {
	r := &session{env: env, rd: env.reader(), res: &Result{}, files: map[string]bool{}, skills: map[string]bool{},
		manifests: map[string]bool{}}
	t := &Trace{Origin: b.Name}
	of := func(list []config.Entry) []config.Entry {
		var out []config.Entry
		for _, e := range list {
			if e.InSource && e.Source == name {
				out = append(out, e)
			}
		}
		return out
	}
	// The config's own collections and the source's, without other
	// sources' collections.
	var colls []config.Entry
	for _, e := range b.Collections {
		if !e.InSource || e.Source == name {
			colls = append(colls, e)
		}
	}
	x := r.collections(&config.Binding{Name: b.Name, Content: config.Content{Collections: colls}}, t)
	inSource := func(items []catalog.Item) []catalog.Item {
		return slices.DeleteFunc(slices.Clone(items), func(it catalog.Item) bool { return it.Source != name })
	}
	ref := Reference{
		Files:  r.items(b, t, of(b.Instructions), catalog.Files, inSource(x.Files)),
		Skills: r.items(b, t, of(b.Skills), catalog.Skills, inSource(x.Skills)),
		Named:  ofSource(x.Entered, name),
		Used:   ofSource(x.Used, name),
	}
	if b.Location != nil {
		r.repoSets(&config.Binding{Name: b.Name, Location: &config.Location{RepoSets: of(b.Location.RepoSets)}}, t)
	}
	for _, it := range ref.Skills {
		md := it.Child("SKILL.md")
		if content, _, ok := r.read(b, t, md, r.skills); ok {
			if _, _, _, err := parseSkill(string(content)); err != nil {
				r.fail(t, b, yamlx.Pos{}, yamlx.UnreadableFile, "%s: %v", md.Ref(), err)
			}
		}
	}
	if s := env.Catalog.Source(name); s != nil && r.manifests[name] {
		t.Errors = append(t.Errors, s.ManifestErrors()...)
	}
	ref.Errors = t.Errors
	return ref
}

// ofSource are the names of the collections of source name in refs, which
// are SOURCE:NAME or the config's own NAME.
func ofSource(refs []string, name string) []string {
	var out []string
	for _, ref := range refs {
		if src, coll, ok := strings.Cut(ref, ":"); ok && src == name {
			out = append(out, coll)
		}
	}
	return out
}
