package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/edit"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/risk"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// newBinding is what binding add writes: the flags' values as given.
type newBinding struct {
	Name                                             string
	Dirs, Repos, Subdirs, RepoSets, Profiles, Agents []string
	Collections, Instructions, Skills, MCPServers    []string
}

// node is the binding's entry in the config, without its name: location
// with dirs, repos and repo-sets, the conditions, then the content lists.
// Subdirs go to the one repository, which a caller has checked.
func (b newBinding) node() *yaml.Node {
	list := func(values []string) *yaml.Node {
		n := &yaml.Node{Kind: yaml.SequenceNode}
		for _, v := range values {
			n.Content = append(n.Content, yamlx.Text(v))
		}
		return n
	}
	add := func(m *yaml.Node, key string, values []string) {
		if len(values) > 0 {
			m.Content = append(m.Content, yamlx.Text(key), list(values))
		}
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	loc := &yaml.Node{Kind: yaml.MappingNode}
	add(loc, "dirs", b.Dirs)
	if len(b.Subdirs) > 0 && len(b.Repos) == 1 {
		entry := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{yamlx.Text("repo"), yamlx.Text(b.Repos[0])}}
		add(entry, "subdirs", b.Subdirs)
		loc.Content = append(loc.Content, yamlx.Text("repos"), &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{entry}})
	} else {
		add(loc, "repos", b.Repos)
	}
	add(loc, "repo-sets", b.RepoSets)
	if len(loc.Content) > 0 {
		n.Content = append(n.Content, yamlx.Text("location"), loc)
	}
	add(n, "profiles", b.Profiles)
	add(n, "agents", b.Agents)
	add(n, "collections", b.Collections)
	add(n, "instructions", b.Instructions)
	add(n, "skills", b.Skills)
	add(n, "mcp-servers", b.MCPServers)
	return n
}

// entryFlags reports whether b has any flag that writes an entry.
func (b newBinding) entryFlags() bool {
	return len(slices.Concat(b.Dirs, b.Repos, b.Subdirs, b.RepoSets, b.Profiles, b.Agents,
		b.Collections, b.Instructions, b.Skills, b.MCPServers)) > 0
}

// inSource makes the values that --source applies to names or paths in
// source name, after any !. A value that names a source already is an
// error.
func (b *newBinding) inSource(name string) error {
	for _, l := range []*[]string{&b.Collections, &b.RepoSets, &b.MCPServers, &b.Instructions, &b.Skills} {
		for i, v := range *l {
			rest, not := strings.CutPrefix(v, "!")
			if namesSource(rest) {
				return fmt.Errorf("%s names a source already; with --source, give only the name or path in the source", v)
			}
			v = name + ":" + rest
			if not {
				v = "!" + v
			}
			(*l)[i] = v
		}
	}
	return nil
}

// namesSource reports whether entry v reads as SOURCE:VALUE in the config.
func namesSource(v string) bool {
	return strings.Contains(v, ":") && !strings.HasPrefix(v, "./") && !strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "~")
}

// splitFolder splits the argument of --source into a repository and the
// folder after //, as in git@github.com:acme/ctx.git//teams/obs.
func splitFolder(arg string) (repo, folder string, ok bool) {
	start := 0
	if i := strings.Index(arg, "://"); i >= 0 {
		start = i + len("://")
	}
	if j := strings.Index(arg[start:], "//"); j >= 0 {
		return arg[:start+j], arg[start+j+2:], true
	}
	return arg, "", false
}

// findSource is the config's name for the source that the argument of
// --source names: a source name, or a repository address, compared as
// host/path, with //FOLDER for the source with that subdir.
func findSource(c *config.Config, arg string) (string, error) {
	if !strings.ContainsAny(arg, "/:") {
		if c.Source(arg) == nil {
			return "", fmt.Errorf("the config has no source %s (see ctxtra source list)", arg)
		}
		return arg, nil
	}
	repo, folder, hasFolder := splitFolder(arg)
	id := config.RepoID(repo)
	var names []string
	for _, src := range c.Sources {
		where := src.Repo
		if where == "" {
			where = src.Dir
		}
		if where == "" || config.RepoID(where) != id {
			continue
		}
		if hasFolder && config.CleanSubdir(src.Subdir) != config.CleanSubdir(folder) {
			continue
		}
		names = append(names, src.Name)
	}
	switch len(names) {
	case 0:
		add := "ctxtra source add --repo " + repo
		if hasFolder && config.CleanSubdir(folder) != "" {
			add += " --subdir " + config.CleanSubdir(folder)
		}
		return "", fmt.Errorf("no source has %s; add it first: %s", arg, add)
	case 1:
		return names[0], nil
	}
	return "", fmt.Errorf("the sources %s all have %s; give --source one of their names", strings.Join(names, ", "), arg)
}

// diskPath is a path argument as the config writes it: absolute, with ~
// for the home folder. Paths in the config are relative to its folder, so
// a path relative to the current folder is made absolute. SOURCE:PATH stays.
func diskPath(wd, p string) string {
	rest, not := strings.CutPrefix(p, "!")
	if source, _, ok := strings.Cut(rest, ":"); ok && !strings.HasPrefix(rest, "./") && !strings.HasPrefix(rest, "/") &&
		!strings.HasPrefix(rest, "~") && config.CheckName(source) == nil {
		return p
	}
	if rest == "" {
		return p
	}
	abs := paths.Tilde(paths.Resolve(wd, rest))
	if not {
		return "!" + abs
	}
	return abs
}

// addFlags are how binding add writes the binding.
type addFlags struct {
	source  string // --source
	file    string // --file, "-" for stdin
	replace bool
	at      config.Place
	dryRun  bool
}

// maxEntryBytes is the largest entry --file reads.
const maxEntryBytes = 1 << 20

// readEntry is the entry of --file: from file, or stdin for "-".
func readEntry(in io.Reader, file string) (*yaml.Node, error) {
	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(io.LimitReader(in, maxEntryBytes+1))
		if err == nil && len(data) > maxEntryBytes {
			err = fmt.Errorf("stdin has more than %d bytes", maxEntryBytes)
		}
		file = "stdin"
	} else {
		data, err = fsutil.ReadBounded(file, maxEntryBytes)
	}
	if err != nil {
		return nil, err
	}
	d, perr := yamlx.ParseEntry(data)
	if perr != nil {
		return nil, fmt.Errorf("%s: %v", file, perr)
	}
	return d.Root, nil
}

// bindingAdd is binding add. Only a binding that works is written: a
// session it applies to would get all it names.
func bindingAdd(stdio IO, b newBinding, o addFlags) error {
	if err := config.CheckName(b.Name); err != nil {
		return err
	}
	switch {
	case o.file != "" && (b.entryFlags() || o.source != ""):
		return errors.New("--file gives the whole binding; leave out the flags of its entries")
	case o.file == "" && len(slices.Concat(b.Collections, b.Instructions, b.Skills, b.MCPServers)) == 0:
		return errors.New("add at least one --collection, --instruction, --skill or --mcp-server, or give the binding with --file")
	case o.at.Before != "" && o.at.After != "":
		return errors.New("a binding goes either --before or --after another, not both")
	}
	s := newSources(stdio)
	if len(b.Subdirs) > 0 && len(b.Repos) != 1 {
		return exitCode(s.fail("binding add", errors.New("--subdir needs exactly one --repo, whose subdirs it writes; for other shapes, give the binding with --file; nothing changed")))
	}
	var entry *yaml.Node
	if o.file != "" {
		var err error
		if entry, err = readEntry(stdio.In, o.file); err != nil {
			return exitCode(s.fail("binding add", fmt.Errorf("%w; nothing changed", err)))
		}
	} else {
		// With --source, files and skills are paths in the source.
		lists := [][]string{b.Dirs}
		if o.source == "" {
			lists = append(lists, b.Instructions, b.Skills)
		}
		wd, _ := os.Getwd()
		for _, l := range lists {
			for i, p := range l {
				l[i] = diskPath(wd, p)
			}
		}
	}
	w, err := s.addBinding(b, entry, o)
	var place *config.PlaceError
	switch {
	case errors.As(err, &place):
		return fmt.Errorf("%v; leave out --before and --after", place)
	case err != nil:
		var exists *config.ExistsError
		if errors.As(err, &exists) {
			err = fmt.Errorf("%w; nothing changed (to change it in place, add --replace)", err)
		}
		return exitCode(s.fail("binding add", err))
	}
	s.showBinding(b.Name, w, o)
	return nil
}

// written is what addBinding wrote.
type written struct {
	changed, replaced bool
	// entry is the binding as in the config; before the config's text
	// before the change; env the changed config and res what a session
	// gets from the binding.
	entry  *yaml.Node
	before []byte
	env    *resolve.Env
	res    *resolve.Result
}

// addBinding writes binding b, or entry when it isn't nil, if a session it
// applies to would get all it names. With o.dryRun it only checks.
func (s *sources) addBinding(b newBinding, entry *yaml.Node, o addFlags) (written, error) {
	var w written
	change := func(ed *config.Editor) error {
		w = written{before: ed.Bytes()}
		n := entry
		if n == nil {
			if o.source != "" {
				name, err := findSource(config.Parse(ed.Bytes(), config.Options{Agents: registry.Names()}), o.source)
				if err != nil {
					return err
				}
				if err := b.inSource(name); err != nil {
					return err
				}
			}
			n = b.node()
		}
		var err error
		if o.replace {
			w.changed, w.replaced, err = ed.Put(config.Bindings, b.Name, n, o.at)
		} else {
			w.changed, err = ed.Add(config.Bindings, b.Name, n, o.at)
		}
		if err != nil || !w.changed {
			return err
		}
		if w.entry, err = ed.Entry(config.Bindings, b.Name); err != nil {
			return err
		}
		w.env, w.res, err = checkBinding(s.file, ed.Bytes(), b.Name)
		return err
	}
	if o.dryRun {
		// Reads only: it works where the config can't be written, for
		// example in an agent's sandbox.
		data, err := readConfig(s.file)
		if err != nil {
			return w, err
		}
		ed, err := config.NewEditor(data)
		if err != nil {
			return w, err
		}
		return w, change(ed)
	}
	return w, s.write(func(tx *edit.Tx) error { return change(tx.Config) })
}

// showBinding prints what binding add wrote: the binding as in the config,
// where it applies, and what a session there gets from it, with the content
// warnings of the files of repo sources that the config didn't use before.
func (s *sources) showBinding(name string, w written, o addFlags) {
	file := paths.Tilde(s.file)
	if !w.changed {
		fmt.Fprintf(s.Out, "the binding %s is in %s already\n", name, file)
		return
	}
	verb := map[[2]bool]string{
		{false, false}: "added binding %s to %s:\n", {true, false}: "would add binding %s to %s:\n",
		{false, true}: "replaced binding %s in %s:\n", {true, true}: "would replace binding %s in %s:\n",
	}[[2]bool{o.dryRun, w.replaced}]
	fmt.Fprintf(s.Out, verb, name, file)
	if text, err := yamlx.Encode(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{w.entry}}); err == nil {
		for l := range strings.Lines(string(text)) {
			fmt.Fprint(s.Out, "  "+l)
		}
	}
	b := w.env.Config.Binding(name)
	fmt.Fprintf(s.Out, "it applies in %s\n", appliesIn(b))
	if len(w.res.Files)+len(w.res.Skills)+len(w.res.MCPServers) > 0 {
		fmt.Fprintln(s.Out, "a session there gets:")
	}
	for _, h := range w.res.Files {
		fmt.Fprintf(s.Out, "  file        %s\n", hitName(h))
	}
	for _, h := range w.res.Skills {
		fmt.Fprintf(s.Out, "  skill       %s (%s)\n", h.Name, hitName(h))
	}
	for _, m := range w.res.MCPServers {
		fmt.Fprintf(s.Out, "  MCP server  %s\n", m.Name)
	}
	s.warnNewFiles(w)
	if o.dryRun {
		fmt.Fprintln(s.Out, "nothing changed (--dry-run)")
		return
	}
	fmt.Fprintln(s.Out, "new sessions get it; a running session keeps what it got at its start")
}

// hitName is a file or skill as a binding names it: SOURCE:PATH in a
// source, else its path.
func hitName(h resolve.Hit) string {
	if h.Ref != "" {
		return h.Ref
	}
	return paths.Tilde(h.Path)
}

// appliesIn says in which sessions binding b applies, such as "sessions of
// claude-code, where its location matches, with iam active".
func appliesIn(b *config.Binding) string {
	split := func(list []config.Entry) (pos, not []string) {
		for _, e := range list {
			if e.Not {
				not = append(not, e.Value)
			} else {
				pos = append(pos, e.Value)
			}
		}
		return pos, not
	}
	or := func(names []string) string { return strings.Join(names, " or ") }
	var parts []string
	switch pos, not := split(b.Agents); {
	case len(pos) > 0 && len(not) > 0:
		parts = append(parts, "of "+or(pos)+", but not of "+or(not))
	case len(pos) > 0:
		parts = append(parts, "of "+or(pos))
	case len(not) > 0:
		parts = append(parts, "of every agent but "+or(not))
	}
	where := "every session"
	if b.Location != nil {
		where = "sessions"
		parts = append(parts, "where its location matches")
	}
	switch pos, not := split(b.Profiles); {
	case len(pos) > 0 && len(not) > 0:
		parts = append(parts, fmt.Sprintf("with %s active and without %s", or(pos), or(not)))
	case len(pos) > 0:
		parts = append(parts, fmt.Sprintf("with %s active", or(pos)))
	case len(not) > 0:
		parts = append(parts, fmt.Sprintf("without %s active", or(not)))
	}
	if len(parts) == 0 {
		return where
	}
	return where + " " + strings.Join(parts, ", ")
}

// warnNewFiles prints the content warnings of the files and skills of repo
// sources that the binding adds and the config's bindings didn't use
// before: a source's first version is accepted without a review, and these
// files are new to the user's sessions.
func (s *sources) warnNewFiles(w written) {
	before := resolve.LoadData(s.file, w.before, registry.Names())
	uses := map[string]*used{}
	for _, h := range slices.Concat(w.res.Files, w.res.Skills) {
		src := before.Config.Source(h.Source)
		if h.Source == "" || src == nil || src.Repo == "" {
			continue
		}
		u := uses[h.Source]
		if u == nil {
			u = usedFrom(before, before.Catalog, h.Source)
			uses[h.Source] = u
		}
		if u.has(h.SourcePath) {
			continue
		}
		data, err := fsutil.ReadBounded(h.Path, source.MaxFileBytes)
		ref := h.Source + ":" + h.SourcePath
		if err != nil || !risk.Text(data) {
			fmt.Fprintf(s.Out, "note: %s: %s\n", source.Sanitize(ref), notCheckedWhy)
			continue
		}
		shown, n := s.contentCheck(h.Source).scan(h.SourcePath, data, nil)
		for _, f := range shown {
			fmt.Fprintf(s.Out, "warning: %s:%d: %s: %s\n", source.Sanitize(ref), f.Line, f.Check, source.Sanitize(f.Text))
		}
		if n > maxWarnings {
			fmt.Fprintf(s.Out, "note: %s: %d content warnings; the first %d are shown\n", source.Sanitize(ref), n, maxWarnings)
		}
	}
}

// checkBinding fails when the binding name of the config data has an error,
// or when a session it applies to wouldn't get all it names. It returns the
// config and what a session gets from the binding.
func checkBinding(file string, data []byte, name string) (*resolve.Env, *resolve.Result, error) {
	env := resolve.LoadData(file, data, registry.Names())
	b := env.Config.Binding(name)
	if b == nil {
		return nil, nil, fmt.Errorf("the binding %s isn't in the changed config; nothing changed", name)
	}
	var problems []string
	for _, e := range b.Errs {
		problems = append(problems, e.Error())
	}
	wd, _ := os.Getwd()
	res := env.Alone(b, resolve.Input{Dir: wd})
	var approve []string
	for _, e := range res.Errors {
		problems = append(problems, e.Err.Error())
	}
	for _, m := range res.MCPServers {
		if m.Unapproved != "" {
			approve = append(approve, "ctxtra mcp approve "+m.ID)
		}
	}
	for _, h := range res.Dropped {
		limit := h.Over
		if limit == "max-ctx-chars" {
			limit = fmt.Sprintf("%s (%d)", limit, res.MaxChars)
		}
		problems = append(problems, fmt.Sprintf("%s would be dropped (over %s)", paths.Tilde(h.Path), limit))
	}
	for _, t := range res.Trace {
		for _, u := range t.Unavailable {
			problems = append(problems, "source "+u+": "+env.Catalog.Source(u).Why)
		}
	}
	// The capacity of each agent the binding reaches.
	for _, a := range registry.All() {
		if !resolve.ForAgent(b.Agents, a.Name()) {
			continue
		}
		problems = append(problems, report{agent: a.Name()}.capacityErrors(res)...)
	}
	if len(problems) > 0 {
		msg := fmt.Sprintf("sessions wouldn't get the binding as asked; nothing changed:\n  %s", strings.Join(problems, "\n  "))
		if len(approve) > 0 {
			msg += "\napprove the servers first, then add the binding again:\n  " + strings.Join(approve, "\n  ")
		}
		return nil, nil, errors.New(msg)
	}
	return env, res, nil
}

// bindingInfo is a binding in the output of binding list: its entries as
// written.
type bindingInfo struct {
	Name         string   `json:"name"`
	Line         int      `json:"line"`
	Dirs         []string `json:"dirs,omitempty"`
	Repos        []string `json:"repos,omitempty"`
	RepoSets     []string `json:"repo_sets,omitempty"`
	Profiles     []string `json:"profiles,omitempty"`
	Agents       []string `json:"agents,omitempty"`
	Collections  []string `json:"collections"`
	Instructions []string `json:"instructions"`
	Skills       []string `json:"skills"`
	MCPServers   []string `json:"mcp_servers"`
}

func entryTexts(list []config.Entry) []string {
	out := []string{}
	for _, e := range list {
		out = append(out, e.Text)
	}
	return out
}

func bindingOf(b *config.Binding) bindingInfo {
	i := bindingInfo{Name: b.Name, Line: b.Pos.Line, Collections: entryTexts(b.Collections), Instructions: entryTexts(b.Instructions),
		Skills: entryTexts(b.Skills), MCPServers: entryTexts(b.MCPServers)}
	if b.Profiles != nil {
		i.Profiles = entryTexts(b.Profiles)
	}
	if b.Agents != nil {
		i.Agents = entryTexts(b.Agents)
	}
	if l := b.Location; l != nil {
		i.Dirs = entryTexts(l.Dirs)
		for _, r := range l.Repos {
			text := r.Text
			if r.Subdirs != nil {
				text += " (subdirs " + strings.Join(entryTexts(r.Subdirs), ", ") + ")"
			}
			i.Repos = append(i.Repos, text)
		}
		i.RepoSets = entryTexts(l.RepoSets)
	}
	return i
}

// conditions is one line about a binding's conditions.
func conditions(i bindingInfo) string {
	var parts []string
	for _, c := range []struct {
		key    string
		values []string
	}{{"dirs", i.Dirs}, {"repos", i.Repos}, {"repo-sets", i.RepoSets}, {"profiles", i.Profiles}, {"agents", i.Agents}} {
		if len(c.values) > 0 {
			parts = append(parts, c.key+" "+strings.Join(c.values, ", "))
		}
	}
	if len(parts) == 0 {
		return "every session"
	}
	return strings.Join(parts, "; ")
}

// bindingList is binding list: the config's bindings, in order.
func bindingList(stdio IO, asJSON bool) int {
	env := loadEnv()
	out := []bindingInfo{}
	for _, b := range env.Config.Bindings {
		out = append(out, bindingOf(b))
	}
	var errs []string
	for _, e := range env.Config.Errors {
		errs = append(errs, paths.Tilde(env.Path)+": "+e.Error())
	}
	if asJSON {
		writeJSON(stdio.Out, out, diagnostics("error", errs))
	} else {
		if len(out) == 0 {
			fmt.Fprintln(stdio.Out, "no bindings (add one with ctxtra binding add)")
		}
		for _, b := range out {
			fmt.Fprintf(stdio.Out, "%s: %s\n", b.Name, conditions(b))
			for _, e := range b.Collections {
				fmt.Fprintf(stdio.Out, "  collection   %s\n", e)
			}
			for _, e := range b.Instructions {
				fmt.Fprintf(stdio.Out, "  instruction  %s\n", e)
			}
			for _, e := range b.Skills {
				fmt.Fprintf(stdio.Out, "  skill        %s\n", e)
			}
			for _, e := range b.MCPServers {
				fmt.Fprintf(stdio.Out, "  mcp-server   %s\n", e)
			}
		}
		for _, e := range errs {
			fmt.Fprintf(stdio.Err, "error: %s\n", e)
		}
	}
	if len(errs) > 0 {
		return 1
	}
	return 0
}

// bindingRemove is binding remove: it removes one of the config's
// bindings, also one with errors.
func bindingRemove(stdio IO, name string, dryRun bool) int {
	s := newSources(stdio)
	if dryRun {
		data, err := readConfig(s.file)
		if err != nil {
			return s.fail("binding remove", err)
		}
		ed, err := config.NewEditor(data)
		if err == nil {
			err = ed.Remove(config.Bindings, name)
		}
		if err != nil {
			return s.fail("binding remove", err)
		}
		fmt.Fprintf(s.Out, "would remove binding %s from %s\nnothing changed (--dry-run)\n", name, paths.Tilde(s.file))
		return 0
	}
	if err := s.write(func(tx *edit.Tx) error { return tx.Config.Remove(config.Bindings, name) }); err != nil {
		return s.fail("binding remove", err)
	}
	fmt.Fprintf(s.Out, "removed binding %s from %s\n", name, paths.Tilde(s.file))
	return 0
}

func (a *app) bindingCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                   "binding",
		RunE:                  group,
		DisableFlagsInUseLine: true,
		Short:                 "Add, list and remove bindings: which sessions get which files",
		Long: `A binding adds instruction files, skills, collections and MCP servers to the
sessions where its conditions hold: its location (folders and repositories),
profiles and agents. A binding without conditions applies to every session.
Bindings apply in config order, which is the order of their text in a
session.`,
		Example: `  ctxtra binding add me --instruction ~/agent-context/profile.md
  ctxtra binding list`,
		GroupID: "config",
	}
	var b newBinding
	var o addFlags
	add := &cobra.Command{
		Use:   "add NAME [condition flags]... {content flags}... | add NAME --file FILE",
		Short: "Add a binding to the config",
		Long: `Add a binding to the config, the last one, or where --before or --after
says. It is written only when it works: a session it applies to would get all
it names. It then shows the binding as written, where it applies, and what a
session there gets.

Conditions: --dir, --repo and --repo-set are its location: a session in a
matching folder, or in a matching repository. --profile holds while one of
the profiles is active, --agent in sessions of those agents. Each condition
is left out when its flags are; a binding without --dir, --repo and
--repo-set applies everywhere. --subdir PATTERN limits the one --repo given
to those folders of the repository.

Content: --collection, --instruction, --skill and --mcp-server. A file,
skill, collection or server of a source is SOURCE:NAME. --source SOURCE
makes the values of --collection, --repo-set, --mcp-server, --instruction and
--skill names or paths in that source: SOURCE is a source name, or the
address of its repository (URL//FOLDER for the source with that subdir).

Each flag is repeatable. A value that starts with ! leaves out what it
matches: --profile '!oncall'. Quote such values and patterns, or the shell
reads them: --dir '~/dev/work/**'. x/** also matches x itself.

--file FILE, or --file - for stdin, gives the whole binding in YAML as in the
config, without its name, instead of the flags of its entries. A binding name
that exists is an error, unless the binding is the same; --replace changes it
in its place, and adds it when there is none.`,
		Example: `  ctxtra binding add me --instruction ~/agent-context/profile.md
  ctxtra binding add work --dir '~/dev/work/**' --instruction ~/agent-context/work.md --skill ~/skills/deploy
  ctxtra binding add iam --profile iam --collection iam-team:essentials
  ctxtra binding add iam-work --source git@github.com:acme/iam-ctx.git --repo-set services --collection essentials
  ctxtra binding add billing --repo github.com/acme/mono --subdir 'services/billing/**' --instruction 'iam-team:billing.md'
  ctxtra binding add tracker --agent '!cursor' --profile '!oncall' --mcp-server linear --before go
  ctxtra binding add obs --replace --file obs.yaml`,
		Args: exactly("NAME"),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			b.Name = args[0]
			return bindingAdd(a.IO, b, o)
		}),
	}
	f := add.Flags()
	f.StringArrayVar(&b.Dirs, "dir", nil, "apply in folders that match `PATTERN`")
	f.StringArrayVar(&b.Repos, "repo", nil, "apply in repositories whose remote matches `PATTERN`, such as github.com/acme/**")
	f.StringArrayVar(&b.Subdirs, "subdir", nil, "apply only in folders of the one --repo that match `PATTERN`, relative to its root")
	f.StringArrayVar(&b.RepoSets, "repo-set", nil, "apply in the repositories of the repo set `NAME`: the config's, or SOURCE:SET")
	f.StringArrayVar(&b.Profiles, "profile", nil, "apply while the profile `NAME` is active; !NAME while it isn't")
	f.StringArrayVar(&b.Agents, "agent", nil, "apply in sessions of the agent `NAME`; !NAME in all others")
	f.StringArrayVar(&b.Collections, "collection", nil, "add the collection `NAME`: the config's, or SOURCE:COLLECTION")
	f.StringArrayVar(&b.Instructions, "instruction", nil, "add the instruction file `FILE`; !FILE leaves files out")
	f.StringArrayVar(&b.Skills, "skill", nil, "add the skill folder `DIR`, with a SKILL.md; !DIR leaves skills out")
	f.StringArrayVar(&b.MCPServers, "mcp-server", nil, "add the MCP server `NAME`: the config's, or SOURCE:SERVER")
	f.StringVar(&o.source, "source", "", "make collection, repo set, server, file and skill values names or paths in `SOURCE`, a name or a repository address")
	f.StringVar(&o.file, "file", "", "read the binding from `FILE` in YAML, - for stdin, instead of the entry flags")
	f.BoolVar(&o.replace, "replace", false, "change the binding in its place if it exists")
	f.StringVar(&o.at.Before, "before", "", "put the new binding before the binding `NAME`")
	f.StringVar(&o.at.After, "after", "", "put the new binding after the binding `NAME`")
	f.BoolVar(&o.dryRun, "dry-run", false, "check the binding and show it; change nothing")
	var asJSON bool
	list := &cobra.Command{
		Use:     "list",
		Short:   "List the bindings, in config order",
		Example: "  ctxtra binding list\n  ctxtra binding list --json",
		Args:    noArgs,
		RunE: a.run(func(*cobra.Command, []string) error {
			return code(bindingList(a.IO, asJSON))
		}),
	}
	list.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	var removeDry bool
	remove := &cobra.Command{
		Use:     "remove NAME",
		Short:   "Remove a binding from the config",
		Example: `  ctxtra binding remove work`,
		Args:    exactly("NAME"),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(bindingRemove(a.IO, args[0], removeDry))
		}),
	}
	remove.Flags().BoolVar(&removeDry, "dry-run", false, "show the change; change nothing")
	c.AddCommand(add, list, remove)
	return c
}
