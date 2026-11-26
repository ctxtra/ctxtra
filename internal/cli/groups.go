package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/edit"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
)

func (a *app) groupCmd(repoSet bool) *cobra.Command {
	name, section := "collection", config.Collections
	if repoSet {
		name, section = "repo-set", config.RepoSets
	}
	c := &cobra.Command{Use: name, Short: "Add, list and remove the config's " + string(section),
		Example: "  ctxtra " + name + " list", GroupID: "config", RunE: group, DisableFlagsInUseLine: true}
	var b newBinding
	var o addFlags
	add := &cobra.Command{Use: "add NAME [entry flags]... | add NAME --file FILE", Short: "Add a " + section.Kind() + " to the config",
		Long: `Add an entry, or change it in place with --replace. The same entry already
there changes nothing. --replace adds it when missing. --file FILE, or - for
stdin, gives the whole entry in YAML instead of entry flags. List flags are
repeatable; quote patterns and values starting with !.`,
		Example: "  ctxtra " + name + " add work --file work.yaml", Args: exactly("NAME"),
		RunE: a.run(func(_ *cobra.Command, args []string) error { b.Name = args[0]; return groupAdd(a.IO, section, b, o) })}
	if repoSet {
		add.Long += "\n\nWith --subdir, give exactly one --repo; for several repositories with subdirs, use --file."
		b.repoFlags(add.Flags())
	} else {
		b.contentFlags(add.Flags(), &o)
	}
	o.flags(add.Flags(), section.Kind())
	var asJSON, dryRemove bool
	list := &cobra.Command{Use: "list", Short: "List the config's " + string(section), Args: noArgs,
		Example: "  ctxtra " + name + " list --json",
		RunE:    a.run(func(*cobra.Command, []string) error { return code(groupList(a.IO, section, asJSON)) })}
	list.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	remove := &cobra.Command{Use: "remove NAME", Short: "Remove a " + section.Kind() + " from the config", Args: exactly("NAME"),
		Example: "  ctxtra " + name + " remove work",
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			s := newSources(a.IO)
			err := s.editEntry(dryRemove, func(ed *config.Editor) error { return ed.Remove(section, args[0]) })
			if err != nil {
				return code(s.fail(name+" remove", err))
			}
			verb := "removed"
			if dryRemove {
				verb = "would remove"
			}
			fmt.Fprintf(a.Out, "%s %s %s from %s\n", verb, section.Kind(), args[0], paths.Tilde(s.file))
			if dryRemove {
				fmt.Fprintln(a.Out, "nothing changed (--dry-run)")
			}
			return nil
		})}
	remove.Flags().BoolVar(&dryRemove, "dry-run", false, "show the change; change nothing")
	c.AddCommand(add, list, remove)
	return c
}

// editEntry uses the same check for a preview as for a locked write.
func (s *sources) editEntry(dry bool, change func(*config.Editor) error) error {
	checked := func(ed *config.Editor) error {
		if err := change(ed); err != nil {
			return err
		}
		return ed.Check(config.Options{Agents: registry.Names()})
	}
	if !dry {
		return s.write(func(tx *edit.Tx) error { return checked(tx.Config) })
	}
	data, err := readConfig(s.file)
	if err != nil {
		return err
	}
	ed, err := config.NewEditor(data)
	if err != nil {
		return err
	}
	return checked(ed)
}

func groupAdd(stdio IO, section config.Section, b newBinding, o addFlags) error {
	s := newSources(stdio)
	command := strings.ReplaceAll(section.Kind(), " ", "-") + " add"
	fail := func(err error) error { return code(s.fail(command, err)) }
	if err := config.CheckName(b.Name); err != nil {
		return fail(err)
	}
	if o.file != "" && (b.entryFlags() || o.source != "") {
		return fmt.Errorf("--file gives the whole %s; leave out the flags of its entries", section.Kind())
	}
	if o.file == "" && !b.entryFlags() {
		return fmt.Errorf("give at least one entry flag, or --file FILE")
	}
	if len(b.Subdirs) > 0 && len(b.Repos) != 1 {
		return fail(errors.New("--subdir needs exactly one --repo; for other shapes use --file; nothing changed"))
	}
	var entry *yaml.Node
	if o.file != "" {
		var err error
		entry, err = readEntry(stdio.In, o.file)
		if err != nil {
			return fail(err)
		}
	} else if o.source == "" {
		wd, _ := os.Getwd()
		for _, list := range [][]string{b.Instructions, b.Skills} {
			for i, p := range list {
				list[i] = diskPath(wd, p)
			}
		}
	}
	changed, replaced := false, false
	err := s.editEntry(o.dryRun, func(ed *config.Editor) error {
		n := entry
		if n == nil {
			value := b
			// Qualifying source references must not change the flags on a
			// second evaluation of the same command.
			value.Collections, value.MCPServers = slices.Clone(b.Collections), slices.Clone(b.MCPServers)
			value.Instructions, value.Skills = slices.Clone(b.Instructions), slices.Clone(b.Skills)
			if o.source != "" {
				name, err := findSource(config.Parse(ed.Bytes(), config.Options{Agents: registry.Names()}), o.source)
				if err != nil {
					return err
				}
				if err := value.inSource(name); err != nil {
					return err
				}
			}
			n = value.node()
			if section == config.RepoSets {
				n = value.reposNode()
			}
		}
		var err error
		if o.replace {
			changed, replaced, err = ed.Put(section, b.Name, n, config.Place{})
		} else {
			changed, err = ed.Add(section, b.Name, n, config.Place{})
		}
		if err != nil || !changed {
			return err
		}
		if err := ed.Check(config.Options{Agents: registry.Names()}); err != nil {
			return err
		}
		cfg := config.Parse(ed.Bytes(), config.Options{Agents: registry.Names()})
		if section == config.RepoSets {
			if set := cfg.RepoSet(b.Name); set == nil || len(set.Entries) == 0 {
				return errors.New("a repo set needs at least one repository; nothing changed")
			}
			return nil
		}
		coll := cfg.Collection(b.Name)
		if coll == nil || len(slices.Concat(coll.Collections, coll.Instructions, coll.Skills, coll.MCPServers)) == 0 {
			return errors.New("a collection needs at least one entry; nothing changed")
		}
		env := resolve.LoadData(s.file, ed.Bytes(), registry.Names())
		wd, _ := os.Getwd()
		res := env.Alone(&config.Binding{Name: b.Name, Content: config.Content{Collections: []config.Entry{{Text: b.Name, Value: b.Name}}}}, resolve.Input{Dir: wd})
		var problems []error
		for _, e := range res.Errors {
			problems = append(problems, e.Err)
		}
		for _, t := range res.Trace {
			for _, u := range t.Unavailable {
				problems = append(problems, fmt.Errorf("source %s: %s", u, env.Catalog.Source(u).Why))
			}
		}
		return errors.Join(problems...)
	})
	if err != nil {
		var exists *config.ExistsError
		if errors.As(err, &exists) {
			err = fmt.Errorf("%w; use --replace to change it; nothing changed", err)
		}
		return fail(err)
	}
	verb := "added"
	if replaced {
		verb = "replaced"
	}
	if o.dryRun {
		verb = "would add"
		if replaced {
			verb = "would replace"
		}
	}
	if !changed {
		fmt.Fprintf(s.Out, "the %s %s is in %s already\n", section.Kind(), b.Name, paths.Tilde(s.file))
	} else {
		fmt.Fprintf(s.Out, "%s %s %s in %s\n", verb, section.Kind(), b.Name, paths.Tilde(s.file))
	}
	if o.dryRun {
		fmt.Fprintln(s.Out, "nothing changed (--dry-run)")
	}
	return nil
}

type repoInfo struct {
	Repo    string   `json:"repo"`
	Subdirs []string `json:"subdirs,omitempty"`
}

type repoSetInfo struct {
	Name  string     `json:"name"`
	Repos []repoInfo `json:"repos"`
}

func groupList(stdio IO, section config.Section, asJSON bool) int {
	env := loadEnv()
	var out any
	if section == config.Collections {
		items := []bindingInfo{}
		for _, c := range env.Config.Collections {
			items = append(items, bindingOf(&config.Binding{Name: c.Name, Pos: c.Pos, Content: c.Content}))
		}
		out = items
	} else {
		items := []repoSetInfo{}
		for _, s := range env.Config.RepoSets {
			i := repoSetInfo{Name: s.Name, Repos: []repoInfo{}}
			for _, e := range s.Entries {
				i.Repos = append(i.Repos, repoInfo{e.Text, entryTexts(e.Subdirs)})
			}
			items = append(items, i)
		}
		out = items
	}
	problems := errorTexts(env.Errors)
	if asJSON {
		writeJSON(stdio.Out, out, diagnostics("error", problems))
	} else {
		count := 0
		switch items := out.(type) {
		case []bindingInfo:
			count = len(items)
			for _, c := range items {
				fmt.Fprintln(stdio.Out, c.Name+":")
				for _, list := range []struct {
					key     string
					entries []string
				}{{"collection", c.Collections}, {"instruction", c.Instructions}, {"skill", c.Skills}, {"mcp-server", c.MCPServers}} {
					for _, v := range list.entries {
						fmt.Fprintf(stdio.Out, "  %s  %s\n", list.key, v)
					}
				}
			}
		case []repoSetInfo:
			count = len(items)
			for _, s := range items {
				fmt.Fprintln(stdio.Out, s.Name+":")
				for _, r := range s.Repos {
					fmt.Fprintf(stdio.Out, "  %s", r.Repo)
					if len(r.Subdirs) > 0 {
						fmt.Fprintf(stdio.Out, " (subdirs %s)", strings.Join(r.Subdirs, ", "))
					}
					fmt.Fprintln(stdio.Out)
				}
			}
		}
		if count == 0 {
			fmt.Fprintf(stdio.Out, "no %s in the config\n", section)
		}
		for _, p := range problems {
			fmt.Fprintf(stdio.Err, "error: %s\n", p)
		}
	}
	if len(problems) > 0 {
		return 1
	}
	return 0
}
