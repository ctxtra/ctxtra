package resolve

import (
	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Referenced is what binding b takes from source name, whatever its
// conditions: the files and skill folders that its entries of that source
// add after its ! entries, before content reads and limits filter them, and
// the errors a session reports for those entries: what a positive entry
// can't find, a ! entry that excludes nothing, and a SKILL.md that can't be
// read or parsed. Nothing of other sources is read.
func (env *Env) Referenced(b *config.Binding, name string) (files, skills []catalog.Item, errs []*catalog.Error) {
	r := &session{env: env, rd: env.reader(), res: &Result{}, files: map[string]bool{}, skills: map[string]bool{}}
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
	files = r.items(b, t, of(b.Instructions), catalog.Files)
	skills = r.items(b, t, of(b.Skills), catalog.Skills)
	for _, it := range skills {
		md := it.Child("SKILL.md")
		if content, _, ok := r.read(b, t, md, r.skills); ok {
			if _, _, _, err := parseSkill(string(content)); err != nil {
				r.fail(t, b, yamlx.Pos{}, yamlx.UnreadableFile, "%s: %v", md.Ref(), err)
			}
		}
	}
	return files, skills, t.Errors
}
