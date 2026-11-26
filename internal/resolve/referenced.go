package resolve

import (
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Referenced is what binding b takes from source name, whatever its
// conditions: the files and skill folders that its entries of that source
// and its collections add after its ! entries, before content reads and
// limits filter them, the source's collections it names, directly or in
// the config's own collections, and the errors a session reports for those
// entries: what a positive entry can't find, a ! entry that excludes
// nothing, an error in a collection, and a SKILL.md that can't be read or
// parsed. Of other sources, only what the config's own collections name is
// read.
func (env *Env) Referenced(b *config.Binding, name string) (files, skills []catalog.Item, entered []string, errs []*catalog.Error) {
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
	files = r.items(b, t, of(b.Instructions), catalog.Files, inSource(x.Files))
	skills = r.items(b, t, of(b.Skills), catalog.Skills, inSource(x.Skills))
	for _, it := range skills {
		md := it.Child("SKILL.md")
		if content, _, ok := r.read(b, t, md, r.skills); ok {
			if _, _, _, err := parseSkill(string(content)); err != nil {
				r.fail(t, b, yamlx.Pos{}, yamlx.UnreadableFile, "%s: %v", md.Ref(), err)
			}
		}
	}
	for _, ref := range x.Entered {
		if src, _, _ := strings.Cut(ref, ":"); src == name {
			entered = append(entered, ref)
		}
	}
	for _, s := range env.Catalog.Sources() {
		if r.manifests[s.Name] && s.Name == name {
			t.Errors = append(t.Errors, s.ManifestErrors()...)
		}
	}
	return files, skills, entered, t.Errors
}
