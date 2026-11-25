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
	"github.com/ctxtra/ctxtra/internal/cfg"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/gitrepo"
	"github.com/ctxtra/ctxtra/internal/paths"
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
		rd:    fsutil.NewReader(config.MaxFileBytes, config.MaxSessionBytes),
		files: map[string]bool{}, skills: map[string]bool{}, servers: map[string]bool{},
		manifests: map[string]bool{}}
	r.res = &Result{Dir: dir, Summary: summary(env.Config.SessionMessage)}
	r.res.Errors = append(r.res.Errors, env.Errors...)
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
	if m := env.Config.MaxCtxChars; m != nil && *m > 0 {
		r.res.MaxChars = *m
		r.res.Fit(func(res *Result) bool { return Chars(res.Render()) <= res.MaxChars }, "max-ctx-chars")
	}
	return r.res
}

// summary is the config's session-message settings as SummaryMessage. 0
// leaves a list out.
func summary(m cfg.SessionMessage) config.SummaryMessage {
	s := config.DefaultSummaryMessage
	if m.MaxInstructions != nil {
		s.MaxInstructions, s.ShowInstructions = *m.MaxInstructions, *m.MaxInstructions > 0
	}
	if m.MaxSkills != nil {
		s.MaxSkills, s.ShowSkills = *m.MaxSkills, *m.MaxSkills > 0
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
func (r *session) fail(t *Trace, b *cfg.Binding, pos yamlx.Pos, m yamlx.Mistake, format string, args ...any) {
	t.Errors = append(t.Errors, &catalog.Error{File: r.env.Path, Err: &yamlx.Error{
		Pos: pos, Mistake: m, Part: yamlx.Part{Kind: "binding", Name: b.Name}, Msg: fmt.Sprintf(format, args...)}})
}

// conditions checks each condition that b has, the cheap ones first.
func (r *session) conditions(b *cfg.Binding, t *Trace) []Condition {
	var out []Condition
	if b.Agents != nil {
		out = append(out, agents(b.Agents, r.in.Agent))
	}
	if b.Profiles != nil {
		out = append(out, profiles(b.Profiles, r.in.Profiles))
	}
	if b.Location != nil {
		out = append(out, r.location(b, t))
	}
	return out
}

// split are the values of a condition's positive and ! entries.
func split(list []cfg.Entry) (pos, not []cfg.Entry) {
	for _, e := range list {
		if e.Not {
			not = append(not, e)
		} else {
			pos = append(pos, e)
		}
	}
	return pos, not
}

func values(list []cfg.Entry) string {
	var out []string
	for _, e := range list {
		out = append(out, e.Value)
	}
	return strings.Join(out, ", ")
}

// agents checks the agents condition: names compare without regard to
// case. Without an agent, no binding with agents applies.
func agents(list []cfg.Entry, agent string) Condition {
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
	if len(pos) > 0 && !slices.ContainsFunc(pos, func(e cfg.Entry) bool { return strings.EqualFold(e.Value, agent) }) {
		c.Why = fmt.Sprintf("for %s, not %s", values(pos), agent)
		return c
	}
	c.Held, c.Why = true, agent
	return c
}

// profiles checks the profiles condition; case counts.
func profiles(list []cfg.Entry, active []string) Condition {
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
	if len(pos) > 0 && !slices.ContainsFunc(pos, func(e cfg.Entry) bool { return slices.Contains(active, e.Value) }) {
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
func (r *session) location(b *cfg.Binding, t *Trace) Condition {
	c := Condition{Key: "location"}
	folders, left := r.dirs(b, t)
	if left {
		c.Why = "a ! dirs entry can't be checked"
		return c
	}
	repos, left := r.repos(b, t)
	if left {
		c.Why = "a ! entry of the repository check can't be checked"
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
func (r *session) dirs(b *cfg.Binding, t *Trace) (k check, left bool) {
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
func firstNot(list []cfg.Entry) *cfg.Entry {
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

// repos is the repository check: repos and repo-sets together. left is
// true when a ! entry can't be checked, which leaves out the binding: a !
// repo-sets entry, or any ! entry when git can't tell the repository. Then
// positive entries match nothing.
func (r *session) repos(b *cfg.Binding, t *Trace) (k check, left bool) {
	l := b.Location
	if len(l.Repos) == 0 && len(l.RepoSets) == 0 {
		return k, false
	}
	k.present = true
	for _, e := range l.RepoSets {
		if e.Err != nil {
			k.positive = k.positive || !e.Not
			continue
		}
		_, err := r.env.Catalog.RepoSet(e)
		if e.Not {
			r.fail(t, b, e.Pos, yamlx.BrokenExclusion, "repo-sets %s can't be read: %v", e.Text, err)
			return k, true
		}
		k.positive = true
		r.fail(t, b, e.Pos, yamlx.BrokenLocation, "repo-sets %s can't be read: %v; the entry matches nothing", e.Text, err)
	}
	repo, err := r.repository()
	if err != nil {
		for _, e := range l.Repos {
			if e.Err != nil {
				k.positive = k.positive || !e.Not
				continue
			}
			if e.Not {
				r.fail(t, b, e.Pos, yamlx.BrokenExclusion, "repos %s can't be checked: %v", e.Text, err)
				return k, true
			}
			for _, sub := range e.Subdirs {
				if sub.Not && sub.Err == nil {
					r.fail(t, b, sub.Pos, yamlx.BrokenExclusion, "repos %s subdirs %s can't be checked: %v", e.Repo, sub.Text, err)
					return k, true
				}
			}
			k.positive = true
		}
		k.why = fmt.Sprintf("repos: the repository can't be told (%v), so no entry matches", err)
		return k, false
	}
	// subNot is a subdirs ! entry that kept an entry from matching.
	var subNot string
	for _, e := range l.Repos {
		if !e.Not {
			k.positive = true
		}
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
	if !k.matched && k.not == "" && subNot != "" {
		k.not = subNot
	}
	switch {
	case k.not != "":
		k.why = fmt.Sprintf("repos %s leaves out %s (folder %s)", k.not, strings.Join(repo.Remotes, ", "), repo.Subpath)
	case k.matched:
	case repo == nil && k.positive:
		k.why = "repos: not in a git repository"
	case repo != nil && k.positive:
		k.why = fmt.Sprintf("no repos entry matches %s (folder %s)", strings.Join(repo.Remotes, ", "), repo.Subpath)
	default:
		k.why = "repos: only ! entries, none matches"
	}
	return k, false
}

// repoMatch reports whether e matches the session's repository, through
// any of its remotes: the repository, and with subdirs the part of it the
// session starts in. not is the subdirs ! entry that left out that part.
func repoMatch(e cfg.RepoEntry, repo *gitrepo.Repo) (hit bool, not string) {
	if repo == nil {
		return false, ""
	}
	pat := cfg.RepoID(e.Repo)
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
func (r *session) add(b *cfg.Binding, t *Trace) {
	cat := r.env.Catalog
	for _, e := range b.Collections {
		if e.Err != nil {
			continue
		}
		if _, err := cat.Collections(e); err != nil {
			r.fail(t, b, e.Pos, yamlx.BrokenEntry, "collections %s: %v", e.Text, err)
		}
	}
	for _, it := range r.items(b, t, b.Instructions, catalog.Files) {
		if content, real, ok := r.read(b, t, it, r.files); ok {
			r.res.Files = append(r.res.Files, r.hit(b, it, real, content))
			t.Files = append(t.Files, it.Path)
		}
	}
	for _, it := range r.items(b, t, b.Skills, catalog.Skills) {
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
	r.addServers(b, t)
}

// items are what the positive path entries of list add, minus what its !
// entries match, each once.
func (r *session) items(b *cfg.Binding, t *Trace, list []cfg.Entry, kind catalog.Kind) []catalog.Item {
	var items []catalog.Item
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
	return slices.DeleteFunc(items, func(it catalog.Item) bool {
		for _, e := range list {
			if e.Not && e.Err == nil && r.excludes(e, it) {
				t.Excluded = append(t.Excluded, Exclusion{Entry: e.Text, Item: it.Ref()})
				return true
			}
		}
		return false
	})
}

// entryErrors records what errs say about entry e.
func (r *session) entryErrors(b *cfg.Binding, t *Trace, e cfg.Entry, errs []error) {
	for _, err := range errs {
		var u *catalog.Unavailable
		switch {
		case errors.As(err, &u):
			if !slices.Contains(t.Unavailable, u.Source.Name) {
				t.Unavailable = append(t.Unavailable, u.Source.Name)
			}
		case catalog.IsUntrusted(err):
			r.fail(t, b, e.Pos, yamlx.UntrustedFile, "%s: %v", e.Text, err)
		case errors.As(err, new(*catalog.Unreadable)):
			r.fail(t, b, e.Pos, yamlx.UnreadableFile, "%s: %v", e.Text, err)
		default:
			r.fail(t, b, e.Pos, yamlx.BrokenEntry, "%v", err)
		}
	}
}

// excludes reports whether ! entry e matches it: a SOURCE:PATH pattern by
// its path in that source, a path on disk by where it is.
func (r *session) excludes(e cfg.Entry, it catalog.Item) bool {
	if e.InSource {
		return it.Source == e.Source && globMatch(path.Clean(e.Value), it.Rel)
	}
	return globMatch(r.env.Catalog.DiskPattern(e.Value), filepath.ToSlash(it.Path))
}

// read is the content of it and its real path, unless seen has it
// already; it is then in seen.
func (r *session) read(b *cfg.Binding, t *Trace, it catalog.Item, seen map[string]bool) ([]byte, string, bool) {
	real := it.Real()
	if seen[real] {
		return nil, "", false
	}
	content, err := r.env.Catalog.Read(it, r.rd)
	switch {
	case catalog.IsUntrusted(err):
		r.fail(t, b, yamlx.Pos{}, yamlx.UntrustedFile, "%v", err)
		return nil, "", false
	case err != nil:
		r.fail(t, b, yamlx.Pos{}, yamlx.UnreadableFile, "%s can't be read: %v", it.Ref(), err)
		return nil, "", false
	}
	seen[real] = true
	return content, real, true
}

func (r *session) hit(b *cfg.Binding, it catalog.Item, real string, content []byte) Hit {
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
func (r *session) addServers(b *cfg.Binding, t *Trace) {
	var found []catalog.Server
	for _, e := range b.MCPServers {
		if e.Err != nil || e.Not {
			continue
		}
		for _, name := range r.env.Catalog.ManifestSources(e) {
			r.manifests[name] = true
		}
		s, errs := r.env.Catalog.Servers(e)
		r.entryErrors(b, t, e, errs)
		found = append(found, s...)
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
		r.res.MCPServers = append(r.res.MCPServers, MCPServer{s.Server, b.Name})
	}
}

// serverExcluded reports whether a ! entry of b matches one of s's names.
func (r *session) serverExcluded(b *cfg.Binding, t *Trace, s catalog.Server) bool {
	for _, e := range b.MCPServers {
		if !e.Not || e.Err != nil {
			continue
		}
		pat := e.Value
		if e.InSource {
			pat = e.Source + ":" + e.Value
		}
		if slices.ContainsFunc(s.Refs, func(ref string) bool { return globMatch(pat, ref) }) {
			t.Excluded = append(t.Excluded, Exclusion{Entry: e.Text, Item: s.Refs[0]})
			return true
		}
	}
	return false
}
