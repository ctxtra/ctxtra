package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ctxtra/ctxtra"
	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/setup"
)

// quiet are the commands after which setupNotices doesn't run: setup and
// onboard, which just ran setup, doctor, which shows the same lines under each agent,
// source validate, which reads only the folder it checks, check-updates, which sessions run in
// the background, and the ones that only print help, the version or a completion script.
var quiet = []string{"setup", "onboard", "doctor", "validate", "check-updates", "help", "version", "bash", "zsh", "fish", "powershell"}

// root is the command tree.
func (a *app) root() *cobra.Command {
	root := &cobra.Command{
		Use:   "ctxtra",
		Short: "Add your instruction files and skills to agent sessions",
		Long: `ctxtra adds your instruction files and skills to coding-agent sessions, by
folder and repository, through the agents' session-start hooks.

Agents: ` + strings.Join(registry.Names(), ", ") + `.
Config: $XDG_CONFIG_HOME/ctxtra/config.yaml or ~/.config/ctxtra/config.yaml (format: ctxtra help config).`,
		Version:           a.version,
		SilenceErrors:     true,
		SilenceUsage:      true,
		PersistentPreRunE: a.global,
	}
	root.SetIn(a.In)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	root.SetVersionTemplate("{{.Version}}\n")
	// Defined here, so that cobra doesn't add -v for it.
	root.Flags().Bool("version", false, "print the version")
	pf := root.PersistentFlags()
	pf.BoolVar(&a.noInput, "no-input", false, "never ask; a step that needs an answer fails instead")
	root.AddGroup(
		&cobra.Group{ID: "setup", Title: "Set up:"},
		&cobra.Group{ID: "config", Title: "Change the config:"},
		&cobra.Group{ID: "inspect", Title: "See what sessions get:"},
		&cobra.Group{ID: "help", Title: "Help:"},
	)
	root.AddCommand(
		a.onboardCmd(), a.setupCmd(), a.importCmd(),
		a.sourceCmd(), a.bindingCmd(), a.skillCmd(), a.mcpCmd(),
		a.contextCmd(), a.doctorCmd(), a.checkUpdatesCmd(),
		a.versionCmd(), hookCmd(),
	)
	root.SetHelpCommand(a.helpCmd(root))
	root.SetHelpCommandGroupID("help")
	root.SetCompletionCommandGroupID("help")
	help := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		help(c, args)
		if c == root {
			fmt.Fprintf(c.OutOrStdout(), "\nHelp topics (ctxtra help TOPIC):\n%s", helpTopics())
		}
	})
	return root
}

// Positional arguments. Their errors are usage errors.

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %q", args[0])
	}
	return nil
}

// exactly accepts the arguments named in what, one each.
func exactly(what ...string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		switch {
		case len(args) < len(what):
			return fmt.Errorf("expected %s", strings.Join(what[len(args):], " "))
		case len(args) > len(what):
			return fmt.Errorf("unexpected argument %q", args[len(what)])
		}
		return nil
	}
}

// atMost accepts up to n arguments.
func atMost(n int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) > n {
			return fmt.Errorf("unexpected argument %q", args[n])
		}
		return nil
	}
}

// group runs a command that only groups others: its help, or a usage
// error for an unknown subcommand.
func group(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return c.Help()
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], c.CommandPath())
	c.SuggestionsMinimumDistance = 2
	if s := c.SuggestionsFor(args[0]); len(s) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(s, "\n\t")
	}
	return errors.New(msg)
}

// dirArg is the folder argument of the commands that show what a session
// gets: the current folder by default.
func dirArg(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return "."
}

func (a *app) onboardCmd() *cobra.Command {
	var agents []string
	c := &cobra.Command{
		Use:   "onboard",
		Short: "Get started with guided hook setup, skills and instruction import",
		Long: `Set up hooks for your agents, learn about ctxtra's skills and choose which
ones to install. Then optionally start an installed agent to help move your
existing instruction files into ctxtra. Each step explains what it changes.

Needs an interactive terminal. For scripts and agents, use setup, skill install
and import separately. Without --agent, sets up every agent found on this machine.`,
		Example: `  ctxtra onboard
  ctxtra onboard --agent claude-code --agent codex`,
		Args:    noArgs,
		GroupID: "setup",
		RunE: a.run(func(*cobra.Command, []string) error {
			return code(onboard(a.IO, agents, a.prompts()))
		}),
	}
	c.Flags().StringArrayVar(&agents, "agent", nil, "set up only `AGENT` (repeatable)")
	return c
}

func (a *app) setupCmd() *cobra.Command {
	var o setup.Options
	c := &cobra.Command{
		Use:   "setup",
		Short: "Add ctxtra's hooks to your agents' user-level configs",
		Long: `Add hooks that run this binary to the user-level config of each agent, so
that new sessions get your instruction files and skills. Without --agent:
every agent found on this machine. Run it again after you move the binary.

Agents: ` + strings.Join(registry.Names(), ", ") + `.`,
		Example: `  ctxtra setup
  ctxtra setup --agent claude-code --agent codex --dry-run
  ctxtra setup --remove --agent cursor`,
		Args:    noArgs,
		GroupID: "setup",
		RunE: a.run(func(*cobra.Command, []string) error {
			return code(setupCmd(a.IO, o))
		}),
	}
	c.Flags().StringArrayVar(&o.Agents, "agent", nil, "change only `AGENT` (repeatable)")
	c.Flags().BoolVar(&o.Remove, "remove", false, "remove ctxtra's hook entries, and nothing else")
	c.Flags().BoolVar(&o.DryRun, "dry-run", false, "show what would change; change nothing")
	return c
}

func (a *app) importCmd() *cobra.Command {
	var start string
	var dryRun bool
	c := &cobra.Command{
		Use:   "import",
		Short: "Back up your agents' own instruction files, and move them into ctxtra",
		Long: `Back up the instruction files that your agents have of their own (such as
~/.claude/CLAUDE.md) and list them. With --start, then start that agent with
the ctxtra skill to move them into ctxtra, so that all your agents get them.
The agent shows you the result and empties the files only if you agree.

--start needs a terminal and the ctxtra skill (ctxtra skill install ctxtra).`,
		Example: `  ctxtra import
  ctxtra import --start claude-code`,
		Args:    noArgs,
		GroupID: "setup",
		RunE: a.run(func(*cobra.Command, []string) error {
			return code(runImport(a.IO, start, dryRun, a.prompts()))
		}),
	}
	c.Flags().StringVar(&start, "start", "", "start `AGENT` to move the files into ctxtra")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "list the files; back up and start nothing")
	return c
}

func (a *app) sourceCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                   "source",
		RunE:                  group,
		DisableFlagsInUseLine: true,
		Short:                 "Add, update and remove sources: repositories and folders of files",
		Long: `A source is where instruction files, skills and MCP servers come from:

  repo  a git repository, read at the commit you accepted; ctxtra.lock, next
        to the config, holds the accepted commits
  dir   a folder of your own files, read live: changes apply right away

Bindings name the files of a source as SOURCE:PATH, and its servers as
SOURCE:SERVER.`,
		Example: `  ctxtra source add --repo https://github.com/acme/agent-context
  ctxtra source update`,
		GroupID: "config",
	}
	c.AddCommand(a.sourceAddCmd(), a.sourceListCmd(), a.sourceShowCmd(), a.sourceUpdateCmd(), a.sourceDownloadCmd(),
		a.sourceTrustCmd(), a.sourceRemoveCmd(), a.sourceValidateCmd())
	return c
}

func (a *app) sourceAddCmd() *cobra.Command {
	o := addOptions{checkUpdates: true}
	c := &cobra.Command{
		Use:   "add (--repo URL | --dir DIR)",
		Short: "Add a git repository or a folder as a source",
		Long: `Add a source.

--repo adds a git repository, read at accepted commits: ctxtra fetches what
the source follows and records its commit in ctxtra.lock. Sessions use exactly
that commit until you accept a newer one with ctxtra source update. A source
follows one of:

  --branch BRANCH  new commits on the branch (the default: the remote's
                   default branch, or your clone's checked-out branch)
  --tag TAG        the tag; when someone moves it, you review the change
  --rev REV        one commit; REV may be any revision, and the config gets
                   its full ID

--subdir PATH makes a folder of the repository the source's top: its
manifest and every SOURCE:PATH are under it. The size limits still count the
whole repository, and nothing is written when it is over them.

URL may be the path of your clone: then ctxtra follows its checked-out
branch, and uses only committed files.

Sessions check a repo source for new commits, or a moved tag, in the
background at most once an hour, using your git credentials. Credential
helpers or hardware keys can ask for approval or a touch. Use
--check-updates=false to turn checks off. A source at a rev has nothing to
check.

Without --name, a source is named after the repository, and with --subdir
after the repository and the folder's last part.

--dir adds a folder of your own files, read live: changes apply to new
sessions right away, without review. When the folder is in a repository with
a remote, others can push to it: ctxtra reads it only after ctxtra source
trust NAME.

Adding the same repository, branch, tag or rev and subdir again changes
nothing, also with another --name.`,
		Example: `  ctxtra source add --repo https://github.com/acme/agent-context
  ctxtra source add --repo https://github.com/acme/agent-context --subdir teams/billing
  ctxtra source add --repo https://github.com/acme/observability-ctx --tag v1.4.0
  ctxtra source add --repo ~/dev/agent-context --name team-dev
  ctxtra source add --dir ~/dev/dotfiles`,
		Args: noArgs,
		RunE: a.run(func(cmd *cobra.Command, _ []string) error {
			refs := 0
			for _, f := range []string{"branch", "tag", "rev"} {
				if cmd.Flags().Changed(f) {
					refs++
				}
			}
			switch {
			case (o.repo == "") == (o.dir == ""):
				return errors.New("give --repo URL or --dir DIR")
			case o.dir != "" && (refs > 0 || cmd.Flags().Changed("subdir") || cmd.Flags().Changed("check-updates")):
				return errors.New("--branch, --tag, --rev, --subdir and --check-updates are for --repo; a dir source is read live")
			case refs > 1:
				return errors.New("give one of --branch, --tag and --rev")
			case o.rev != "" && cmd.Flags().Changed("check-updates"):
				return errors.New("a source at a rev has nothing to check; leave out --check-updates")
			}
			s := newSources(a.IO)
			return s.result("source add", s.add(o))
		}),
	}
	f := c.Flags()
	f.StringVar(&o.repo, "repo", "", "add the git repository at `URL`, or your clone at a path")
	f.StringVar(&o.dir, "dir", "", "add the folder `DIR`, read live")
	f.StringVar(&o.branch, "branch", "", "follow `BRANCH` (default: the remote's default branch, or the clone's checked-out branch)")
	f.StringVar(&o.tag, "tag", "", "follow the tag `TAG`")
	f.StringVar(&o.rev, "rev", "", "stay at the commit `REV`, any revision; the config gets its full ID")
	f.StringVar(&o.subdir, "subdir", "", "make the folder `PATH` of the repository the source's top")
	f.StringVar(&o.name, "name", "", "name the source `NAME` (default: from the URL or folder)")
	f.BoolVar(&o.dryRun, "dry-run", false, "check and show the change; change nothing and fetch nothing")
	f.BoolVar(&o.checkUpdates, "check-updates", true, "have sessions check the source for new commits at most once an hour; use --check-updates=false to disable")
	return c
}

func (a *app) sourceListCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:     "list",
		Short:   "List the sources",
		Example: "  ctxtra source list\n  ctxtra source list --json",
		Args:    noArgs,
		RunE: a.run(func(*cobra.Command, []string) error {
			return code(newSources(a.IO).list(asJSON))
		}),
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func (a *app) sourceShowCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:     "show NAME",
		Short:   "Show a source: its accepted commit, state and last check",
		Example: `  ctxtra source show team`,
		Args:    exactly("NAME"),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(newSources(a.IO).show(args[0], asJSON))
		}),
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func (a *app) sourceUpdateCmd() *cobra.Command {
	var full, dryRun, asJSON bool
	c := &cobra.Command{
		Use:   "update [NAME]...",
		Short: "Fetch repo sources, review what changed, and accept it",
		Long: `Fetch the repo sources (all of them without NAME), show what changed in
the files your bindings use, and ask whether to accept the new commit. New
sessions get the new files only after you accept them; ctxtra.lock records
the accepted commit. What counts as new depends on what the source follows:
new commits of a branch that change files under its subdir, a moved tag, and
for a rev nothing until you edit it. A changed branch, tag or rev is reviewed
as a change from the accepted commit; another address or subdir is a new
source, reviewed from nothing, as is a source without an accepted commit.

The review warns about text that is easy to miss in the lines that change:
hidden characters, HTML comments, long encoded blocks, curl | sh and similar
commands, secret paths like ~/.ssh, new URLs, and new executable files. The
warnings don't block anything, and no warning doesn't mean the source is safe.

Accepting needs a terminal. --dry-run fetches and shows the same review but
accepts and dismisses nothing; it works anywhere, for example in an agent. It
writes only to ctxtra's cache.`,
		Example: `  ctxtra source update
  ctxtra source update team --full
  ctxtra source update --dry-run --json`,
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			if asJSON && !dryRun {
				return errors.New("--json needs --dry-run")
			}
			s := newSources(a.IO)
			s.full, s.prompt = full, a.prompts()
			return code(s.update(args, dryRun, asJSON))
		}),
	}
	c.Flags().BoolVar(&full, "full", false, "show every changed file in full, also the ones you don't use")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "fetch and review only; accept and dismiss nothing")
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON (with --dry-run)")
	return c
}

func (a *app) sourceDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "download [NAME]...",
		Short: "Download exactly the commits that ctxtra.lock accepted",
		Long: `Download the commits that ctxtra.lock accepted into ctxtra's store, for
example on a new machine. It asks nothing, accepts nothing new and never
changes ctxtra.lock. Sources that are not on this machine are skipped.`,
		Example: `  ctxtra source download`,
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(newSources(a.IO).download(args))
		}),
	}
}

func (a *app) sourceTrustCmd() *cobra.Command {
	var dryRun bool
	c := &cobra.Command{
		Use:   "trust NAME",
		Short: "Read a dir source live although its repository has remotes",
		Long: `Trust the dir source NAME: record the remotes of its folder's repository,
and read its files live, without review, while they stay the same. When they
change, ctxtra stops reading the folder until you trust it again. A folder
whose repository has no remote needs no trust.`,
		Example: `  ctxtra source trust dotfiles`,
		Args:    exactly("NAME"),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			s := newSources(a.IO)
			return s.result("source trust", s.trust(args[0], dryRun))
		}),
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show the change; change nothing")
	return c
}

func (a *app) sourceValidateCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "validate DIR",
		Short: "Check a source's ctxtra-source.yaml, for example in CI",
		Long: `Check DIR/ctxtra-source.yaml, the manifest of a checkout of a source, the way
sessions read it: its YAML, its collections, MCP servers and repo sets.

It reads only DIR: not your config or sources, and it runs and fetches
nothing. It checks that the manifest is usable, not what the files say.

Exit code: 0 without errors, also with warnings; 1 with errors.`,
		Example: `  ctxtra source validate .
  ctxtra source validate ~/dev/team-ctx --json`,
		Args: exactly("DIR"),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			wd, _ := os.Getwd()
			return code(validateSource(a.IO, paths.Resolve(wd, args[0]), asJSON))
		}),
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func (a *app) sourceRemoveCmd() *cobra.Command {
	var dryRun bool
	c := &cobra.Command{
		Use:     "remove NAME",
		Short:   "Remove a source and its accepted commit",
		Example: `  ctxtra source remove team`,
		Args:    exactly("NAME"),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			s := newSources(a.IO)
			return s.result("source remove", s.removeSource(args[0], dryRun))
		}),
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show the change; change nothing")
	return c
}

func (a *app) bindingCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                   "binding",
		RunE:                  group,
		DisableFlagsInUseLine: true,
		Short:                 "Add, list and remove bindings: which sessions get which files",
		Long: `A binding adds instruction files, skills and MCP servers to the sessions
where its conditions hold: every session without conditions, or sessions in
matching folders or repositories. Bindings apply in config order, which is
the order of their text in a session.`,
		Example: `  ctxtra binding add me --instruction ~/agent-context/profile.md
  ctxtra binding list`,
		GroupID: "config",
	}
	var b newBinding
	var dryRun bool
	add := &cobra.Command{
		Use:   "add NAME [--dir PATTERN | --repo PATTERN]... {--instruction FILE | --skill DIR | --mcp-server NAME}...",
		Short: "Add a binding to the config",
		Long: `Add a binding to the config, the last one. It is written only when it works:
a session it applies to would get all it names. Without --dir and --repo it
applies to every session; with them, to sessions in a matching folder or in a
matching repository.

A file or skill of a source is SOURCE:PATH; a source's server is
SOURCE:SERVER. An entry that starts with ! leaves out what it matches.

Quote patterns, or the shell expands them: --dir '~/dev/work/**'. x/** also
matches x itself.`,
		Example: `  ctxtra binding add me --instruction ~/agent-context/profile.md
  ctxtra binding add work --dir '~/dev/work/**' --instruction ~/agent-context/work.md --skill ~/skills/deploy
  ctxtra binding add acme --repo 'github.com/acme/**' --instruction 'team:acme.md'`,
		Args: exactly("NAME"),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			b.Name = args[0]
			return bindingAdd(a.IO, b, dryRun)
		}),
	}
	f := add.Flags()
	f.StringArrayVar(&b.Dirs, "dir", nil, "apply in folders that match `PATTERN` (repeatable)")
	f.StringArrayVar(&b.Repos, "repo", nil, "apply in repositories whose remote matches `PATTERN` (repeatable), such as github.com/acme/**")
	f.StringArrayVar(&b.Instructions, "instruction", nil, "add the instruction file `FILE` (repeatable); !FILE leaves files out")
	f.StringArrayVar(&b.Skills, "skill", nil, "add the skill folder `DIR`, with a SKILL.md (repeatable); !DIR leaves skills out")
	f.StringArrayVar(&b.MCPServers, "mcp-server", nil, "add the MCP server `NAME`: the config's, or SOURCE:SERVER (repeatable)")
	f.BoolVar(&dryRun, "dry-run", false, "check the binding and show it; change nothing")
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

func (a *app) skillCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                   "skill",
		RunE:                  group,
		DisableFlagsInUseLine: true,
		Short:                 "Show and install ctxtra's own skills",
		Long: `ctxtra comes with skills for your agents:

  ctxtra         lets your agents set up and change ctxtra for you
  ctxtra-follow  when an agent's work moves to another folder, it gets your
                 bindings for that folder too`,
		Example: `  ctxtra skill install ctxtra`,
		GroupID: "config",
	}
	var asJSON bool
	list := &cobra.Command{
		Use:     "list",
		Short:   "List ctxtra's skills and whether each is installed",
		Example: "  ctxtra skill list",
		Args:    noArgs,
		RunE: a.run(func(*cobra.Command, []string) error {
			return code(skillList(a.IO, asJSON))
		}),
	}
	list.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	show := &cobra.Command{
		Use:     "show NAME",
		Short:   "Print a skill, to give it to any agent",
		Example: `  ctxtra skill show ctxtra`,
		Args:    exactly("NAME"),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(skillShow(a.IO, args[0]))
		}),
	}
	var dryRun bool
	install := &cobra.Command{
		Use:   "install NAME",
		Short: "Install a skill and add it to every session",
		Long: `Write the skill to ~/.local/share/ctxtra/skills/NAME and add a binding of the same name
that adds it to every session, unless a binding has it already.`,
		Example: `  ctxtra skill install ctxtra
  ctxtra skill install ctxtra-follow`,
		Args: exactly("NAME"),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(skillInstall(a.IO, args[0], dryRun))
		}),
	}
	install.Flags().BoolVar(&dryRun, "dry-run", false, "show the change; change nothing")
	c.AddCommand(list, show, install)
	return c
}

func (a *app) contextCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                   "context",
		RunE:                  group,
		DisableFlagsInUseLine: true,
		Short:                 "See what a session in a folder gets, and why",
		Long: `See what a new session in DIR (default: the current folder) gets from your
bindings. These commands only read: they fetch nothing and change nothing.`,
		Example: `  ctxtra context explain ~/dev/work/api
  ctxtra context show`,
		GroupID: "inspect",
	}
	var filesJSON bool
	var agentName string
	files := &cobra.Command{
		Use:     "files [DIR]",
		Short:   "List the instruction files and skills a session gets",
		Example: `  ctxtra context files ~/dev/work/api`,
		Args:    atMost(1),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(report{a.IO, a.version, agentName}.files(dirArg(args), filesJSON))
		}),
	}
	files.Flags().BoolVar(&filesJSON, "json", false, "print JSON")
	var o showOptions
	show := &cobra.Command{
		Use:   "show [DIR]",
		Short: "Print the exact text a session gets",
		Long: `Print the exact text that ctxtra adds to a new session in DIR.

--exclude-session prints only what DIR adds to a session that runs in another
folder already: ctxtra resolves that folder again now, it doesn't read the
running session. --part prints one part of a long text, for agents that cut
long command output.`,
		Example: `  ctxtra context show
  ctxtra context show ~/dev/api --exclude-session ~/dev/web --part 1`,
		Args: atMost(1),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(report{a.IO, a.version, agentName}.show(dirArg(args), o))
		}),
	}
	show.Flags().StringArrayVar(&o.exclude, "exclude-session", nil, "leave out what a session in `DIR` gets (repeatable)")
	show.Flags().IntVar(&o.part, "part", 0, fmt.Sprintf("print part `N` of the text, in parts of at most %d bytes", showPart))
	var explainJSON bool
	explain := &cobra.Command{
		Use:     "explain [DIR]",
		Short:   "Show why each binding matches or not, and the size of each file",
		Example: `  ctxtra context explain ~/dev/work/api`,
		Args:    atMost(1),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(report{a.IO, a.version, agentName}.explain(dirArg(args), explainJSON))
		}),
	}
	explain.Flags().BoolVar(&explainJSON, "json", false, "print JSON")
	for _, cmd := range []*cobra.Command{files, show, explain} {
		cmd.Flags().StringVar(&agentName, "agent", "", "as a session of `AGENT`; without it, bindings with agents don't apply")
	}
	c.AddCommand(files, show, explain)
	return c
}

func (a *app) doctorCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor [DIR]",
		Short: "Check the config, the agents' setup and recent hook runs",
		Long: `Check config problems in every binding, what a session in DIR (default: the
current folder) gets, the setup of every agent (including Codex's hook trust),
and the last hook runs there. It exits with 1 when it finds a problem.`,
		Example: `  ctxtra doctor`,
		Args:    atMost(1),
		GroupID: "inspect",
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(report{a.IO, a.version, ""}.doctor(dirArg(args), asJSON))
		}),
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print the version",
		Example: "  ctxtra version\n  ctxtra --version",
		Args:    noArgs,
		GroupID: "help",
		RunE: a.run(func(*cobra.Command, []string) error {
			fmt.Fprintln(a.Out, a.version)
			return nil
		}),
	}
}

// helpCmd shows help for a command, or a docs topic of this version.
func (a *app) helpCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [COMMAND... | TOPIC]",
		Short: "Help for a command, or a docs topic",
		Long: `Help for a command, the same as COMMAND --help, or a topic of the docs of
this version:

` + helpTopics(),
		Example: `  ctxtra help source add
  ctxtra help config`,
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			if len(args) == 1 {
				if t, ok := findTopic(args[0]); ok {
					text, err := t.read()
					if err != nil {
						fmt.Fprintf(a.Err, "ctxtra help: %v\n", err)
						return exitCode(1)
					}
					fmt.Fprint(a.Out, text)
					return nil
				}
			}
			c, rest, err := root.Find(args)
			if err != nil || len(rest) > 0 {
				return fmt.Errorf("no command or topic %q; topics:\n%s", strings.Join(args, " "), helpTopics())
			}
			return c.Help()
		}),
	}
}

// hookCmd documents the hook entry point. Run calls the hook before the
// command tree, so this command never runs.
func hookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hook AGENT [EVENT [--slot K --of N]]",
		Short: "The entry point of the agents' hooks",
		Long: `The command that the hooks written by ctxtra setup run: it reads the agent's
hook input (JSON) on stdin and answers with the text for the session, in the
agent's format. EVENT defaults to SessionStart. Agents with a size limit per
hook run several hooks, each with its slot K of N. It never fails a session:
problems go into the answer and the log. You don't run it yourself.`,
		Hidden:                true,
		DisableFlagParsing:    true,
		DisableFlagsInUseLine: true,
		Run:                   func(*cobra.Command, []string) {},
	}
}

// helpTopics lists the help topics, for help texts.
func helpTopics() string {
	var b strings.Builder
	for _, t := range topics {
		fmt.Fprintf(&b, "  %-10s %s\n", t.name, t.about)
	}
	return b.String()
}

// skillNames lists ctxtra's skills, for messages.
func skillNames() string { return strings.Join(ctxtra.SkillNames(), ", ") }
