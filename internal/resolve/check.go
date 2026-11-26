package resolve

import (
	"slices"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Alone is what binding b adds by itself, as a session whose conditions
// all hold would get it, with its errors: for a command that writes a
// binding, and for doctor, which checks every binding. It reads with its
// own limits: the limits are per session.
func (env *Env) Alone(b *config.Binding, in Input) *Result {
	r := &session{env: env, in: in, dir: in.Dir, repoKnown: true,
		rd:    env.reader(),
		files: map[string]bool{}, skills: map[string]bool{}, servers: map[string]bool{},
		manifests: map[string]bool{}}
	r.res = &Result{Dir: in.Dir, Summary: summary(env.Config.SessionMessage)}
	t := Trace{Origin: b.Name, Matched: true}
	if !b.Broken() {
		r.add(b, &t)
	}
	r.res.Errors = append(r.res.Errors, t.Errors...)
	for _, s := range env.Catalog.Sources() {
		if r.manifests[s.Name] {
			r.res.Errors = append(r.res.Errors, s.ManifestErrors()...)
		}
	}
	r.res.Trace = []Trace{t}
	if m := env.Config.MaxCtxChars; m != nil && *m > 0 {
		r.res.MaxChars = *m
		r.res.Fit(func(res *Result) bool { return Chars(res.Render()) <= res.MaxChars }, "max-ctx-chars")
	}
	return r.res
}

// OtherErrors are the errors that the bindings that didn't apply in res
// would have where they apply: doctor checks every binding, not only the
// ones of its folder. Each error is reported once.
func (env *Env) OtherErrors(res *Result) []*catalog.Error {
	seen := map[string]bool{}
	for _, e := range res.Errors {
		seen[e.Error()] = true
	}
	var out []*catalog.Error
	for i, b := range env.Config.Bindings {
		if i >= len(res.Trace) || res.Trace[i].Matched || b.Broken() {
			continue
		}
		for _, e := range env.Alone(b, Input{Dir: res.Dir}).Errors {
			if !seen[e.Error()] {
				seen[e.Error()] = true
				out = append(out, e)
			}
		}
	}
	return out
}

// CheckCollections are the errors that sessions would have in the
// collections of source name's manifest, each read by itself: those of
// their entries, cycles, and each skill's SKILL.md that can't be read or
// parsed, as an error of the first collection that adds the skill.
func CheckCollections(cat *catalog.Catalog, name string) []*catalog.Error {
	s := cat.Source(name)
	if s == nil {
		return nil
	}
	m, _ := cat.Manifest(s)
	if m == nil {
		return nil
	}
	var out []*catalog.Error
	skills := map[string]bool{}
	for _, c := range m.Collections {
		x, ok := cat.Collection(name, c.Name)
		if !ok {
			continue
		}
		for _, e := range x.Errors {
			if !slices.Contains(out, e) {
				out = append(out, e)
			}
		}
		for _, it := range x.Skills {
			md := it.Child("SKILL.md")
			if skills[md.Path] {
				continue
			}
			skills[md.Path] = true
			content, _, err := cat.Read(md, nil)
			if err == nil {
				_, _, _, err = parseSkill(string(content))
			}
			if err != nil {
				out = append(out, &catalog.Error{File: name + ":" + manifest.FileName, Err: &yamlx.Error{Pos: c.Pos,
					Mistake: yamlx.UnreadableFile, Part: yamlx.Part{Kind: "collection", Name: c.Name}, Msg: md.Ref() + ": " + err.Error()}})
			}
		}
	}
	return out
}
