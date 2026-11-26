package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/edit"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// newBinding is what binding add writes: the flags' values as given.
type newBinding struct {
	Name         string
	Dirs, Repos  []string
	Instructions []string
	Skills       []string
	MCPServers   []string
}

// node is the binding's entry in the config, without its name: location
// with dirs and repos, then the content lists.
func (b newBinding) node() *yaml.Node {
	list := func(values []string) *yaml.Node {
		n := &yaml.Node{Kind: yaml.SequenceNode}
		for _, v := range values {
			n.Content = append(n.Content, yamlx.Text(v))
		}
		return n
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key string, v *yaml.Node) { n.Content = append(n.Content, yamlx.Text(key), v) }
	if len(b.Dirs)+len(b.Repos) > 0 {
		loc := &yaml.Node{Kind: yaml.MappingNode}
		if len(b.Dirs) > 0 {
			loc.Content = append(loc.Content, yamlx.Text("dirs"), list(b.Dirs))
		}
		if len(b.Repos) > 0 {
			loc.Content = append(loc.Content, yamlx.Text("repos"), list(b.Repos))
		}
		add("location", loc)
	}
	for _, l := range []struct {
		key    string
		values []string
	}{{"instructions", b.Instructions}, {"skills", b.Skills}, {"mcp-servers", b.MCPServers}} {
		if len(l.values) > 0 {
			add(l.key, list(l.values))
		}
	}
	return n
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

// bindingAdd is binding add. Only a binding that works is written: a
// session it applies to would get all it names.
func bindingAdd(stdio IO, b newBinding, dryRun bool) error {
	if err := config.CheckName(b.Name); err != nil {
		return err
	}
	if len(b.Instructions)+len(b.Skills)+len(b.MCPServers) == 0 {
		return errors.New("add at least one --instruction, --skill or --mcp-server")
	}
	wd, _ := os.Getwd()
	for i, p := range b.Dirs {
		b.Dirs[i] = diskPath(wd, p)
	}
	for i, p := range b.Instructions {
		b.Instructions[i] = diskPath(wd, p)
	}
	for i, p := range b.Skills {
		b.Skills[i] = diskPath(wd, p)
	}
	s := newSources(stdio)
	added, err := s.addBinding(b.Name, b.node(), dryRun)
	if err != nil {
		return exitCode(s.fail("binding add", err))
	}
	switch {
	case !added:
		fmt.Fprintf(s.Out, "the binding %s is in %s already\n", b.Name, paths.Tilde(s.file))
		return nil
	case dryRun:
		fmt.Fprintf(s.Out, "would add binding %s to %s: %s\n", b.Name, paths.Tilde(s.file), where(b))
	default:
		fmt.Fprintf(s.Out, "added binding %s to %s: %s\n", b.Name, paths.Tilde(s.file), where(b))
	}
	if len(b.Instructions) > 0 {
		fmt.Fprintf(s.Out, "  instructions: %s\n", strings.Join(b.Instructions, ", "))
	}
	if len(b.Skills) > 0 {
		fmt.Fprintf(s.Out, "  skills: %s\n", strings.Join(b.Skills, ", "))
	}
	if len(b.MCPServers) > 0 {
		fmt.Fprintf(s.Out, "  MCP servers: %s\n", strings.Join(b.MCPServers, ", "))
	}
	if dryRun {
		fmt.Fprintln(s.Out, "nothing changed (--dry-run)")
		return nil
	}
	fmt.Fprintln(s.Out, "new sessions get it; a running session keeps what it got at its start")
	return nil
}

// where says which sessions binding b applies to.
func where(b newBinding) string {
	var parts []string
	if len(b.Dirs) > 0 {
		parts = append(parts, "folders "+strings.Join(b.Dirs, ", "))
	}
	if len(b.Repos) > 0 {
		parts = append(parts, "repositories "+strings.Join(b.Repos, ", "))
	}
	if len(parts) == 0 {
		return "every session"
	}
	return "sessions in " + strings.Join(parts, " or in ")
}

// addBinding adds the binding name with entry n, the last one, if a
// session it applies to would get all it names. added is false when the
// same binding is there already. With dryRun it only checks.
func (s *sources) addBinding(name string, n *yaml.Node, dryRun bool) (added bool, err error) {
	change := func(ed *config.Editor) error {
		if added, err = ed.Add(config.Bindings, name, n, config.Place{}); err != nil || !added {
			return err
		}
		return checkBinding(s.file, ed.Bytes(), name)
	}
	if dryRun {
		// Reads only: it works where the config can't be written, for
		// example in an agent's sandbox.
		data, err := readConfig(s.file)
		if err != nil {
			return false, err
		}
		ed, err := config.NewEditor(data)
		if err != nil {
			return false, err
		}
		err = change(ed)
		return added, err
	}
	err = s.write(func(tx *edit.Tx) error { return change(tx.Config) })
	return added, err
}

// checkBinding fails when the binding name of the config data has an error,
// or when a session it applies to wouldn't get all it names.
func checkBinding(file string, data []byte, name string) error {
	env := resolve.LoadData(file, data, registry.Names())
	b := env.Config.Binding(name)
	if b == nil {
		return fmt.Errorf("the binding %s isn't in the changed config; nothing changed", name)
	}
	var problems []string
	for _, e := range b.Errs {
		problems = append(problems, e.Error())
	}
	wd, _ := os.Getwd()
	res := env.Alone(b, resolve.Input{Dir: wd})
	for _, e := range res.Errors {
		problems = append(problems, e.Err.Error())
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
	if len(problems) > 0 {
		return fmt.Errorf("sessions wouldn't get the binding as asked; nothing changed:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
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
