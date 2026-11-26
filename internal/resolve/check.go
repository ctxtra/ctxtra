package resolve

import (
	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
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
