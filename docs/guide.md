# ctxtra guide

Add your own instruction files and skills to Claude Code, Codex CLI, Pi, OpenCode, Muse Code, Hermes Agent,
DeepSeek Harness (dsh), Cursor Agent CLI, GitHub Copilot CLI, Antigravity and Devin (Devin CLI, and Devin Desktop,
formerly Windsurf) sessions, for the folders and repositories you choose, in every worktree. Nothing is written into
repositories, and the repo's own `AGENTS.md` and your user-level agent files keep working as before.

**Status: preview (`v0`, config `ctxtra/config/v1`).** The config format may still change. ctxtra does
not check what the bound files say: bind only files you wrote or reviewed.

## How it works

The agent IDs for commands are `claude-code`, `github-copilot` and `hermes-agent`. The other IDs are
`codex`, `cursor`, `opencode`, `pi`, `antigravity`, `devin`, `muse` and `dsh`.
After upgrading from an earlier release, run `ctxtra setup` again. Earlier hook and proxy entries add
nothing with this release. Setup replaces them; `ctxtra doctor` lists the command for each agent that
needs it. Once every agent is set up again, you can delete `~/.ctxtra`.

`ctxtra setup` adds a session-start hook to each agent's user-level config. When a session starts, the
hook reads your config, finds the bindings that match the session folder, and gives the agent the bound
files and a list of the bound skills. Pi and OpenCode have no command hooks: for them, setup writes a small
extension (`~/.pi/agent/extensions/ctxtra.ts`, `~/.config/opencode/plugins/ctxtra.ts`) that runs ctxtra.
For Copilot, setup writes a hook file of its own, `~/.copilot/hooks/ctxtra.json`.
For Antigravity, setup adds a hook named `ctxtra` to `~/.gemini/config/hooks.json`, which the Antigravity
CLI (`agy`), App and IDE share.
For Devin, setup adds a hook to `~/.config/devin/config.json`, which Devin CLI and Devin Desktop share.
Hermes's shell hooks don't get the session folder, so setup writes a Python plugin (`~/.hermes/plugins/ctxtra/`)
that adds the text to the first message of each session.
DSH's command hooks need an extra package, so setup writes a plugin, `~/.dsh/ctxtra.mjs`, and adds the row that
loads it to `~/.dsh/cordis.patch.yml`, which applies to every profile.

For Claude Code, setup also writes a small plugin, `~/.claude/skills/ctxtra/`, that offers the bound skills as
slash commands: type `/` in a session to see them, each with its description and `(ctxtra)`. See
[Skills as slash commands in Claude Code](#skills-as-slash-commands-in-claude-code).

At session start, Claude Code, Codex, Muse and Pi also show you a summary, for example:

```text
✓ ctxtra added 3 instructions and 3 skills
Instructions:
  dotfiles
  - global/english.md
  - go/style.md
  local
  - ~/work/AGENTS.md
Skills:
  ctxtra
  - ctxtra
  team-ctx
  - deploy
  - review
```

Selected MCP servers also appear in the count, for example
`✓ ctxtra added 3 instructions, 3 skills and 1 MCP server`. An `MCP servers:` list shows up to 30 of
them, without groups: a server from a source is shown as `SOURCE:NAME`, one of your config by its name.
Servers awaiting sign-in or approval are included, with their notices below. With no selected servers,
the summary stays as above.

The lists are grouped by source, in the order the session gets them. A file from a source is shown by its
path in the source; when one source's folder is inside another's, its files are in the inner source. A file that is in no source is in the `local` group, by its full path, from `~` when it
is under your home folder. Skills are shown by name. The skills that `ctxtra skill install` adds are in the
`ctxtra` group.

Each group lists up to 30 entries, then `+N more`. Change those limits with `session-message`:

```yaml
session-message:
  max-instructions: 10
  max-skills: 0
```

Zero hides that list. The first line still counts all instructions and skills.
Errors and notices appear below: the first three errors, with their line in the config or the command that fixes
them, and how many more `ctxtra doctor` shows. Over capacity is an error;
the message names files that did not fit. Cursor, Copilot, Antigravity, OpenCode, Hermes and DSH cannot
show a separate message to the user. `ctxtra doctor` and `ctxtra context explain` show the same errors.

## Install

```bash
brew install atelpis/tap/ctxtra   # or: go install github.com/ctxtra/ctxtra/cmd/ctxtra@latest
ctxtra setup --dry-run            # show what would change
ctxtra setup                      # every agent found on this machine, or --agent claude-code --agent codex
```

`setup` adds hooks that run the binary you started it from, by its full path: agents started from the Dock
often don't have `~/go/bin` in their `PATH`. It changes only its own hook entries and backs up each file it
changes to `~/.local/state/ctxtra/backups`. Setup and config edits keep the latest three backups per original file,
pruning only after a successful change. The latest backup is reused when its content is unchanged.
Cleanup is best effort: if an older backup cannot be removed, extra copies stay without failing the
config change. Import backups are not pruned. Older config backups without a
full-path hash in their names are left alone: their original paths cannot be identified safely. You can
remove those manually when you no longer need them; agent config backups may contain tokens.
Codex runs the hooks only after you trust them once (`/hooks` in Codex), and
asks once more after a hook changed, for example after an upgrade that changes the hooks.
Hermes loads the plugin only after you enable it once (`hermes plugins enable ctxtra`).
`ctxtra setup --remove` removes them.

After setting up this release, an upgrade needs `ctxtra setup` only when the release notes say so, 0.x releases included: each release
keeps serving what an earlier one set up ([setup compatibility](spec.md#setup-compatibility)). When the
notes say so, Codex asks once to trust the changed hooks after setup, and Hermes needs the plugin reloaded, or
Hermes restarted, if it is running. If the binary moves, run `ctxtra setup` again, and trust the hooks again
in Codex; `doctor` shows which binary the hooks run and whether it exists. Every other command says so too: a
line on stderr per agent whose setup needs you (a missing or other binary, an older setup with less room, a
setup that is incomplete or gone, an agent found since the last setup), with the command that repairs it (see
[the spec](spec.md#setup-notices)). Until then, Claude Code, Codex, Muse, Devin and Pi say at session
start that the binary is missing; the other agents add nothing, without a word.

At most once an hour, a session checks in the background for a new ctxtra release, and the next session
tells you when there is one, with the command that upgrades it. The check sends your IP address and ctxtra
version to GitHub. To turn it off, add `check-releases: false` to the config; `CTXTRA_NO_UPDATE_CHECK=1` stops it for
one environment, such as CI, if the hook inherits it. Muse clears that variable: turn release checks off in
its config and checks off on each repo source instead. See [Update checks](spec.md#update-checks).

To get release candidates before each release, install the `ctxtra-rc` formula instead:
`brew uninstall ctxtra && brew install atelpis/tap/ctxtra-rc`. A release candidate, such as
`v0.6.0-rc.1`, may still change before its release. The formula installs the same `ctxtra` command at the
same path, so the hooks keep working. It also follows every release, and its update notice includes release
candidates. Update it with `brew upgrade atelpis/tap/ctxtra-rc`. You can't install both formulas. To go
back, run `brew uninstall ctxtra-rc && brew install atelpis/tap/ctxtra`. The older release may not serve
what a release candidate set up: if you ran `ctxtra setup` with it, run `ctxtra setup` again.

## Let your agent help

ctxtra comes with a skill that teaches agents its commands, how to change the config safely, and the
risks of source repositories that other people push to. `ctxtra setup` prints the command that installs it:

```bash
ctxtra skill show ctxtra      # the skill's files, exactly as the agents get them
ctxtra skill install ctxtra   # writes it to ~/.local/share/ctxtra/skills/ctxtra, and adds a binding for it to your config
```

The binding is an ordinary one, named `ctxtra`, without conditions: `ctxtra binding remove ctxtra`
stops adding the skill. Then ask any agent, for example "add my instructions to every session in my work
repositories". The skill tells the agent to ask you before every change, and never to trust a repository or
accept a new commit for you. Before setup, you can paste the output of `ctxtra skill show ctxtra` into an
agent to start.

When your agents already have instruction files of their own, `ctxtra import` moves them into ctxtra, so that
all your agents get them; setup names it while no binding adds instructions. It takes the files of the
agents that are set up: the ones each agent reads at user level, checked against each agent by the e2e
tests:

| Agent | Files |
|---|---|
| Claude Code | `~/.claude/CLAUDE.md`, `~/.claude/rules/**/*.md` |
| Codex | `~/.codex/AGENTS.override.md`, `~/.codex/AGENTS.md` |
| Pi | the first of `~/.pi/agent/AGENTS.override.md`, `AGENTS.md`, `AGENTS.MD`, `CLAUDE.md`, `CLAUDE.MD` that exists; `APPEND_SYSTEM.md` |
| OpenCode | `~/.config/opencode/AGENTS.md` |
| Copilot | `~/.copilot/copilot-instructions.md`, `~/.copilot/instructions/**/*.instructions.md` |
| Antigravity | `~/.gemini/AGENTS.md`, `~/.gemini/GEMINI.md`, `~/.gemini/config/GEMINI.md` |
| Muse | `~/.config/muse/AGENTS.md` |
| DSH | `$DSH_HOME/AGENTS.md` |

Files that replace an agent's own system prompt or persona (Pi's `SYSTEM.md`, Hermes's `SOUL.md`) and files the
agent writes itself (Hermes's `memories/`) stay where they are. `ctxtra import` copies the files to
`~/.local/state/ctxtra/backups/import-<time>-<n>/<agent>/` and lists them. `ctxtra import --start claude-code` then starts that
agent in `~/.local/state/ctxtra/import`, in your terminal, with a prompt that lists the files and the backup. Every agent
above except DSH, whose app is a web page, can be started, and so can Hermes. The agent follows the skill
(install it first): it merges rules that repeat, asks about rules that fit only one agent, shows you the new
file, and binds it. Then it recommends removing the moved rules from the old files, so that your agents don't
get two copies that drift apart, and does it only if you agree. It asks before each change, as usual. Without
an agent to start, import prints the prompt for you to give an agent. It starts the agent with its normal
permissions and never with flags that skip them. When you ask an agent to import your instructions, the skill
runs `ctxtra import` and does the rest itself.

The skill is part of the binary: each version has a fixed text, which `ctxtra skill show ctxtra` shows. When you upgrade
ctxtra, the hook of the next session writes the new version to `~/.local/share/ctxtra/skills/ctxtra`, and the same for
ctxtra's other skills that you installed. Antigravity asks before it reads the skill there (see [Limits](#limits)).

## Configure

ctxtra reads one config: `~/.config/ctxtra/config.yaml`, or `$XDG_CONFIG_HOME/ctxtra/config.yaml` when
the variable is absolute. Its accepted commits are in `ctxtra.lock` next to the config's real file. Only
ctxtra writes the lock: keep it with the config, for example in your dotfiles.
There is no `--config` flag or `CTXTRA_CONFIG` override, and bindings cannot be included from other files.

On Linux and macOS, accepted source content and installed skills live in `~/.local/share/ctxtra`, state
and backups in `~/.local/state/ctxtra`, and git clones in `~/.cache/ctxtra`. Absolute `XDG_DATA_HOME`,
`XDG_STATE_HOME` and `XDG_CACHE_HOME` values replace those parent folders. Relative or empty values use the
defaults. `ctxtra doctor` shows all four folders. Clearing the cache leaves session content available.
Deleting the state folder forgets your profile selection, trusted folders, server approvals and sign-ins;
the session message says what to run again.
Codex passes these variables to its MCP proxy; Muse records their values during setup.

Start with your own files:

```yaml
schema: ctxtra/config/v1
bindings:
  - name: me
    instructions: [~/agent-context/me.md]
  - name: work
    location:
      repos: ['github.com/acme/**']
    instructions: [~/agent-context/work.md]
    skills: [~/agent-context/skills/deploy]
```

No location means every session. `location.dirs` selects folders; `location.repos` selects repositories
in any clone or worktree. Either can match when both are present. To select parts of a repository, use
`repos: [{repo: github.com/acme/mono, subdirs: ['services/billing/**']}]`.
Quoted `!` entries exclude places from their own list. `~/work/**` includes `~/work` itself.
Names of bindings, sources, collections, repo sets and profiles are 1 to 64 letters, digits, `.`, `_` and `-`,
starting with a letter; case counts.

The command for the work binding is:

```bash
ctxtra binding add work --repo 'github.com/acme/**' --instruction ~/agent-context/work.md --skill ~/agent-context/skills/deploy
```

Each key has a flag: `--dir`, `--repo`, `--repo-set`, `--profile`, `--agent`, `--collection`, `--instruction`,
`--skill` and `--mcp-server`, each repeatable. Quote values that start with `!` and patterns, or the shell reads
them: `--profile '!oncall'`. For one part of a monorepo, add `--subdir 'services/billing/**'` to a single
`--repo`. For any other shape, give the binding in YAML, without its name:

```bash
ctxtra binding add obs --file obs.yaml        # or --file - to read stdin
ctxtra binding add obs --replace --file obs.yaml
```

`--replace` changes the binding in its place, or adds it if it isn't there, so a script can run the line
again. A new binding goes last; `--before NAME` or `--after NAME` puts it elsewhere, since bindings apply in
order.

The command keeps existing comments and writes only when the binding works. It shows the binding as written,
where it applies, and the files, skills and servers a session there gets. `--dry-run` shows the same without
writing. The same binding again changes nothing; a different binding with that name is an error.
`ctxtra binding list` lists bindings and `ctxtra binding remove NAME` removes one.

### Your live folder

Files in repositories with remotes are refused until you trust them for live reading:

```bash
ctxtra source add --dir ~/dev/dotfiles
ctxtra source trust dotfiles
ctxtra binding add personal --instruction dotfiles:agents/me.md --skill 'dotfiles:skills/*'
```

This accepts whatever is in that folder, including other people's changes pulled into it.
With no remotes, a dir source needs no trust step. If its remotes change later, ctxtra stops reading it
until you trust it again. Files outside git and in repositories without remotes need no source.
The session message, `doctor` and `context explain` give the command to trust the source again.
When a folder gets its first remote, they also show how to add a repo source for reviewed commits.
`ctxtra source trust dotfiles --dry-run` explains the decision without recording trust.
A symlink is followed only when every link and its destination pass the same trust check.
Trust records include fetch and push URLs. Files are read and shown by the destination path that passed the check.
A dir source's path can lead through a symlink in another repository with remotes only when that folder is a
trusted dir source too. Otherwise name the folder the link leads to.
Live patterns can follow links to your own files and folders. Repo sources leave all symlinks out,
including symlinks in an explicit path or at the manifest.

### Reviewed source content

For a repository other people push to, add it as a repo source and bind its files explicitly:

```bash
ctxtra source add --repo git@github.com:acme/team-ctx.git
ctxtra binding add billing --repo github.com/acme/billing --instruction team-ctx:services-overview.md --skill team-ctx:skills/deploy
ctxtra source update --dry-run team-ctx
ctxtra source update team-ctx
```

A team's README can give the same two lines to everyone, whatever each person names the source:

```bash
ctxtra source add --repo git@github.com:acme/iam-ctx.git
ctxtra binding add iam-work --source git@github.com:acme/iam-ctx.git --repo-set services --collection essentials
```

`--source` takes a source name or its repository's address, in any form (`https://github.com/acme/iam-ctx`
works too), and makes `--collection`, `--repo-set`, `--mcp-server`, `--instruction` and `--skill` refer to that
source. For one folder of a shared repository, write `URL//teams/obs`. `binding add` shows content warnings
for the source's files that it binds for the first time. A source without a README still tells you how to
bind it: `source add` lists its collections and repo sets, and prints a `binding add` line that uses them.

Read the repository before adding it: its first commit is accepted without review.
Later commits reach sessions only after you review and accept them in your terminal.
`--dry-run` shows the review without accepting or dismissing it, and writes only to ctxtra's cache. Accepting covers every file in the commit,
also files shown only by name. The warnings help you read; they do not prove the text is safe.

`SOURCE:PATH` always means content from that source. An absolute, `~` or relative disk path stays a disk
path; it does not map to the reviewed copy. Use `./draft:notes.md` for a disk filename with a colon.
Relative config paths start at the config's real folder, after symlinks.

You can also review commits of a local clone:

```bash
ctxtra source add --repo ~/dev/team-ctx --name preview
ctxtra binding add preview --instruction preview:services-overview.md
```

This follows the clone's checked-out named branch when added. It reads accepted commits, never dirty files
or the branch's upstream. Use `--branch NAME` to choose another branch, or when HEAD is detached.

A source can follow a tag or stay at one commit instead of a branch:

```bash
ctxtra source add --repo https://github.com/acme/observability-ctx.git --tag v1.4.0
ctxtra source add --repo git@github.com:acme/security-guidelines.git --rev 9f8e7d6
```

With a tag, you hear when someone moves it, and `source update` shows the change for review. With `--rev`,
nothing is new until you edit `rev` in the config; `source add` writes the commit's full ID.

When teams share one repository, one folder per team, add only your team's folder:

```bash
ctxtra source add --repo git@github.com:acme/agent-context.git --subdir teams/billing
ctxtra binding add billing --skill 'agent-context-billing:skills/*'
```

The folder is the source's top: `agent-context-billing:skills/*` means `teams/billing/skills/*`, and its
manifest is `teams/billing/ctxtra-source.yaml`. Commits that change only other teams' folders are nothing
new for you. Changing `subdir` or `repo` later makes it a new source, which `source update` reviews from
nothing. Changing `branch`, `tag` or `rev` is reviewed as a change from the accepted commit.

On another machine, copy both the config and its lock, then run:

```bash
ctxtra source download
```

It downloads exactly the accepted commits and leaves the lock unchanged. Run it too after you delete
`~/.local/share/ctxtra`; deleting `~/.cache/ctxtra` changes nothing for sessions. Missing local source folders
are listed as not on this machine, without an error. A source without a matching lock entry is inactive;
run `ctxtra source update NAME` to review and accept a version.

Sessions check repo sources for new commits, or a moved tag, at most once an hour in the background.
Checks never accept them. Remote checks use your git credentials; keys needing a touch or approval can ask for it.
Use `--check-updates=false` when adding such a source, or set `check-updates: false` on it later.
`ctxtra check-updates` checks now. `ctxtra source list` and `ctxtra source show NAME` show source state.

### Collections

A source can name sets of its files, skills and servers in its `ctxtra-source.yaml`. Bind a set by its name,
and the source can move its files without breaking your config:

```yaml
bindings:
  - name: iam
    collections: ['iam-team:essentials']
    instructions: ['!iam-team:docs/testing.md']   # leave one file of the set out
```

Your config can have its own `collections` too, for one set in several places:

```bash
ctxtra collection add go-kit --instruction ~/agent-context/go.md --skill ~/agent-context/skills/go
ctxtra collection list --json
ctxtra collection remove go-kit
```

```yaml
collections:
  go-kit:
    instructions: [go-notes.md, 'go-style:go/*.md']
bindings:
  - {name: go, location: {dirs: ['~/dev/**']}, collections: [go-kit]}
  - {name: go-work, location: {repos: ['github.com/acme/**']}, collections: [go-kit]}
```

A collection's own entries come after the collections it nests. `context explain` shows what a binding's
collections added and what its `!` entries left out. `source update` tells you when a commit removes a
collection you use, or adds servers, skills or files to it.

### Repo sets

A team can list the repositories it works in, as a repo set in its `ctxtra-source.yaml`. Link a binding
to the set, and it follows the list: when the team adds a repository, your sessions there get the binding
after you accept the commit.

```yaml
bindings:
  - name: iam-repos
    location:
      repo-sets: ['iam-team:services']
      repos: [github.com/me/iam-playground, '!github.com/acme/iam-legacy']   # add one of mine, leave one of theirs out
    collections: ['iam-team:base']
```

Your config can have its own `repo-sets`, bound by its bare name. A `!` entry leaves out every repository
of a set:

```bash
ctxtra repo-set add clients --repo 'github.com/acme/client-*' --repo '!github.com/acme/client-demo'
ctxtra repo-set add billing --repo github.com/acme/mono --subdir 'services/billing/**'
ctxtra repo-set list --json
```

For either collections or repo sets, `add NAME --file entry.yaml` reads one entry in YAML, without its
name; `--file -` reads stdin. Entry flags cannot be combined with `--file`. `--replace` changes an
entry in its place, or adds it when missing. The same entry again changes nothing. Add and remove
accept `--dry-run`. Relative file and skill flags start at the current folder; paths in YAML start
at the config's real folder. Quote exclusions and patterns.

```yaml
repo-sets:
  clients: [github.com/acme/client-*]
bindings:
  - {name: tracker, location: {repo-sets: ['!clients']}, mcp-servers: [linear]}
```

If ctxtra can't read an excluded set, for example on a new machine before `ctxtra source download`, it
leaves out the binding, so it never reaches the repositories you excluded. `context explain` shows which
set and which of its entries matched. `source update` shows what a commit changes in a set you use, tells
you when it removes one, and warns about an entry such as `github.com/**`, which matches every repository on
the host.

### Program settings

```bash
ctxtra config set check-releases false
ctxtra config set max-ctx-chars 40000
ctxtra config set session-message.max-skills 0
ctxtra config unset session-message.max-skills
```

Only `check-releases`, `max-ctx-chars`, `session-message.max-skills` and
`session-message.max-instructions` can be set. Counts are whole numbers of 0 or more.
`unset` restores the default. `ctxtra help config` still shows the config format.
Counts use decimal values, including when written with leading zeros.
Every list command accepts `--json`. Exit codes are 0 for success, 1 for a mistake or refused change,
and 2 for bad flags or arguments.

### Profiles and agents

A binding can have `agents: [claude-code, codex]`, with or without location. Every condition present must
hold. `agents: ['!cursor']` means every agent except Cursor. Preview with
`ctxtra context explain DIR --agent codex`; without an agent argument, agent-specific bindings do not apply.

`profiles` is another condition, with case-sensitive names. A binding with `profiles: [iam]` applies only
while `iam` is active:

```yaml
bindings:
  - name: iam-work
    profiles: [iam]
    instructions: [~/agent-context/iam.md]
```

The same from the command line: `ctxtra binding add iam-work --profile iam --instruction ~/agent-context/iam.md`.

Choose the profiles for one launch with `CTXTRA_PROFILES`:

```bash
CTXTRA_PROFILES=iam claude            # iam
CTXTRA_PROFILES=iam,oncall codex      # several profiles
CTXTRA_PROFILES= claude               # none, whatever is stored
ctxtra context explain --profile iam  # preview, without switching
```

Without the variable, a session uses the selection stored on this machine, in
`~/.local/state/ctxtra/profiles.json`. Choose it with these commands; they don't change the config:

```bash
ctxtra profile set iam oncall        # replace the selection
ctxtra profile list                  # names, active selection and bindings
ctxtra profile clear                 # no stored profile
```

`set` accepts only names the bindings use, including names after `!`. Invalid or unknown names leave the
selection unchanged. `set` and `clear` refuse to overwrite a selection file they can't read. The session message
names the active profiles and where they came from. A name that no binding uses is reported as a likely typo.

A session keeps its profiles until it ends or you run `/clear`, even when you switch in another terminal.
Claude Code, Codex, Copilot, Muse, OpenCode, Hermes and DSH can keep the parent's profiles for subagents.
Run `ctxtra setup` again for OpenCode, Hermes and DSH to update their plugins. `doctor` reports an older
plugin that can't forward session IDs, with the setup command to fix it. Muse needs its native parent
ID; without it, a subagent uses the current selection. Antigravity gives no parent ID, so its subagents
use the current selection. Cursor, Pi and Devin also use it at every hook event.
OpenCode's `/clear` makes a new session ID and takes the current profiles.
The profiles can also differ from what you expect here:

- An agent started from another terminal (for example by Orca, Rex or Herdr) uses the stored selection
  unless started with `CTXTRA_PROFILES`.
- The MCP proxy uses the profiles of when it started: on resume they can be newer than the instructions',
  and after `/clear` it keeps the old servers until the session restarts.
- Muse, and IDE agents started from the Dock, don't pass your shell's variables to hooks: they see only the
  stored selection.
- Some agents keep the hook's text in their history and don't run the hook again on resume or compaction.
  Copilot can lose that text at compaction. Hermes's default in-place compaction keeps the session;
  its rotation mode has not been verified.

Codex passes `CTXTRA_PROFILES` to its MCP proxy, also when empty. After upgrading, run
`ctxtra setup --agent codex` again to update an older allow-list; `doctor` reports a missing variable.
Muse's proxy uses the stored selection, like its hooks.

Full rules: [spec.md](spec.md). Step-by-step cases: [scenarios.md](scenarios.md).

## Check

```bash
ctxtra context files [DIR]     # files and skills a session in DIR gets; errors on stderr
ctxtra context show [DIR]      # the exact text the agent gets
ctxtra context explain [DIR]   # every binding: matched or not, and why
ctxtra doctor [DIR]            # config errors, setup state, Codex trust, recent hook runs
```

They exit with 1 when a session wouldn't get everything it should, and take `--json` (except
`context show`). `doctor` checks every binding, also those that don't match DIR. It checks a session once for
each agent you set up, and each error names its agents, such as `(codex)`. `ctxtra help` lists every command; `ctxtra help COMMAND` shows one.

If you maintain a source, `ctxtra source validate DIR` checks `DIR/ctxtra-source.yaml` without reading
your config, trust records or credentials and without network access. It validates YAML, keys, names,
MCP definitions and the files that collections name, as sessions would read them from a commit. It exits 1
on errors and supports `--json`, so CI can run it.
Sources need no manifest when users bind files directly by path.


Every hook run is logged to `~/.local/state/ctxtra/log.jsonl`. Past 1 MiB, the log moves to `log.jsonl.1`, replacing the
older one; `doctor` reads both. The hook never blocks a session: on any error it adds nothing and logs the
error.

## Follow the agent to another folder

A session gets what its session folder matches when it starts. When you then ask the agent to also change
something in another repository ("update the client in ../billing too"), your bindings for that repository
don't reach the session. ctxtra has an optional skill for this:

```bash
ctxtra skill install ctxtra-follow
```

The session's text gives the exact command with its agent and profiles, for example
`ctxtra context show DIR --agent claude-code --profile iam --exclude-session SESSION_DIR --part 1`.
With no active profile, it starts with `CTXTRA_PROFILES=` so a later selection doesn't apply.
Before the agent works in a folder outside the session folder, the skill tells it to run that command,
once for each folder: the top of another repository, or a
project folder in a repository with several. That prints only the files and skills the folder adds, not
what the session has already (a file the session has from another folder is listed by name, so the agent
knows it applies here too), and the agent follows them for its work there. Each file's header names the
repository or folder it is for. A long text comes in outputs of at most 8,000 bytes, each saying how to get
the next, because agents cut long command output.

Unlike session start, this depends on the model: it decides when to use the skill, and it can miss it. The
agent gets only your own bound files, read with the same trust rules as at session start. A binding for a
folder below the one the agent ran the command for doesn't reach it. That includes folders under the
session folder: a binding for `~/dev/mono/billing/**` doesn't reach a session started in `~/dev/mono`.

## Skills as slash commands in Claude Code

`ctxtra setup --agent claude-code` writes a plugin named `ctxtra` to `~/.claude/skills/ctxtra/` (or
`$CLAUDE_CONFIG_DIR/skills/ctxtra/`). Claude Code loads a plugin from that folder as it is: nothing is
installed from a marketplace. When a session starts, the plugin asks ctxtra for the skills bound to the
session folder and adds a command for each. Type `/` to see them; their descriptions end with `(ctxtra)`.

`/deploy staging` gives Claude the skill's `SKILL.md` and starts a turn with your arguments. `$ARGUMENTS` in
the skill is replaced with them; without it, they are added at the end. Claude reads the skill's other files
as it reads any file.

- They are commands, not Claude's own skills. Claude's `Skill` tool can't load them, and Claude doesn't apply
  frontmatter such as `allowed-tools`, `context: fork` or `disable-model-invocation`. Claude still reads a
  skill's `SKILL.md` on its own when a task needs it, as in the other agents.
- Two bound skills with one name both reach the session. The first keeps `/release`, the next gets its source's
  name in front: `/obs-team-release`.
- A command that is already there wins: a built-in, your own skill or command, another plugin's. The bound
  skill with that name then has no command. Names other than letters, digits, `_` and `-` have none either.
- A session has the commands of the skills bound when it started. A skill bound later gets its command in
  the next session; `/clear` doesn't add it. A command reads its `SKILL.md` each time you run it, so a change
  to a skill applies at once.
- `claude -p` gets no commands: there a command that is the whole prompt ends the run. `/deploy staging`
  reaches Claude as text, which then finds the skill in ctxtra's list.
- An organization can stop such plugins (`strictPluginOnlyCustomization`, `strictKnownMarketplaces`). The
  hooks still give Claude the skill list.
- A folder `~/.claude/skills/ctxtra` that ctxtra didn't write, such as a skill of your own with that name, is
  left alone: setup says so, and Claude Code gets no commands.

`ctxtra setup --remove` removes the plugin. After an upgrade that added it, every ctxtra command reminds you
to run `ctxtra setup --agent claude-code` until you do.

## MCP servers

ctxtra gives your sessions MCP servers by folder, like instructions and skills, through one MCP server named
`ctxtra-proxy`. Setup supports Claude Code, Codex, Cursor, OpenCode, Pi, Muse and Antigravity.
Setup adds the proxy even when you have no servers yet. Servers you bind later reach the next session
without another setup. In Codex, a local server with a new `pass-env` variable needs
`ctxtra setup --agent codex` again: Codex passes the proxy only the variables setup listed. The session
message and `doctor` say so.
[spec.md](spec.md#mcp-servers) describes the format and limits.

For a server you declare yourself:

```bash
ctxtra mcp add linear https://mcp.linear.app/mcp --notes 'Choose the work workspace'
ctxtra binding add work-linear --dir '~/work/**' --mcp-server linear
```

Run add in your own terminal. It authorizes in your browser if needed; then start a new matching agent session.
If credentials already exist for the same identity and target, add reuses them without
authorizing again. It reports that reuse. If your definition declares secret names that have no stored value, add
asks for those values only and adds them to the shared credentials. It keeps the stored values and the sign-in.
Servers receive only the secrets their definitions declare. Add rejects `--client-secret` and
`--callback-port` in that case; add the definition without those flags, then use login to change the shared
credentials.
The definition, sign-in, binding and agent setup are separate. Adding another remote server needs its own sign-in
and binding, but no new native agent entry. The model sees each upstream tool and description, with a prefix such
as `linear__`, not a single opaque ctxtra tool.

For fixed remote headers and OAuth scopes, repeat `--header NAME=VALUE` and `--scope SCOPE`:

```bash
ctxtra mcp add internal https://mcp.example.com/mcp --header 'X-Team=core' --scope read --scope write --no-login
```

These values are written to the config. Use `--secret-header NAME` for a header whose value should be
stored encrypted. Quote values that contain spaces, punctuation or start with `!`.

A source declares servers in top-level `ctxtra-source.yaml`, with `schema: ctxtra/source/v1` and an
`mcp-servers` map. Your config selects a source's `linear` server as `work:linear`:

```bash
ctxtra binding add tracker --repo 'github.com/acme/**' --mcp-server work:linear
```

Sources do not define bindings or select other sources' servers.

A server of a repo source needs your approval in your terminal before ctxtra contacts or starts it, also one
without sign-in:

```bash
ctxtra mcp approve work:linear   # inspect and consent; connects to nothing
ctxtra mcp login work:linear    # authorize this identity if needed
```

Login offers that separate approval first if missing. Source-version acceptance, server approval, provider
consent and each agent's tool permissions are different decisions. A no-auth server needs approval, not a browser.
Config and dir-source definitions need no extra approval. Trust only live source content you would run as your own
configuration.

Use `mcp list`, `context explain` and `mcp status` to check selection, sign-in and approval. Tools still use the
short prefix `linear__`. If two identities with that prefix match one folder, the first binding wins and ctxtra
reports the other. It never falls back to the other account if the winner fails or lacks approval. To get both,
give one a new name in your config's `mcp-servers`:

```yaml
mcp-servers:
  jira_iam: {use: 'iam-team:jira'}   # its tools are jira_iam__TOOL
```

The new name applies wherever the server comes from: your bindings, your collections, and a source's collection
that adds it. `jira_iam` and `iam-team:jira` are then one server, served once. Its sign-in and approval stay with
`iam-team:jira`, so a new name needs no new approval. Two names for one server are an error.

Write that alias with:

```bash
ctxtra mcp add jira_iam --use iam-team:jira
```

This contacts no server and changes no sign-in or approval. Use `--dry-run` to preview it. The same
definition or alias added again changes nothing.
An identical collection or alias add succeeds even when its files or source server are no longer
available. Use `ctxtra doctor` to check the current content.

Review source updates carefully: commands, endpoints, inherited environment and targeting can change. Operational
changes need approval again; notes or an unrelated commit do not. Accepting wider targeting can give an already
approved unchanged server to more folders. `mcp revoke REF` withdraws local approval, not credentials or provider
grants. Close affected sessions to stop old connections; live revocation is not guaranteed.

Each agent asks before it runs an MCP tool, as for its own servers. Allowing `ctxtra-proxy` allows every server that
ctxtra binds to that folder, including servers bound later. A rule for a short tool prefix can match different
source identities or accounts in different folders. Check the full identity with `ctxtra mcp status DIR`.
Setup keeps existing native entries for the same provider, so both tool sets can appear:

- Claude Code: allow `mcp__ctxtra-proxy` in `permissions.allow`, or a pattern such as `mcp__ctxtra-proxy__linear_work__*`.
- Codex: add `default_tools_approval_mode = "approve"` to the `[mcp_servers.ctxtra-proxy]` table; setup keeps it.
- Antigravity: an allow rule `mcp(ctxtra-proxy/*)`.

ctxtra keeps the servers' secret values, such as an API token, encrypted in `~/.local/state/ctxtra/mcp`, with the key in
your OS keychain. Without a keychain, for example on a Linux server, the key is a file there, and ctxtra warns
that any process of yours can read the credentials.

For a server that uses OAuth, you sign in once, in your terminal, and every agent uses that sign-in:

```bash
ctxtra mcp login linear_work                # opens your browser
ctxtra mcp login linear_work --no-browser   # over SSH: open the link elsewhere, paste where it ends
ctxtra mcp logout linear_work               # deletes the sign-in on this machine
```

- Sessions never open a browser. When a sign-in expires or the server wants more permissions, a tool call
  says to run `ctxtra mcp login NAME`. A session that started before you signed in gets the tools within a few
  seconds; in Codex, start a new session.
- Two accounts of one provider, such as two Linear workspaces, are two servers with two names. Write in each
  server's notes which account it is. When you sign in to the second one, the provider may pick the account
  your browser is signed in to: use a private window.
- GitHub and Slack don't let ctxtra register itself. Create an OAuth app there with the redirect address
  `http://127.0.0.1:PORT/callback`, and add the server with `--client-id` (with `--client-secret` for Slack) and
  `--callback-port PORT`.
- Retry an interrupted authorization with `login REF`. If add already saved the definition, don't add it again.
  Use a fresh browser link; an old callback cannot complete the new attempt.
- Login can replace an existing sign-in. Cancelling or failing replacement keeps the old credentials, including
  when `--secrets` asks for new values. Start new sessions after switching accounts; login does not reset an agent's
  conversation or a provider's open session. Restart local servers after rotating environment secrets.
- A shorter configured scope list does not revoke old permissions. Provider access and agent tool rules are separate.
- Agent sessions share the sign-in for an identity, origin and target. Add cannot silently replace its
  credentials with another target; explicit login replacement asks first and affects new sessions using that identity.
  To replace after a refused add, declare the intended definition in your config, then run `login REF`.
  If the new endpoint needs neither sign-in nor secret values, login leaves the old credentials alone and reports
  how to delete the old sign-in with `logout REF`.
- `logout` doesn't tell the provider or withdraw connection approval. Remove the grant in the provider's settings
  to end it there. `mcp remove` cannot edit package definitions. Source removal keeps credentials and prints explicit
  cleanup commands. Removing credentials also cancels pending logins for that
  identity, including logins started by another process. A small, secret-free cancellation marker remains locally.
- On another machine, restore source content, approve repo-source servers there, sign in and run setup. Config and source
  copies do not transfer credentials or machine-local approval. Source names and origins are part of the credential binding;
  renaming a source or changing its origin requires a new sign-in, not copied refresh tokens.

What to keep in mind:

- Binding servers to folders prevents mistakes, such as a session using the wrong account. It doesn't
  protect anything from an agent that has a shell: it can use every server of your config.
- Any process of yours can read the stored credentials, as with the agents' own sign-ins.
- `ctxtra mcp show` prints a local server's error output, which can hold secrets; an agent that runs it
  reads it too.
- ctxtra's MCP proxy sees every tool call and its result, and passes them on. Its log has neither, only
  which servers started and how calls failed.
- Without a keychain, a backup of `~/.local/state/ctxtra/mcp` holds the key of your credentials too.
- A server under a certificate you trust yourself can use keys smaller than NIST's minimums. To refuse
  them, run ctxtra with `GODEBUG=fips140=on`: see [Key lengths](spec.md#key-lengths).
- ctxtra will later sign in as an app described by a file on its own domain, which authorization servers
  show you. Until then it registers itself with each authorization server.

When a session lacks a server's tools, `ctxtra mcp status` in that folder says why: the server needs sign-in,
failed, or was too slow for the agent's first tool list. It also shows how long the first tool list took, and
how much of that was ctxtra's own start. The session summary also names servers that need sign-in.

A session's servers are fixed when it starts; their tools can still change. ctxtra waits for slow servers only as long as each agent
waits for it: 1.5 seconds in Claude Code (later tools follow in its interactive mode), up to 8 in Pi, up to 10
in the others. One slow server holds back the first tool list of all of them until then. A server that starts
slower, such as one run with `npx`, can be missing from that session. In Codex, the
`ctxtra-proxy` server is required: if the ctxtra binary moves, Codex doesn't start until you run `ctxtra setup` again.
Logs are in `~/.local/state/ctxtra/log.jsonl`.

## Limits

- Each agent gets a limited amount of text in full: 360,000 characters in Claude Code and Muse, 1,000,000
  bytes in Codex, 650,000 bytes in Antigravity, 140,000 bytes in Copilot, 1,000,000 characters in the
  others (not measured in Cursor and Devin). Copilot counts tokens, and a byte is at most one token in any
  text, so bytes are a safe limit there; it costs English text, which would fit about four times more.
  Non-Latin text counts more in Codex, Antigravity and Copilot: a CJK character is 3 bytes; an emoji counts 2
  characters in Claude Code and Muse. Muse's hooks count bytes as they are in the hook's JSON answer, so
  Muse fits less non-ASCII text, and less text with many quotes or backslashes. Above the limit,
  whole files are dropped from the end, as with `max-ctx-chars`; the message at session start, the log,
  `context explain` and `doctor` name them. Several agents cut or drop a large hook output, so ctxtra splits the
  text into labeled parts there: 40 hooks per event in Claude Code and Muse, a plugin callback per part in
  Hermes, an injected step per part in Antigravity. See [the spec](spec.md#capacity).
- Except in Pi and OpenCode, bound skills are a list in the session text, not the agent's own skills. The
  agent reads a skill's `SKILL.md` at its listed location with its file tool when a task needs it or an
  instruction names it. This also applies to skills listed by `ctxtra-follow`: the agent's skill tool and
  slash commands can't load them. Frontmatter keys other than `name` and `description` have no effect.
  Claude Code also offers the bound skills as slash commands, which you run yourself (see below).
- Codex gets the text in full only from version 0.145.0, which reads the hook's `additionalContextLimit`;
  older versions show the model about 10,000 characters, and `doctor` warns.
- Resumed sessions don't get the text again, because the transcript already has it. `/compact` and
  `/clear` add it again in Claude and Codex. Copilot is different: it doesn't keep the text in the session,
  so its hook adds it again on resume. When Copilot compacts a long conversation, the text is gone for the
  rest of the session: Copilot doesn't run the hook again. Muse and OpenCode differ too, see below.
- A subagent gets the files when it starts, so it can see a newer version than its parent.
- Codex asks you to trust a hook again after every change to it, for example after the binary moved.
  `doctor` shows whether a trust record exists, but can't tell whether it is for the current hook.
- Muse Code (tested with 1.4.10) clears the environment for hooks and MCP servers. Setup records the four
  XDG folder values in both entries. After changing one, run `ctxtra setup --agent muse`; `doctor` reports
  stale values. Hooks use the log in the state folder. The MCP entry also copies `CTXTRA_LOG` at setup time.
  Setup refreshes an existing proxy even if the new data folder has no downloaded sources yet.
  Muse cannot inherit shell proxy or certificate settings, a Linux keychain session, or local servers'
  `pass-env` values. Use explicit non-secret `env` values or stored `secret-env` values for local servers.
  Start a new Muse session after signing in to an MCP server. A resumed session runs the hook again and
  replaces the earlier text with the current files.
- Devin: the hook goes into `~/.config/devin/config.json`, which Devin CLI and Devin Desktop's agent both
  read. Devin has no subagent hook. It allows comments in that file; `setup` leaves such a file alone. Devin
  also runs the hooks in `~/.claude/settings.json`: with both set up, Claude's hook adds nothing in Devin
  sessions (`doctor` shows it as skipped). Checked by the CLI tests only, not with the real agent, which
  needs a Devin login. Windsurf's old Cascade agent isn't supported: its hooks can't add context.
- Cursor: the Agent CLI waits for the session-start hook: with 800,000 characters, every codeword that
  the [test kit](../e2e/cursorkit) samples from the text reached the model (tested with 2026.10.12). That
  shows the text arrived at those places, not that every line did. The desktop app (tested with 3.25)
  starts the hooks when the first message is sent and doesn't wait for them: text from hooks that end too
  late is lost for that message. So far every sampled codeword of 30,000 characters arrived, and all of
  100,000 characters was lost. It also runs the hooks in `~/.claude/settings.json`: with
  ctxtra's Cursor hook set up, Claude's hook adds nothing there, at once (`doctor` shows it as skipped).
- OpenCode: only OpenCode 2 (tested with 2.0.29); version 1 plugins have another format. OpenCode adds the
  text to every model request, not to the transcript, so resumed sessions get it too. Its background
  service keeps the plugin loaded: when the bound skills change, they apply from the second model request
  of the next session, or right away after `opencode service restart`.
- Antigravity: tested with the CLI (1.4.7); the App and IDE read the same hook file but aren't tested. A
  subagent is a session of its own there, so it gets the files from the same hook. Antigravity asks before
  it reads a file outside the workspace, such as a bound skill's `SKILL.md`, and headless runs (`agy -p`)
  refuse it. To skip the question, allow your skill folders, not everything, in
  `~/.gemini/antigravity-cli/settings.json`: `"permissions": {"allow": ["read_file(/Users/me/agent-context/skills)"]}`.
- Hermes: the plugin reads the session folder with `agent.runtime_cwd.resolve_agent_cwd()`, the way Hermes
  tells plugin authors to. With `plugins.isolation: host`, plugins run in another process, which may not
  know the session folder: then the plugin uses that process's folder, and folder and repository bindings
  can match the wrong place. The e2e tests pass with Hermes `v2026.9.24`, and with `main` at `bebe448`, which runs
  every session in one gateway.
- DSH: tested with 0.2.1-rc.2 (and 0.2.0-rc.2, the npm `latest`). The plugin uses DSH's plugin API, which may still
  change before 1.0. DSH adds the text after the first prompt and the repo's `AGENTS.md`; `/clear` and
  compaction add it again.
