package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/chunk"
	"github.com/ctxtra/ctxtra/internal/hook"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/setup"
	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/term"
	"github.com/ctxtra/ctxtra/internal/update"
)

// report implements the commands that show what a session gets.
type report struct {
	IO
	version string
	// agent is the session's agent, "" for none: then bindings with
	// agents don't apply.
	agent string
}

// checkAgent fails for an agent name that no agent has.
func (r report) checkAgent() error {
	if r.agent == "" {
		return nil
	}
	if _, ok := registry.Get(r.agent); !ok {
		return fmt.Errorf("unknown agent %q (one of: %s)", r.agent, strings.Join(registry.Names(), ", "))
	}
	return nil
}

// resolve is what a session of r.agent in dir gets, with the release
// notice that sessions show.
func (r report) resolve(dir string) (*resolve.Result, *resolve.Env) {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	env := loadEnv()
	res := env.Resolve(resolve.Input{Dir: dir, Agent: r.agent})
	// A session of a known agent gets what fits in it.
	if a, ok := registry.Get(r.agent); ok {
		a.Delivery().Fit(res)
	}
	if n := update.ReleaseNotice(update.FromConfig(env.Config), state.Load(), r.version); n != "" {
		res.Notices = append([]string{n}, res.Notices...)
	}
	return res, env
}

// status is the exit code of a command that shows res: 1 when a session
// wouldn't get all it should.
func status(res *resolve.Result) int {
	if len(res.Errors) > 0 || len(res.Dropped) > 0 {
		return 1
	}
	return 0
}

// errorTexts are errs as reported.
func errorTexts(errs []*catalog.Error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}

// sessionDiagnostics are res's errors, drops and notices, for --json.
func sessionDiagnostics(res *resolve.Result) []diagnostic {
	d := diagnostics("error", errorTexts(res.Errors))
	for _, h := range res.Dropped {
		severity := "error"
		if h.Over == "max-ctx-chars" {
			severity = "warning"
		}
		d = append(d, diagnostic{severity, fmt.Sprintf("dropped (over %s): %s", h.Over, h.Path)})
	}
	return append(d, diagnostics("notice", oneLine(res.Notices))...)
}

// hitInfo is a file or skill a session gets, in --json output.
type hitInfo struct {
	Path    string `json:"path"`
	Ref     string `json:"ref,omitempty"`  // SOURCE:PATH of a file in a source
	Name    string `json:"name,omitempty"` // skills
	Binding string `json:"binding"`
	Chars   int    `json:"chars,omitempty"` // files
}

func hits(hs []resolve.Hit, skills bool) []hitInfo {
	out := []hitInfo{}
	for _, h := range hs {
		i := hitInfo{Path: h.Path, Ref: h.Ref, Binding: h.Origin}
		if skills {
			i.Name = h.Name
		} else {
			i.Chars = resolve.Chars(h.Content)
		}
		out = append(out, i)
	}
	return out
}

// printIssues prints res's errors, drops and notices to w.
func printIssues(w interface{ Write([]byte) (int, error) }, res *resolve.Result) {
	for _, e := range res.Errors {
		fmt.Fprintf(w, "error: %s\n", e)
	}
	for _, h := range res.Dropped {
		fmt.Fprintf(w, "dropped (over %s): %s\n", h.Over, h.Path)
	}
	for _, n := range oneLine(res.Notices) {
		fmt.Fprintf(w, "notice: %s\n", n)
	}
}

func (r report) files(dir string, asJSON bool) int {
	if err := r.checkAgent(); err != nil {
		fmt.Fprintf(r.Err, "ctxtra context files: %v\n", err)
		return 2
	}
	res, _ := r.resolve(dir)
	if asJSON {
		writeJSON(r.Out, map[string]any{"dir": res.Dir, "files": hits(res.Files, false), "skills": hits(res.Skills, true)},
			sessionDiagnostics(res))
		return status(res)
	}
	for _, h := range res.Files {
		fmt.Fprintln(r.Out, h.Path)
	}
	for _, h := range res.Skills {
		fmt.Fprintln(r.Out, h.Path)
	}
	printIssues(r.Err, res)
	return status(res)
}

// showPart is the most bytes that context show --part prints. Agents cut a
// long command output: Codex shows the model about 10,000 bytes of it.
const showPart = 8000

// showTitle is the most bytes of a block's title in context show --part.
const showTitle = 1000

// showFooter ends a part of context show --part that isn't the last one.
const showFooter = "\n[ctxtra: part %d of %d; for the next part, run the same command with --part %d]\n"

type showOptions struct {
	exclude []string // leave out what sessions in these folders get
	part    int      // show only this part of the text, from 1; 0 for all of it
}

func (r report) show(dir string, o showOptions) int {
	if err := r.checkAgent(); err != nil {
		fmt.Fprintf(r.Err, "ctxtra context show: %v\n", err)
		return 2
	}
	res, _ := r.resolve(dir)
	for _, e := range res.Errors {
		fmt.Fprintf(r.Err, "error: %s\n", e)
	}
	for _, h := range res.Dropped {
		fmt.Fprintf(r.Err, "dropped (over %s): %s\n", h.Over, h.Path)
	}
	empty := "(nothing is added here)"
	if len(o.exclude) > 0 {
		// A file the session has from another folder is listed, without its
		// text: its header there doesn't say that it applies here too.
		for _, s := range o.exclude {
			base, _ := r.resolve(s)
			// Only the files that every agent's session gets in full: the
			// command doesn't know which agent runs it.
			base.Fit(func(c *resolve.Result) bool {
				return !slices.ContainsFunc(registry.All(), func(a agent.Agent) bool { return !a.Delivery().Fits(c) })
			}, "")
			res.Files = slices.DeleteFunc(res.Files, func(h resolve.Hit) bool {
				i := slices.IndexFunc(base.Files, func(b resolve.Hit) bool { return b.Path == h.Path })
				if i < 0 {
					return false
				}
				if !h.Global || !base.Files[i].Global {
					res.Known = append(res.Known, h)
				}
				return true
			})
			res.Skills = slices.DeleteFunc(res.Skills, func(h resolve.Hit) bool {
				return slices.ContainsFunc(base.Skills, func(b resolve.Hit) bool { return b.Path == h.Path })
			})
		}
		empty = "(nothing is added here beyond what sessions in the --exclude-session folders get)"
	}
	if o.part == 0 {
		if text := res.Render(); text != "" {
			fmt.Fprintln(r.Out, text)
		} else {
			fmt.Fprintln(r.Out, empty)
		}
		return status(res)
	}
	// Measured as printed: the output escapes control characters. Room is
	// kept for the line end and the footer.
	// A title repeats in the header of each continued part: a long one, such
	// as a path of escapes, is cut so that the parts keep room for the text.
	blocks := res.Blocks()
	for i, b := range blocks {
		blocks[i] = chunk.Block{Title: cut(term.Sanitize(b.Title), showTitle), Text: term.Sanitize(b.Text)}
	}
	room := 1 + len(fmt.Sprintf(showFooter, 99999, 99999, 99999))
	parts := chunk.Split(blocks, showPart-room, chunk.Bytes)
	switch {
	case len(parts) == 0 && o.part == 1:
		fmt.Fprintln(r.Out, empty)
		return status(res)
	case o.part < 1 || o.part > len(parts):
		fmt.Fprintf(r.Err, "ctxtra context show: --part %d: the text has %d parts\n", o.part, len(parts))
		return 2
	}
	fmt.Fprintln(r.Out, parts[o.part-1])
	if o.part < len(parts) {
		fmt.Fprintf(r.Out, showFooter, o.part, len(parts), o.part+1)
	}
	return status(res)
}

// cut is s cut to at most n bytes, at the start of a character, with "..."
// at the end when it is cut.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	n -= len("...")
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "..."
}

// sessionInfo is the session folder in --json output.
type sessionInfo struct {
	Dir        string   `json:"dir"`
	Agent      string   `json:"agent,omitempty"`
	Repository string   `json:"repository,omitempty"`
	Remotes    []string `json:"remotes,omitempty"`
	Subpath    string   `json:"subpath,omitempty"`
}

func (r report) sessionInfo(res *resolve.Result) sessionInfo {
	s := sessionInfo{Dir: res.Dir, Agent: r.agent}
	if res.Repo != nil {
		s.Repository, s.Remotes, s.Subpath = res.Repo.Root, res.Repo.Remotes, res.Repo.Subpath
	}
	return s
}

func (r report) session(res *resolve.Result) {
	fmt.Fprintf(r.Out, "session folder: %s\n", res.Dir)
	if r.agent != "" {
		fmt.Fprintf(r.Out, "  agent: %s\n", r.agent)
	} else {
		fmt.Fprintln(r.Out, "  agent: none given (--agent): bindings with agents don't apply")
	}
	switch {
	case res.RepoErr != nil:
		fmt.Fprintf(r.Out, "  repository: can't be told: %v\n", res.RepoErr)
	case res.Repo != nil:
		remotes := strings.Join(res.Repo.Remotes, ", ")
		if remotes == "" {
			remotes = "(none)"
		}
		fmt.Fprintf(r.Out, "  repository: %s\n  remotes: %s\n  subpath: %s\n", res.Repo.Root, remotes, res.Repo.Subpath)
	}
}

// traceItem is a file, skill or server of a binding in explain --json.
type traceItem struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added, dropped
	Chars  int    `json:"chars,omitempty"`
	Name   string `json:"name,omitempty"` // skills
	Over   string `json:"over,omitempty"` // dropped files: the limit
}

// conditionInfo is a condition of a binding in explain --json.
type conditionInfo struct {
	Key  string `json:"key"`
	Held bool   `json:"held"`
	Why  string `json:"why"`
	Not  string `json:"excluded_by,omitempty"`
}

// exclusionInfo is what a ! entry removed, in explain --json.
type exclusionInfo struct {
	Entry string `json:"entry"`
	Item  string `json:"item"`
}

// traceInfo is a binding in explain --json.
type traceInfo struct {
	Binding     string          `json:"binding"`
	Matched     bool            `json:"matched"`
	Reason      string          `json:"reason"`
	Conditions  []conditionInfo `json:"conditions"`
	Files       []traceItem     `json:"files"`
	Skills      []traceItem     `json:"skills"`
	MCPServers  []traceItem     `json:"mcp_servers,omitempty"`
	Excluded    []exclusionInfo `json:"excluded,omitempty"`
	Unavailable []string        `json:"unavailable,omitempty"`
	Errors      []string        `json:"errors,omitempty"`
}

// agentDrop is an agent whose sessions don't get all of a text, in --json.
type agentDrop struct {
	Agent   string   `json:"agent"`
	Over    string   `json:"over"`
	Dropped []string `json:"dropped"`
}

// traces is how each binding of res added its files, skills and servers.
func traces(res *resolve.Result) []traceInfo {
	files := map[string]resolve.Hit{}
	for _, h := range res.Files {
		files[h.Path] = h
	}
	skills := map[string]resolve.Hit{}
	for _, h := range res.Skills {
		skills[h.Path] = h
	}
	dropped := map[string]resolve.Hit{}
	for _, h := range res.Dropped {
		dropped[h.Path] = h
	}
	out := []traceInfo{}
	for _, t := range res.Trace {
		ti := traceInfo{Binding: t.Origin, Matched: t.Matched, Reason: t.Reason, Conditions: []conditionInfo{},
			Files: []traceItem{}, Skills: []traceItem{}, Unavailable: t.Unavailable, Errors: errorTexts(t.Errors)}
		for _, c := range t.Conditions {
			ti.Conditions = append(ti.Conditions, conditionInfo{c.Key, c.Held, c.Why, c.Not})
		}
		for _, f := range t.Files {
			if h, ok := dropped[f]; ok {
				ti.Files = append(ti.Files, traceItem{Path: f, Status: "dropped", Over: h.Over})
			} else {
				ti.Files = append(ti.Files, traceItem{Path: f, Status: "added", Chars: resolve.Chars(files[f].Content)})
			}
		}
		for _, s := range t.Skills {
			ti.Skills = append(ti.Skills, traceItem{Path: s, Status: "added", Name: skills[s].Name})
		}
		for _, id := range t.MCPServers {
			i := slices.IndexFunc(res.MCPServers, func(m resolve.MCPServer) bool { return m.ID == id })
			switch {
			case i >= 0 && res.MCPServers[i].Origin == t.Origin:
				ti.MCPServers = append(ti.MCPServers, traceItem{Path: id, Status: "added"})
			case i >= 0:
				ti.MCPServers = append(ti.MCPServers, traceItem{Path: id, Status: "already added by an earlier binding"})
			default:
				ti.MCPServers = append(ti.MCPServers, traceItem{Path: id, Status: "same name as an earlier server; see the notices"})
			}
		}
		for _, x := range t.Excluded {
			ti.Excluded = append(ti.Excluded, exclusionInfo{x.Entry, x.Item})
		}
		out = append(out, ti)
	}
	return out
}

// overCapacity is the exit code of res's agent capacity: 1 when an agent
// wouldn't get all of it.
func (r report) overCapacity(res *resolve.Result) int {
	if len(r.agentDrops(res)) > 0 {
		return 1
	}
	return 0
}

// capacityErrors are the errors of the agents that wouldn't get all of res.
func (r report) capacityErrors(res *resolve.Result) []string {
	var out []string
	for _, d := range r.agentDrops(res) {
		out = append(out, fmt.Sprintf("over %s, %s sessions don't get %s", d.Over, d.Agent, agent.Names(d.Dropped)))
	}
	return out
}

// agentDrops are the agents that wouldn't get all of res, with the files
// their sessions don't get.
func (r report) agentDrops(res *resolve.Result) []agentDrop {
	out := []agentDrop{}
	for _, a := range registry.All() {
		if r.agent != "" && a.Name() != r.agent {
			continue
		}
		d := a.Delivery()
		c := *res
		c.Dropped = nil
		d.Fit(&c)
		if len(c.Dropped) == 0 {
			continue
		}
		name := c.Namer()
		names := make([]string, len(c.Dropped))
		for i, h := range c.Dropped {
			names[i] = name(h)
		}
		out = append(out, agentDrop{a.Name(), d.Over(), names})
	}
	return out
}

func (r report) explain(dir string, asJSON bool) int {
	if err := r.checkAgent(); err != nil {
		fmt.Fprintf(r.Err, "ctxtra context explain: %v\n", err)
		return 2
	}
	res, env := r.resolve(dir)
	total := resolve.Chars(res.Render())
	if asJSON {
		writeJSON(r.Out, map[string]any{
			"config": env.Path, "session": r.sessionInfo(res), "bindings": traces(res),
			"total":         map[string]int{"files": len(res.Files), "skills": len(res.Skills), "chars": total, "tokens": total / 4},
			"max_ctx_chars": res.MaxChars, "agents_over": r.agentDrops(res),
		}, slices.Concat(sessionDiagnostics(res), diagnostics("error", r.capacityErrors(res))))
		return max(status(res), r.overCapacity(res))
	}
	fmt.Fprintf(r.Out, "config: %s\n", env.Path)
	r.session(res)
	fmt.Fprintln(r.Out)
	if len(res.Trace) == 0 {
		fmt.Fprintln(r.Out, "no bindings")
	}
	for _, t := range traces(res) {
		mark := "-"
		if t.Matched {
			mark = "+"
		}
		fmt.Fprintf(r.Out, "%s %s: %s\n", mark, t.Binding, t.Reason)
		for _, c := range t.Conditions {
			if c.Not != "" {
				fmt.Fprintf(r.Out, "    %s: left out by %s\n", c.Key, c.Not)
			}
		}
		for _, f := range t.Files {
			if f.Status == "dropped" {
				fmt.Fprintf(r.Out, "    dropped  %s (over %s)\n", f.Path, f.Over)
			} else {
				fmt.Fprintf(r.Out, "    file     %s (%d chars)\n", f.Path, f.Chars)
			}
		}
		for _, s := range t.Skills {
			fmt.Fprintf(r.Out, "    skill    %s (%s)\n", s.Path, s.Name)
		}
		for _, m := range t.MCPServers {
			if m.Status == "added" {
				fmt.Fprintf(r.Out, "    mcp      %s\n", m.Path)
			} else {
				fmt.Fprintf(r.Out, "    skip     mcp %s (%s)\n", m.Path, m.Status)
			}
		}
		for _, x := range t.Excluded {
			fmt.Fprintf(r.Out, "    excluded %s (by %s)\n", x.Item, x.Entry)
		}
		for _, u := range t.Unavailable {
			fmt.Fprintf(r.Out, "    unavailable source %s: %s\n", u, env.Catalog.Source(u).Why)
		}
		for _, e := range t.Errors {
			fmt.Fprintf(r.Out, "    error    %s\n", e)
		}
	}
	if len(res.Errors) > 0 {
		fmt.Fprintln(r.Out, "\nerrors:")
		for _, e := range res.Errors {
			fmt.Fprintf(r.Out, "  %s\n", e)
		}
	}
	if len(res.Notices) > 0 {
		fmt.Fprintln(r.Out, "\nnotices:")
		for _, n := range oneLine(res.Notices) {
			fmt.Fprintf(r.Out, "  %s\n", n)
		}
	}
	fmt.Fprintf(r.Out, "\ntotal: %d files, %d skills, %d chars (about %d tokens)", len(res.Files), len(res.Skills), total, total/4)
	if n := len(res.MCPServers); n > 0 {
		fmt.Fprintf(r.Out, ", %d MCP servers", n)
	}
	if res.MaxChars > 0 {
		fmt.Fprintf(r.Out, ", max-ctx-chars %d", res.MaxChars)
	}
	fmt.Fprintln(r.Out)
	r.sizeWarnings(res, "")
	return max(status(res), r.overCapacity(res))
}

// sizeWarnings reports each agent that wouldn't get all of res: the files
// its sessions don't get. Over capacity is an error.
func (r report) sizeWarnings(res *resolve.Result, indent string) {
	for _, e := range r.capacityErrors(res) {
		fmt.Fprintf(r.Out, "%serror: %s\n", indent, e)
	}
}

// hookInfo is an agent's setup in doctor --json.
type hookInfo struct {
	Agent   string   `json:"agent"`
	State   string   `json:"state"`
	OK      bool     `json:"ok"`
	Config  string   `json:"config"`
	Details []string `json:"details"`
}

func (r report) doctor(dir string, asJSON bool) int {
	ok := true
	res, env := r.resolve(dir)
	others := errorTexts(env.OtherErrors(res))
	notes := mcpOrphans(env)
	servers, _ := env.Catalog.AllServers()
	for _, s := range servers {
		if s.Source == "" {
			notes = append(notes, secretNotes(s.ID, s.Server, "config")...)
		}
	}
	if st, err := os.Stat(filepath.Join(paths.Home(), ".ctxtra")); err == nil && st.IsDir() {
		notes = append(notes, "~/.ctxtra can be deleted once every agent is set up again with ctxtra setup")
	}
	_, statErr := os.Stat(env.Path)
	setups, _, _ := state.LoadSetups()
	bin := running()
	var agents []hookInfo
	mcp := setup.LoadMCP()
	for _, a := range registry.All() {
		known := setups.Known(a.Name(), a.ConfigPath())
		st := a.Status(bin, known)
		details := append([]string{}, st.Details...)
		if m, isMCP := a.(agent.MCPAgent); isMCP {
			host := m.MCP()
			ms := host.MCPStatus(bin, agent.MCPCheck{Known: setups.Known(setup.MCPKey(a.Name()), host.MCPPath()), Want: known, EnvPass: mcp.EnvPass})
			details = append(details, "MCP: "+ms.State+" ("+paths.Tilde(host.MCPPath())+")")
			details = append(details, ms.Details...)
			st.OK = st.OK && ms.OK
		}
		ok = ok && st.OK
		agents = append(agents, hookInfo{a.Name(), st.State, st.OK, a.ConfigPath(), details})
	}
	repo := ""
	if res.Repo != nil {
		repo = res.Repo.Root
	}
	runs := hook.LastRuns(res.Dir, repo)
	ok = ok && status(res) == 0 && r.overCapacity(res) == 0 && len(others) == 0
	checks := update.FromConfig(env.Config)
	if asJSON {
		sources := []sourceInfo{}
		for _, cs := range env.Catalog.Sources() {
			sources = append(sources, info(env, cs))
		}
		if runs == nil {
			runs = []hook.Entry{}
		}
		out := map[string]any{
			"folders": map[string]string{"config": paths.ConfigDir(), "data": paths.DataDir(), "state": paths.StateDir(), "cache": paths.CacheDir()},
			"ctxtra":  releaseInfo(checks, state.Load(), r.version),
			"config":  map[string]any{"file": env.Path, "exists": statErr == nil, "bindings": len(env.Config.Bindings)},
			"sources": sources, "session": r.sessionInfo(res),
			"files": hits(res.Files, false), "skills": hits(res.Skills, true), "agents_over": r.agentDrops(res),
			"hooks": agents, "last_hook_runs": runs,
		}
		if len(servers) > 0 {
			out["mcp"] = map[string]any{"key": keyLocation(), "servers": mcpInfos(servers)}
		}
		writeJSON(r.Out, out, slices.Concat(sessionDiagnostics(res), diagnostics("error", r.capacityErrors(res)), diagnostics("error", others), diagnostics("notice", notes)))
		if ok {
			return 0
		}
		return 1
	}
	fmt.Fprintf(r.Out, "ctxtra %s: %s\n", r.version, releaseStatus(checks, state.Load(), r.version))
	fmt.Fprintf(r.Out, "folders:\n  config: %s\n  data: %s\n  state: %s\n  cache: %s\n", paths.ConfigDir(), paths.DataDir(), paths.StateDir(), paths.CacheDir())
	fmt.Fprintf(r.Out, "config: %s\n", env.Path)
	if statErr != nil {
		fmt.Fprintln(r.Out, "  not found: nothing is added (the format: ctxtra help config)")
	}
	fmt.Fprintf(r.Out, "  bindings: %d\n", len(env.Config.Bindings))
	for _, cs := range env.Catalog.Sources() {
		fmt.Fprintf(r.Out, "  source %s: %s\n", cs.Name, describeSource(env, cs))
	}
	for _, n := range notes {
		fmt.Fprintf(r.Out, "  NOTE: %s\n", n)
	}
	for _, e := range slices.Concat(errorTexts(res.Errors), others) {
		fmt.Fprintf(r.Out, "  ERROR: %s\n", e)
	}
	fmt.Fprintln(r.Out)
	r.session(res)
	for _, h := range res.Files {
		fmt.Fprintf(r.Out, "  file: %s (%d chars, %s)\n", h.Path, resolve.Chars(h.Content), h.Origin)
	}
	for _, h := range res.Skills {
		fmt.Fprintf(r.Out, "  skill: %s (%s, %s)\n", h.Name, h.Path, h.Origin)
	}
	for _, h := range res.Dropped {
		fmt.Fprintf(r.Out, "  DROPPED (over %s): %s\n", h.Over, h.Path)
	}
	for _, n := range oneLine(res.Notices) {
		fmt.Fprintf(r.Out, "  NOTICE: %s\n", n)
	}
	r.sizeWarnings(res, "  ")

	if len(servers) > 0 {
		fmt.Fprintln(r.Out, "\nMCP servers:")
		fmt.Fprintf(r.Out, "  credentials: %s\n", keyNote())
		for _, s := range mcpInfos(servers) {
			fmt.Fprintf(r.Out, "  %s: %s, %s", s.ID, s.Kind, term.Sanitize(s.SignIn))
			if s.Source != "" {
				fmt.Fprintf(r.Out, ", %s", s.Approval)
			}
			fmt.Fprintln(r.Out)
		}
	}

	fmt.Fprintln(r.Out, "\nhooks:")
	for _, a := range agents {
		fmt.Fprintf(r.Out, "  %s: %s  (%s)\n", a.Agent, a.State, a.Config)
		for _, d := range a.Details {
			fmt.Fprintf(r.Out, "    %s\n", d)
		}
	}
	fmt.Fprintln(r.Out, "\nlast hook runs here:")
	if len(runs) == 0 {
		fmt.Fprintln(r.Out, "  none yet")
	}
	for _, e := range runs {
		status := fmt.Sprintf("files=%d skills=%d errors=%d", len(e.Files), len(e.Skills), len(e.Errors))
		if e.Error != "" {
			status = "error: " + e.Error
		}
		if e.Skipped != "" {
			status = "skipped: " + e.Skipped
		}
		fmt.Fprintf(r.Out, "  %s %s: %s %s\n", e.Harness, e.Event, e.Ts, status)
	}
	if ok {
		return 0
	}
	return 1
}

// oneLine is notices with their command on the same line, for reports.
func oneLine(notices []string) []string {
	out := make([]string, len(notices))
	for i, n := range notices {
		out[i] = strings.ReplaceAll(n, "\n", "; ")
	}
	return out
}
