package resolve

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ctxtra/ctxtra/internal/caserule"
	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/gitrepo"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Condition is one of a binding's conditions in a session.
type Condition struct {
	// Key is location, profiles or agents.
	Key  string
	Held bool
	// Why says what held, or what didn't.
	Why string
	// Not is the ! entry that kept the condition from holding, "" for none.
	Not string
}

// Exclusion is an item that a ! entry removed from a binding.
type Exclusion struct {
	// Entry is the ! entry as written.
	Entry string
	// Item is the file, skill folder or server it removed, as the config
	// names it.
	Item string
}

// SameSkill is a skill whose name an earlier skill of the session has.
type SameSkill struct {
	Name         string
	First, Other Hit
}

// Resolve works out what session in gets. Every binding's conditions
// are checked first; only a binding whose conditions all hold has its
// content read. Bindings apply in config order, and a file, skill or server
// that several add counts once, at its first position.
func (env *Env) Resolve(in Input) *Result {
	dir := paths.Real(in.Dir)
	r := &session{env: env, in: in, dir: dir, repo: in.Repo, repoKnown: in.Repo != nil,
		rd:    env.reader(),
		files: map[string]bool{}, skills: map[string]bool{}, servers: map[string]bool{},
		manifests: map[string]bool{}}
	r.res = &Result{Dir: dir, Agent: in.Agent, Summary: summary(env.Config.SessionMessage), Profiles: in.Profiles}
	r.res.Errors = slices.Concat(r.res.Errors, env.Errors, in.Profiles.Errors, env.unusedProfiles(in.Profiles.Names))
	for _, b := range env.Config.Bindings {
		t := Trace{Origin: b.Name}
		if b.Broken() {
			t.Reason = "the binding has errors"
		} else {
			t.Conditions = r.conditions(b, &t)
			t.Matched = !slices.ContainsFunc(t.Conditions, func(c Condition) bool { return !c.Held })
			var why []string
			for _, c := range t.Conditions {
				why = append(why, c.Key+": "+c.Why)
			}
			t.Reason = strings.Join(why, "; ")
			if len(t.Conditions) == 0 {
				t.Reason = "no conditions: every session"
			}
			if t.Matched {
				r.add(b, &t)
			}
		}
		r.res.Errors = append(r.res.Errors, t.Errors...)
		r.res.Trace = append(r.res.Trace, t)
	}
	for _, s := range env.Catalog.Sources() {
		if r.manifests[s.Name] {
			r.res.Errors = append(r.res.Errors, s.ManifestErrors()...)
		}
	}
	r.res.Repo, r.res.RepoErr = r.repo, r.repoErr
	r.res.sameNames()
	r.res.updateNotices(env)
	if m := env.Config.MaxCtxChars; m != nil && *m > 0 {
		r.res.MaxChars = *m
		r.res.Fit(func(res *Result) bool { return Chars(res.Render()) <= res.MaxChars }, "max-ctx-chars")
	}
	return r.res
}

// unusedProfiles are the errors of active profiles that no binding uses:
// likely typos. A config that can't be read has no bindings to tell.
func (env *Env) unusedProfiles(active []string) []*catalog.Error {
	if slices.ContainsFunc(env.Config.Errors, func(e *yamlx.Error) bool { return e.Out() == yamlx.OutFile }) {
		return nil
	}
	used := profile.Used(env.Config)
	var out []*catalog.Error
	for _, n := range active {
		if used[n] == nil {
			out = append(out, &catalog.Error{Err: &yamlx.Error{Mistake: yamlx.UnusedProfile, Part: yamlx.Part{Kind: "profile", Name: n},
				Msg: "no binding uses it; check the name for a typo"}})
		}
	}
	return out
}

// summary is the config's session-message settings.
func summary(m config.SessionMessage) SessionMessage {
	s := DefaultSessionMessage
	if m.MaxInstructions != nil {
		s.MaxInstructions = *m.MaxInstructions
	}
	if m.MaxSkills != nil {
		s.MaxSkills = *m.MaxSkills
	}
	return s
}

// session is one Resolve.
type session struct {
	env       *Env
	in        Input
	dir       string
	repo      *gitrepo.Repo
	repoErr   error
	repoKnown bool
	rule      *caserule.Rule
	rd        *fsutil.Reader
	res       *Result
	// files and skills are the real paths of the files and SKILL.md files
	// added; servers the IDs of the servers.
	files, skills, servers map[string]bool
	// manifests are the sources whose manifests this session read.
	manifests map[string]bool
	// reported are the errors in collections reported already.
	reported map[*catalog.Error]bool
}

// repository is the session's repository, found once when first needed:
// nil outside git, and an error when git can't tell.
func (r *session) repository() (*gitrepo.Repo, error) {
	if !r.repoKnown {
		r.repo, r.repoErr = detectRepo(r.dir)
		r.repoKnown = true
	}
	return r.repo, r.repoErr
}

// detectRepo is gitrepo.Detect; tests make git fail.
var detectRepo = gitrepo.Detect

// caseRuleOf is caserule.Of; tests set the rule of a volume.
var caseRuleOf = caserule.Of

// caseRule is the case rule of the session folder's volume, found once.
func (r *session) caseRule() caserule.Rule {
	if r.rule == nil {
		rule := caseRuleOf(r.dir)
		r.rule = &rule
		r.res.CaseRule = rule
	}
	return *r.rule
}

// fail adds an error of binding b's entry at pos.
func (r *session) fail(t *Trace, b *config.Binding, pos yamlx.Pos, m yamlx.Mistake, format string, args ...any) {
	t.Errors = append(t.Errors, &catalog.Error{File: r.env.Path, Err: &yamlx.Error{
		Pos: pos, Mistake: m, Part: yamlx.Part{Kind: "binding", Name: b.Name}, Msg: fmt.Sprintf(format, args...)}})
}

// conditions checks each condition that b has, the cheap ones first.
func (r *session) conditions(b *config.Binding, t *Trace) []Condition {
	var out []Condition
	if b.Agents != nil {
		out = append(out, agents(b.Agents, r.in.Agent))
	}
	if b.Profiles != nil {
		out = append(out, profiles(b.Profiles, r.in.Profiles.Names))
	}
	if b.Location != nil {
		out = append(out, r.location(b, t))
	}
	return out
}

// split are the values of a condition's positive and ! entries.
func split(list []config.Entry) (pos, not []config.Entry) {
	for _, e := range list {
		if e.Not {
			not = append(not, e)
		} else {
			pos = append(pos, e)
		}
	}
	return pos, not
}

func values(list []config.Entry) string {
	var out []string
	for _, e := range list {
		out = append(out, e.Value)
	}
	return strings.Join(out, ", ")
}

// agents checks the agents condition: names compare without regard to
// case. Without an agent, no binding with agents applies.
// ForAgent reports whether a binding with the agents condition list, nil
// for none, applies in a session of agent.
func ForAgent(list []config.Entry, agent string) bool {
	return list == nil || agents(list, agent).Held
}

func agents(list []config.Entry, agent string) Condition {
	c := Condition{Key: "agents"}
	pos, not := split(list)
	switch {
	case agent == "" && len(pos) > 0:
		c.Why = "for " + values(pos) + "; no agent given"
		return c
	case agent == "":
		c.Why = "for every agent but " + values(not) + "; no agent given"
		return c
	}
	for _, e := range not {
		if strings.EqualFold(e.Value, agent) {
			c.Why, c.Not = "not for "+agent, e.Text
			return c
		}
	}
	if len(pos) > 0 && !slices.ContainsFunc(pos, func(e config.Entry) bool { return strings.EqualFold(e.Value, agent) }) {
		c.Why = fmt.Sprintf("for %s, not %s", values(pos), agent)
		return c
	}
	c.Held, c.Why = true, agent
	return c
}

// profiles checks the profiles condition; case counts.
func profiles(list []config.Entry, active []string) Condition {
	c := Condition{Key: "profiles"}
	pos, not := split(list)
	act := "none active"
	if len(active) > 0 {
		act = "active: " + strings.Join(active, ", ")
	}
	for _, e := range not {
		if slices.Contains(active, e.Value) {
			c.Why, c.Not = "not while "+e.Value+" is active", e.Text
			return c
		}
	}
	if len(pos) > 0 && !slices.ContainsFunc(pos, func(e config.Entry) bool { return slices.Contains(active, e.Value) }) {
		c.Why = fmt.Sprintf("needs %s; %s", values(pos), act)
		return c
	}
	c.Held, c.Why = true, act
	return c
}

// check is one of location's two checks while it is worked out.
type check struct {
	present, positive, matched bool
	not                        string // the ! entry that matched
	why                        string
}

// holds reports whether the check holds: a positive entry matches, or
// there is none, and no ! entry does.
func (c check) holds() bool { return c.present && (!c.positive || c.matched) && c.not == "" }

// location checks where the session starts: the folder check (dirs) and
// the repository check (repos and repo-sets), joined with OR. A ! entry
// never crosses checks.
func (r *session) location(b *config.Binding, t *Trace) Condition {
	c := Condition{Key: "location"}
	folders, left := r.dirs(b, t)
	if left {
		c.Why = "a ! dirs entry can't be checked"
		return c
	}
	repos, out := r.repos(b, t)
	if out != "" {
		c.Why = out
		return c
	}
	var why []string
	for _, k := range []check{folders, repos} {
		if !k.present {
			continue
		}
		c.Held = c.Held || k.holds()
		if k.not != "" && c.Not == "" {
			c.Not = k.not
		}
		why = append(why, k.why)
	}
	c.Why = strings.Join(why, "; ")
	return c
}

// dirs is the folder check: the session folder, or its repository's main
// checkout, against the entries. Folders compare as the session folder's
// volume does; when that is unknown, positive entries compare exactly and !
// entries without regard to case, so the binding never applies wider. left
// is true when git can't tell the main checkout and a ! entry needs it,
// which leaves out the binding.
func (r *session) dirs(b *config.Binding, t *Trace) (k check, left bool) {
	list := b.Location.Dirs
	if len(list) == 0 {
		return k, false
	}
	k.present = true
	folders := []string{filepath.ToSlash(r.dir)}
	repo, err := r.repository()
	if err != nil {
		if e := firstNot(list); e != nil {
			r.fail(t, b, e.Pos, yamlx.BrokenExclusion, "dirs %s can't be checked against the repository's main checkout: %v", e.Text, err)
			return k, true
		}
	} else if repo != nil && repo.Root != r.dir {
		folders = append(folders, filepath.ToSlash(repo.Root))
	}
	rule := r.caseRule()
	for _, e := range list {
		if e.Not {
			if e.Err == nil && dirMatch(e.Value, folders, rule != caserule.Sensitive) {
				k.not = e.Text
			}
			continue
		}
		k.positive = true
		if e.Err == nil && !k.matched && dirMatch(e.Value, folders, rule == caserule.Insensitive) {
			k.matched = true
			k.why = "dirs " + e.Text + " matches " + paths.Tilde(r.dir)
		}
	}
	switch {
	case k.not != "":
		k.why = "dirs " + k.not + " leaves out " + paths.Tilde(r.dir)
	case k.positive && !k.matched:
		k.why = "no dirs entry matches " + paths.Tilde(r.dir)
	case !k.matched:
		k.why = "dirs: only ! entries, none matches " + paths.Tilde(r.dir)
	}
	if rule == caserule.Unknown {
		k.why += " (the folder's case rule is unknown)"
	}
	return k, false
}

// firstNot is the first ! entry of list that can be checked, nil if none.
func firstNot(list []config.Entry) *config.Entry {
	for i, e := range list {
		if e.Not && e.Err == nil {
			return &list[i]
		}
	}
	return nil
}

// dirMatch reports whether the folder pattern p matches one of folders.
func dirMatch(p string, folders []string, fold bool) bool {
	p = filepath.ToSlash(filepath.Clean(paths.Expand(p)))
	pats := []string{p, realPattern(p)}
	for _, f := range folders {
		for _, pat := range pats {
			if fold {
				pat, f = strings.ToLower(pat), strings.ToLower(f)
			}
			if globMatch(pat, f) {
				return true
			}
		}
	}
	return false
}

// realPattern resolves symlinks in the fixed start of a pattern, so that
// /tmp/** also matches /private/tmp/x on macOS.
func realPattern(pat string) string {
	if !strings.ContainsAny(pat, `*?[{\`) {
		return filepath.ToSlash(paths.Real(filepath.FromSlash(pat)))
	}
	base, rest := doublestar.SplitPattern(pat)
	real := strings.TrimSuffix(filepath.ToSlash(paths.Real(filepath.FromSlash(base))), "/")
	return real + "/" + rest
}

func globMatch(pattern, name string) bool {
	ok, err := doublestar.Match(pattern, name)
	return err == nil && ok
}

// repos is the repository check: repos and repo-sets together. left says
// why a ! entry can't be checked, which leaves out the binding: a !
// repo-sets entry whose set can't be read, or any ! entry when git can't
// tell the repository. Then positive entries match nothing.
func (r *session) repos(b *config.Binding, t *Trace) (k check, left string) {
	l := b.Location
	if len(l.Repos) == 0 && len(l.RepoSets) == 0 {
		return k, ""
	}
	k.present = true
	k.positive = slices.ContainsFunc(l.Repos, func(e config.RepoEntry) bool { return !e.Not }) ||
		slices.ContainsFunc(l.RepoSets, func(e config.Entry) bool { return !e.Not })
	sets, left := r.repoSets(b, t)
	if left != "" {
		return k, left
	}
	repo, err := r.repository()
	if err != nil {
		for _, e := range l.Repos {
			if e.Err != nil {
				continue
			}
			if e.Not {
				r.fail(t, b, e.Pos, yamlx.BrokenExclusion, "repos %s can't be checked: %v", e.Text, err)
				return k, "a ! entry of the repository check can't be checked"
			}
			for _, sub := range e.Subdirs {
				if sub.Not && sub.Err == nil {
					r.fail(t, b, sub.Pos, yamlx.BrokenExclusion, "repos %s subdirs %s can't be checked: %v", e.Repo, sub.Text, err)
					return k, "a ! entry of the repository check can't be checked"
				}
			}
		}
		for _, s := range sets {
			if s.entry.Not {
				r.fail(t, b, s.entry.Pos, yamlx.BrokenExclusion, "repo-sets %s can't be checked: %v", s.entry.Text, err)
				return k, "a ! entry of the repository check can't be checked"
			}
		}
		k.why = fmt.Sprintf("repos: the repository can't be told (%v), so no entry matches", err)
		return k, ""
	}
	// subNot is a subdirs ! entry that kept an entry from matching.
	var subNot string
	for _, e := range l.Repos {
		if e.Err != nil {
			continue
		}
		hit, not := repoMatch(e, repo)
		switch {
		case not != "" && subNot == "":
			subNot = e.Repo + " subdirs " + not
		case !hit:
		case e.Not:
			k.not = e.Text
		case !k.matched:
			k.matched = true
			k.why = "repos " + e.Text + " matches " + strings.Join(repo.Remotes, ", ")
		}
	}
	// notSet is the set entry that a ! repo-sets entry matched with.
	var notSet string
	for _, s := range sets {
		in, via := inSet(s.set, repo)
		switch {
		case !in:
		case s.entry.Not:
			if k.not == "" {
				k.not, notSet = s.entry.Text, via
			}
		case !k.matched:
			k.matched = true
			k.why = fmt.Sprintf("repo-sets %s matches %s (set entry %s)", s.entry.Text, strings.Join(repo.Remotes, ", "), via)
		}
	}
	if !k.matched && k.not == "" && subNot != "" {
		k.not = subNot
	}
	switch {
	case notSet != "":
		k.why = fmt.Sprintf("repo-sets %s leaves out %s (set entry %s)", k.not, strings.Join(repo.Remotes, ", "), notSet)
	case k.not != "":
		k.why = fmt.Sprintf("repos %s leaves out %s (folder %s)", k.not, strings.Join(repo.Remotes, ", "), repo.Subpath)
	case k.matched:
	case repo == nil && k.positive:
		k.why = "repos: not in a git repository"
	case repo != nil && k.positive:
		k.why = fmt.Sprintf("no %s entry matches %s (folder %s)", repoKeys(l), strings.Join(repo.Remotes, ", "), repo.Subpath)
	default:
		k.why = repoKeys(l) + ": only ! entries, none matches"
	}
	return k, ""
}

// repoKeys names the keys of l's repository check.
func repoKeys(l *config.Location) string {
	switch {
	case len(l.RepoSets) == 0:
		return "repos"
	case len(l.Repos) == 0:
		return "repo-sets"
	}
	return "repos or repo-sets"
}

// repoSet is a repo-sets entry with the set it names.
type repoSet struct {
	entry config.Entry
	set   *config.RepoSet
}

// repoSets are the sets that the repo-sets entries of b name and that can
// be read. A set that can't be read matches nothing in a positive entry,
// and in a ! entry leaves out the binding: left then says why. Each is an
// error, except a set of a source not on this machine; a set of an
// inactive source in a positive entry is the source's own error.
func (r *session) repoSets(b *config.Binding, t *Trace) (sets []repoSet, left string) {
	for _, e := range b.Location.RepoSets {
		if e.Err != nil {
			continue
		}
		var s *config.RepoSet
		var err error
		if e.InSource {
			err = r.countManifest(e.Source)
		}
		if err == nil {
			s, err = r.env.Catalog.RepoSet(e)
		}
		if err == nil {
			sets = append(sets, repoSet{e, s})
			continue
		}
		var u *catalog.Unavailable
		if errors.As(err, &u) {
			if !slices.Contains(t.Unavailable, u.Source.Name) {
				t.Unavailable = append(t.Unavailable, u.Source.Name)
			}
		}
		unreadable := func() {
			r.res.UnreadableSets = append(r.res.UnreadableSets, UnreadableSet{Binding: b.Name, Entry: e.Text, Why: err.Error()})
		}
		switch {
		case !e.Not && u != nil:
		case !e.Not:
			r.fail(t, b, e.Pos, yamlx.BrokenLocation, "repo-sets %s can't be read: %v; the entry matches nothing", e.Text, err)
			unreadable()
		case left != "":
		case u != nil && u.Source.State == catalog.NotHere:
			left = fmt.Sprintf("repo-sets %s can't be read: %v", e.Text, err)
		default:
			r.fail(t, b, e.Pos, yamlx.BrokenExclusion, "repo-sets %s can't be read: %v", e.Text, err)
			unreadable()
			left = fmt.Sprintf("repo-sets %s can't be read", e.Text)
		}
	}
	return sets, left
}

// inSet reports whether the session's repository is in set s: one of its
// positive entries matches and none of its ! entries does. via is the
// entry that matched.
func inSet(s *config.RepoSet, repo *gitrepo.Repo) (in bool, via string) {
	for _, e := range s.Entries {
		if e.Err == nil && e.Not {
			if hit, _ := repoMatch(e, repo); hit {
				return false, ""
			}
		}
	}
	for _, e := range s.Entries {
		if e.Err == nil && !e.Not {
			if hit, _ := repoMatch(e, repo); hit {
				return true, e.Text
			}
		}
	}
	return false, ""
}

// repoMatch reports whether e matches the session's repository, through
// any of its remotes: the repository, and with subdirs the part of it the
// session starts in. not is the subdirs ! entry that left out that part.
func repoMatch(e config.RepoEntry, repo *gitrepo.Repo) (hit bool, not string) {
	if repo == nil {
		return false, ""
	}
	pat := config.RepoID(e.Repo)
	if !slices.ContainsFunc(repo.Remotes, func(remote string) bool { return globMatch(pat, remote) }) {
		return false, ""
	}
	if e.Subdirs == nil {
		return true, ""
	}
	positive, matched := false, false
	for _, s := range e.Subdirs {
		in := s.Err == nil && globMatch(path.Clean(s.Value), repo.Subpath)
		switch {
		case s.Not && in:
			return false, s.Text
		case !s.Not:
			positive = true
			matched = matched || in
		}
	}
	return !positive || matched, ""
}

// add reads what binding b adds: its collections, then its instructions,
// skills and servers, each in list order, minus what its ! entries remove.
func (r *session) add(b *config.Binding, t *Trace) {
	x := r.collections(b, t)
	for _, it := range r.items(b, t, b.Instructions, catalog.Files, x.Files) {
		if content, real, ok := r.read(b, t, it, r.files); ok {
			r.res.Files = append(r.res.Files, r.hit(b, it, real, content))
			t.Files = append(t.Files, real)
		}
	}
	for _, it := range r.items(b, t, b.Skills, catalog.Skills, x.Skills) {
		md := it.Child("SKILL.md")
		content, real, ok := r.read(b, t, md, r.skills)
		if !ok {
			continue
		}
		h := r.hit(b, md, real, content)
		name, desc, body, err := parseSkill(string(content))
		if err != nil {
			r.fail(t, b, yamlx.Pos{}, yamlx.UnreadableFile, "%s: %v", md.Ref(), err)
			continue
		}
		if name == "" {
			name = filepath.Base(it.Path)
		}
		h.Name, h.Description, h.Content = name, desc, body
		if i := slices.IndexFunc(r.res.Skills, func(o Hit) bool { return o.Name == name }); i >= 0 {
			r.res.SameSkills = append(r.res.SameSkills, SameSkill{Name: name, First: r.res.Skills[i], Other: h})
		}
		r.res.Skills = append(r.res.Skills, h)
		t.Skills = append(t.Skills, h.Path)
	}
	r.addServers(b, t, x.Servers)
}

// collections is what b's collections add, with their errors in t: those
// of b's entries, and those in the collections, once a session.
func (r *session) collections(b *config.Binding, t *Trace) *catalog.Expansion {
	if len(b.Collections) == 0 {
		return &catalog.Expansion{}
	}
	x, errs := r.env.Catalog.Expand(b.Collections)
	for _, e := range errs {
		r.entryErrors(b, t, e.Entry, []error{e.Err})
	}
	for _, name := range x.Manifests {
		if err := r.countManifest(name); err != nil {
			r.fail(t, b, yamlx.Pos{}, yamlx.BrokenEntry, "%v", err)
			x = x.Without(name)
		}
	}
	for _, name := range x.Unavailable {
		if !slices.Contains(t.Unavailable, name) {
			t.Unavailable = append(t.Unavailable, name)
		}
	}
	if r.reported == nil {
		r.reported = map[*catalog.Error]bool{}
	}
	for _, e := range x.Errors {
		if !r.reported[e] {
			r.reported[e] = true
			t.Errors = append(t.Errors, e)
		}
	}
	return x
}

// items are what the collections added of a kind, from, then what the
// positive path entries of list add, minus what its ! entries match, each
// once.
func (r *session) items(b *config.Binding, t *Trace, list []config.Entry, kind catalog.Kind, from []catalog.Item) []catalog.Item {
	items := slices.Clone(from)
	for _, e := range list {
		if e.Err != nil || e.Not {
			continue
		}
		found, errs := r.env.Catalog.Paths(e, kind)
		r.entryErrors(b, t, e, errs)
		for _, it := range found {
			if !slices.ContainsFunc(items, func(o catalog.Item) bool { return o.Path == it.Path }) {
				items = append(items, it)
			}
		}
	}
	cat := r.env.Catalog
	for _, e := range list {
		if e.Not && e.Err == nil && !slices.ContainsFunc(items, func(it catalog.Item) bool { return cat.Excludes(e, it) }) {
			r.fail(t, b, e.Pos, yamlx.BrokenEntry, "%s excludes nothing", e.Text)
		}
	}
	return slices.DeleteFunc(items, func(it catalog.Item) bool {
		for _, e := range list {
			if e.Not && e.Err == nil && cat.Excludes(e, it) {
				t.Excluded = append(t.Excluded, Exclusion{Entry: e.Text, Item: it.Ref()})
				return true
			}
		}
		return false
	})
}

// entryErrors records what errs say about entry e.
func (r *session) entryErrors(b *config.Binding, t *Trace, e config.Entry, errs []error) {
	for _, err := range errs {
		var u *catalog.Unavailable
		switch {
		case errors.As(err, &u):
			if !slices.Contains(t.Unavailable, u.Source.Name) {
				t.Unavailable = append(t.Unavailable, u.Source.Name)
			}
		case catalog.EntryMistake(err) != yamlx.BrokenEntry:
			r.fail(t, b, e.Pos, catalog.EntryMistake(err), "%s: %v", e.Text, err)
		default:
			r.fail(t, b, e.Pos, yamlx.BrokenEntry, "%v", err)
		}
	}
}

// read is the content of it and its real path, unless seen has it
// already; it is then in seen.
func (r *session) read(b *config.Binding, t *Trace, it catalog.Item, seen map[string]bool) ([]byte, string, bool) {
	content, real, err := r.env.Catalog.Read(it, r.rd)
	switch {
	case catalog.IsUntrusted(err):
		r.fail(t, b, yamlx.Pos{}, yamlx.UntrustedFile, "%v", err)
		return nil, "", false
	case err != nil:
		r.fail(t, b, yamlx.Pos{}, yamlx.UnreadableFile, "%s can't be read: %v", it.Ref(), err)
		return nil, "", false
	}
	if seen[real] {
		return nil, "", false
	}
	seen[real] = true
	return content, real, true
}

func (r *session) hit(b *config.Binding, it catalog.Item, real string, content []byte) Hit {
	h := Hit{Path: real, Content: string(content), Origin: b.Name, Global: b.Location == nil,
		Source: it.Source, SourcePath: it.Rel}
	if it.Source != "" {
		h.Ref = it.Ref()
	}
	return h
}

// addServers adds b's MCP servers. A server is served once; of two servers
// with one name, the first in config order is served, and the other is
// recorded as a conflict.
func (r *session) addServers(b *config.Binding, t *Trace, from []catalog.Server) {
	found := slices.Clone(from)
	for _, e := range b.MCPServers {
		if e.Err != nil || e.Not {
			continue
		}
		if err := r.countManifests(e); err != nil {
			r.entryErrors(b, t, e, []error{err})
			continue
		}
		s, errs := r.env.Catalog.Servers(e)
		r.entryErrors(b, t, e, errs)
		found = append(found, s...)
	}
	for _, e := range b.MCPServers {
		if e.Not && e.Err == nil && !r.unavailable(t, e) &&
			!slices.ContainsFunc(found, func(s catalog.Server) bool { return catalog.ServerExcluded(e, s) }) {
			r.fail(t, b, e.Pos, yamlx.BrokenEntry, "%s excludes nothing", e.Text)
		}
	}
	for _, s := range found {
		if r.serverExcluded(b, t, s) {
			continue
		}
		t.MCPServers = append(t.MCPServers, s.ID)
		if r.servers[s.ID] {
			continue
		}
		r.servers[s.ID] = true
		if i := slices.IndexFunc(r.res.MCPServers, func(m MCPServer) bool { return m.Name == s.Name }); i >= 0 {
			r.res.MCPConflicts = append(r.res.MCPConflicts, MCPConflict{Winner: r.res.MCPServers[i].ID, Loser: s.ID, Prefix: s.Name, Origin: b.Name})
			continue
		}
		m := MCPServer{Server: s.Server, Origin: b.Name}
		if s.Pinned {
			if m.Unapproved = creds.Open().Unapproved(s.Server); m.Unapproved != "" {
				r.fail(t, b, yamlx.Pos{}, yamlx.NotApproved, "MCP server %s: %s; run `ctxtra mcp approve %s`", s.ID, m.Unapproved, s.ID)
			}
		}
		r.res.MCPServers = append(r.res.MCPServers, m)
	}
}

// countManifests reads the manifests that entry e needs and counts them
// against the session's total read limit.
func (r *session) countManifests(e config.Entry) error {
	for _, name := range r.env.Catalog.ManifestSources(e) {
		if err := r.countManifest(name); err != nil {
			return err
		}
	}
	return nil
}

// countManifest reads the manifest of source name, if it is active, and
// counts it against the session's total read limit.
func (r *session) countManifest(name string) error {
	r.manifests[name] = true
	s := r.env.Catalog.Source(name)
	if s == nil || s.State != catalog.Active {
		return nil
	}
	if _, _, err := r.env.Catalog.ManifestWithin(s, r.rd); err != nil {
		return fmt.Errorf("%s:%s can't be read: %w", name, manifest.FileName, err)
	}
	return nil
}

// unavailable reports whether server entry e names a source whose
// content this session can't use, itself or through the config's new name
// for one of its servers: what it would match is unknown.
func (r *session) unavailable(t *Trace, e config.Entry) bool {
	return slices.ContainsFunc(r.env.Catalog.ManifestSources(e), func(name string) bool {
		return slices.Contains(t.Unavailable, name)
	})
}

// serverExcluded reports whether a ! entry of b matches one of s's names.
func (r *session) serverExcluded(b *config.Binding, t *Trace, s catalog.Server) bool {
	for _, e := range b.MCPServers {
		if e.Not && e.Err == nil && catalog.ServerExcluded(e, s) {
			t.Excluded = append(t.Excluded, Exclusion{Entry: e.Text, Item: s.Refs[0]})
			return true
		}
	}
	return false
}
