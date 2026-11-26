# Use cases

These examples use `schema: ctxtra/config/v1`. Run `ctxtra setup` first, then start a new agent session
after changing bindings. `ctxtra context explain DIR` shows what applies in a folder and why.

## Your instructions everywhere

Keep your instruction files outside repositories other people can change. A binding without conditions
applies to every session:

```bash
ctxtra binding add me --instruction ~/agent-context/me.md
ctxtra context files
ctxtra context show
```

```yaml
schema: ctxtra/config/v1
bindings:
  - name: me
    instructions: [~/agent-context/me.md]
```

## Your dotfiles, live

```bash
ctxtra source add --dir ~/dev/dotfiles
ctxtra source trust dotfiles
ctxtra binding add personal --instruction dotfiles:agents/me.md --skill 'dotfiles:skills/*'
```

A dir source with no remotes needs no trust step. With remotes, trust records them on this machine.
Edits and pulls apply immediately to new sessions. If the remotes change, live reading stops until you
trust it again. Use this only for files whose live changes you accept. Trust does not transfer to another clone.

## Instructions for your work repositories

```bash
ctxtra binding add work --repo 'github.com/acme/**' --instruction ~/agent-context/work.md
ctxtra binding add local --dir '~/dev/work/**' --skill ~/skills/deploy
ctxtra context explain ~/dev/work/billing
```

Repository conditions work in every clone and worktree, using normalized remote addresses.
Folder conditions also check the main checkout, so a worktree outside your work folder still gets the files.
With both dirs and repos in one location, either can match.

To exclude a place, quote a `!` entry in each binding that could reach it:

```yaml
bindings:
  - name: work
    location:
      dirs: ['~/work/**', '!~/work/clients/**']
    instructions: [~/agent-context/work.md]
```

An exclusion affects only its own list. A repository condition can still match under an excluded dir.
There is no config-wide exclusion.

## A team repository, reviewed

Read the repository before adding it. The first version is accepted without review:

```bash
ctxtra source add --repo git@github.com:acme/team-ctx.git
ctxtra binding add billing --repo github.com/acme/billing --instruction team-ctx:services-overview.md --skill team-ctx:skills/deploy
ctxtra source show team-ctx
ctxtra source update --dry-run team-ctx
```

The source provides files; your config chooses where they apply. The source cannot supply bindings.
Your sessions read the accepted commit in `ctxtra.lock`, not the newest one. A background check can tell
you there are new commits but never accepts them.

Run the review in your terminal to accept a version:

```bash
ctxtra source update team-ctx
```

The review shows used files and every file of used skills. Other changes are listed by filename.
`ctxtra source update --full team-ctx` shows those too. Content warnings point out hidden characters,
download-and-run commands, secret paths and new URLs. They cannot prove a file is safe.
Answering no keeps the old commit and remembers the dismissal. An empty answer accepts nothing.

## Use only part of a source

Bind the paths you want. Instruction and skill lists support globs and quoted exclusions:

```bash
ctxtra binding add team --repo 'github.com/acme/**' --instruction 'team-ctx:docs/*.md' --instruction '!team-ctx:docs/legacy.md'
```

Every path is relative to the source's top. A source reference cannot leave it.
A plain disk path remains a disk path, even when a repo source points at the same local clone.
Missing files and unmatched patterns are errors. Other entries still apply.

## Try local commits or live edits

For committed changes on your clone's checked-out branch:

```bash
ctxtra source add --repo ~/dev/team-ctx --name preview
ctxtra binding add preview --instruction preview:services-overview.md
ctxtra source update preview
```

Dirty edits do not apply. It follows the local named branch, not its upstream.
For live edits instead:

```bash
ctxtra source add --dir ~/dev/team-ctx --name live
ctxtra source trust live
ctxtra binding add live-preview --instruction live:services-overview.md
```

These are separate sources. Reviewed references stay pinned while live references read disk.
Remove a temporary live binding before pulling changes you have not reviewed.

## Another machine

Copy the config and its `ctxtra.lock`, then download accepted commits:

```bash
ctxtra source download
ctxtra doctor
```

Download asks nothing and never advances the lock. Local source folders missing on this machine add
nothing without an error. Run setup for the installed agents; trust live remotes, approve source servers
and sign in on this machine. Those machine-local decisions do not travel with the config.

## Remove content

```bash
ctxtra binding list
ctxtra binding remove billing
ctxtra source remove team-ctx --dry-run
ctxtra source remove team-ctx
```

Removing a source removes its lock entry and reports bindings that still reference it.
It does not remove those bindings or source credentials. Use the cleanup commands it prints.
`ctxtra setup --remove` removes ctxtra's agent setup while keeping other hooks and settings.

## Maintain a source

A source can be only Markdown and skill folders. Its optional `ctxtra-source.yaml` declares MCP servers:

```yaml
schema: ctxtra/source/v1
mcp-servers:
  jira:
    url: https://mcp.atlassian.com/v1/mcp
    notes: Choose the work account
```

```bash
ctxtra source validate .
ctxtra source validate . --json
```

Validation reads that one manifest without user config, credentials or network. It checks YAML, names,
entry shapes and servers. Collection file checks are not implemented yet. Collections, repo sets, tags,
revisions and source subdirectories are also not usable yet; bind branch sources by direct paths.

## Agent-specific content

```yaml
bindings:
  - name: review
    agents: [claude-code, codex]
    skills: [~/skills/review]
```

```bash
ctxtra context explain --agent codex
ctxtra mcp status --agent codex
```

All present conditions must hold. Without an agent argument, previews do not apply agent conditions.
Profile conditions parse, but selection and profile commands are not available yet.
