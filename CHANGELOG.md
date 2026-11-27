# Changelog

## Unreleased

### Breaking changes

This release starts over. It reads nothing that v0.6 and earlier wrote. To move:

1. Write the config again in the new format, `schema: ctxtra/config/v1`, in `~/.config/ctxtra/config.yaml`
   (`ctxtra help config`). The `ctxtra/v1` format is not read or converted.
2. Add each git source again with `ctxtra source add --repo URL`, so its accepted commit goes into
   `ctxtra.lock` next to the config.
3. Run `ctxtra setup` for each agent. Earlier hooks and MCP proxy entries add nothing; setup replaces them,
   and `ctxtra doctor` lists the ones left.
4. Trust live folders, approve source servers and sign in to MCP servers again: the new state folder starts
   empty. Then delete `~/.ctxtra`.

What changed:

- Files follow the XDG folders on Linux and macOS: config in `~/.config/ctxtra`, data in
  `~/.local/share/ctxtra`, state in `~/.local/state/ctxtra` and clones in `~/.cache/ctxtra`. `~/.ctxtra` is
  not read. `CTXTRA_CONFIG` and `--config` are gone.
- Bindings are only in the config, as a list with unique names. `include`, binding files, `match` and
  `global` are gone: a binding has `location` (`dirs`, `repos`, `repo-sets`), `profiles` and `agents`, and
  one without `location` applies everywhere. Entries that start with `!` must be quoted.
- Sources are `repo` (with `branch`, `tag` or `rev`) or `dir`. Accepted commits are in `ctxtra.lock`, not in
  the config: `commit:` is gone. Source servers move from `mcp-servers.yaml` to the source's
  `ctxtra-source.yaml`. Files of a source are `SOURCE:PATH`, which replaces `ctxtra://SOURCE/PATH`.
- A local repo source (`repo: ~/dev/team-ctx`, formerly `local-git`) follows a named branch, by default the
  clone's checked-out branch, not the clone's committed `HEAD`.
- Agent IDs `claude`, `copilot` and `hermes` are now `claude-code`, `github-copilot` and `hermes-agent`, with
  no aliases.
- Renamed config keys: `check-updates` at the top is `check-releases`, `budget.max-chars` is
  `max-ctx-chars`, `summary-message` is `session-message` (without `show-skills` and `show-instructions`:
  0 leaves a list out), and a server's `env-pass` is `pass-env`.
- Renamed commands: `source add git URL` and `source add trusted-path DIR` are `source add --repo URL` and
  `source add --dir DIR`, `source update --check` is `source update --dry-run`, `source check` is
  `source validate`, `source restore` is `source download`, `binding add --mcp` is `--mcp-server`, and
  `mcp add --env-pass` is `--pass-env`. `source add local-git DIR` is `source add --repo DIR`.
  `source add git DIR --remote REMOTE` is gone: give the remote's address, `source add --repo URL`.
  `binding add --global` is gone.
- `source trust NAME` records the remotes of an existing `dir` source, so that it is read live. It no longer
  turns a pinned source into a live one, and takes no DIR: to read a clone live, add it with
  `source add --dir DIR`.
- Every mistake is an error that `doctor` reports with exit 1, and the part with the mistake is left out.
  A YAML tag, an anchor, a duplicate key, or a number where text is expected (`tag: 1.10`) is an error.

### Changes

- **Profiles.** `CTXTRA_PROFILES=iam,oncall` sets the active profiles for one launch; otherwise the selection
  that `ctxtra profile set NAME...` stores applies (`profile clear` empties it, `profile list` shows every
  profile the bindings use). A binding with `profiles` applies only while they are active, and `!` entries
  exclude one. A session keeps its profiles for its subagents and compaction; `/clear` takes them anew.
  Copilot, Muse, OpenCode, Hermes and DSH keep them for subagents when the agent gives the parent ID.
  Antigravity gives none and uses the current selection. Codex passes `CTXTRA_PROFILES` to its MCP proxy.
  The session message, the log, `context explain`, `doctor` and `mcp status` show the active profiles and
  where they came from, and a name no binding uses is reported as a likely typo.
- **Agents.** A binding with `agents` applies only to those agents. `context` commands and `mcp status` take
  `--agent` and `--profile` to preview another session. `doctor` checks a session once for each agent that
  setup set up, and names the agents that have each error.
- **Sources.** A `repo` source follows a `branch`, a `tag` (a moved tag is reviewed) or a `rev`, and
  `--subdir` makes one folder of a repository the source's top. `source add` names the source after the
  repository and folder, checks the size limits before writing, and lists the manifest's collections and repo
  sets with a `binding add` line that uses them. `source download` fetches exactly the accepted commits, for
  example on a new machine. A `dir` source in a repository with remotes is read only after
  `ctxtra source trust NAME`, and stops when its remotes change. Repo sources leave symlinks out: a symlink that
  a binding names or matches is an error. Removing a source names its leftover MCP credentials.
- **Source manifests.** A source can have `ctxtra-source.yaml` with MCP servers, collections and repo sets.
  Collections are named sets of instructions, skills, servers and nested collections, which a binding adds by
  name (`collections: ['team:essentials']`), with `!` entries to leave parts out. Repo sets are named lists of
  repositories, which a binding follows in `location.repo-sets`. The config can have its own collections and
  repo sets too. `source validate DIR` checks a manifest and the files it names, as a session reads them.
- **Review.** `source update` reviews a new commit in the terminal: changed files with content warnings,
  collections in use that it removes or grows, repo set entries it adds or removes (with a warning for
  `HOST/**`), new servers and their definitions, and new errors with the bindings they affect. `--dry-run`
  shows the review and writes only to the cache; `--json` lists the new errors in `diagnostics`.
- **MCP servers.** The config's `mcp-servers` can give a source's server another name,
  `jira_iam: {use: 'iam-team:jira'}`, keeping its approval and sign-in. A source's server needs approval on
  this machine, again after its definition changes; config and `dir` source servers don't. An unapproved
  server is an error with its approve command everywhere it is reported, and one that may need sign-in names
  `ctxtra mcp login ID`. In Codex, a server that needs a new `pass-env` variable says to run setup again.
  `mcp add` takes `--header NAME=VALUE`, `--scope SCOPE` and `--use SOURCE:SERVER`.
- **Errors.** Every mistake is an error, and only the smallest part with it is left out. The session message
  names the first three errors, with their line in the config or the command that fixes them, and counts the
  rest; each is one line of at most 240 characters. Going over an agent's capacity or `max-ctx-chars` is an
  error that names the bindings of the dropped files. Two skills or servers with one name are notices: both
  skills reach the session, and the first server in binding order is served. `mcp status` exits 1 when a
  session in the folder wouldn't get everything it should.
- **Commands for the whole config.** `binding add` writes every key: `--dir`, `--repo` with `--subdir`,
  `--repo-set`, `--profile`, `--agent`, `--collection`, `--instruction`, `--skill` and `--mcp-server`, or any
  binding in YAML with `--file`. `--source` makes them refer to one source, by name or address, so a team's
  README works whatever you named it. `--replace` changes a binding in its place, or adds it, and
  `--before`/`--after` place a new one. It shows the binding as written, where it applies, what a session
  there gets, and content warnings for repo source files it binds for the first time. `collection` and
  `repo-set` have `add`, `remove` and `list`; `config set` and `config unset` change program settings. Every
  `list` has `--json`, and commands exit 0, 1 or 2. Commands that write the config keep every byte they don't
  change, comments included, and refuse a change they can't make that way, or one that would make the config
  or lock larger than 4 MiB.
- **File headers.** A file's header in the session says which sessions get it: every session, sessions with a
  profile or agent, this repository and its worktrees, or the session folder.
- **Setup.** Setup always adds the MCP proxy for Claude Code, Codex, Cursor, OpenCode, Pi, Muse and
  Antigravity, so servers bound later need no setup. Hooks an earlier ctxtra wrote are one notice per file,
  with their count. `doctor` reports older OpenCode, Hermes and DSH plugins that don't pass session IDs.
- **ctxtra's own skills.** `skill install` puts ctxtra's skills into one binding, `ctxtra-system`, with a comment
  that says to keep it. A refused or failed install changes neither the config nor the skill's files.
  Sessions with `ctxtra-follow` give the exact command for another folder, with the session's agent and
  profiles.

## v0.6.0

- MCP servers are available without `CTXTRA_EXPERIMENTAL_MCP=1`. Commands, setup and sessions accept MCP
  configuration by default. Existing experimental setups keep working; approval and sign-in rules are unchanged.

- Muse supports MCP servers. Run `ctxtra setup --agent muse` to add the entry. Muse hooks and its proxy use
  the config path recorded by setup when `CTXTRA_CONFIG` is absent. Background checks use that config too.
  Muse still cannot inherit shell proxy, certificate or Linux keychain settings, or local servers' `env-pass`
  values.

- MCP sign-in instructions describe the state at session start, so they do not claim tools are still missing
  after a successful sign-in during the session.

## v0.5.3

### Experimental

- After an MCP sign-in, the browser shows a clear page that says whether the sign-in succeeded and to go
  back to the terminal, in light and dark mode.

- The MCP entry that setup adds to agents is now named `ctxtra-proxy`, not `ctxtra`. Tool names and
  permission rules change with it: for example `mcp__ctxtra-proxy__linear_work__list_issues` in Claude Code,
  `[mcp_servers.ctxtra-proxy]` in Codex, and `mcp(ctxtra-proxy/*)` in Antigravity. An entry named `ctxtra` that
  doesn't run `ctxtra mcp proxy` is now left alone.

  **Upgrade:** run `CTXTRA_EXPERIMENTAL_MCP=1 ctxtra setup`. It renames the old entry and keeps the keys you
  added to it. Until then, the old entry keeps working, and the setup notices ask you to run setup. Update
  allow rules that name `ctxtra`.

- The `MCP servers:` list in session summaries is no longer grouped by source. A server from a source is
  shown as `SOURCE/NAME`, so the group added nothing. Servers of your config were grouped as `local`, which
  read like local command servers.

## v0.5.2

- Pinned sources check for new commits by default. Set `check-updates: false` on a source, or pass
  `--check-updates=false` when adding it, to turn checks off. Source and ctxtra release checks run in
  the background at most once an hour. Checks never accept commits or change session content.

- The guide documents how to refuse certificate keys below NIST's minimum lengths with
  `GODEBUG=fips140=on`. See [Key lengths](docs/spec.md#key-lengths).

### Experimental

- Experimental MCP servers can come from a source's root `mcp-servers.yaml`. Bind them as
  `SOURCE/NAME`. Pinned definitions need separate approval with `ctxtra mcp approve` before ctxtra
  connects or starts a command. Operational changes need approval again; `mcp revoke` withdraws it
  for future connections. Config and trusted-path definitions need no extra approval.

- Adding an experimental MCP server reuses an existing sign-in for the same identity and target across
  configs. It asks only for missing secret values. Replacing a shared sign-in requires explicit login;
  cancelling or failing replacement keeps the old credentials. Removing a source keeps credentials
  and prints cleanup commands, since another config may still use them.

- With experimental MCP enabled, session summaries count and list selected MCP servers, grouped by
  source or `local`, including servers awaiting sign-in or approval.

- `ctxtra mcp status` shows the actual time of the first tool list, ctxtra's own startup time, and
  credential-read times, separately from the agent's deadline.

## v0.5.1

v0.5.0 was tagged, but its release failed: v0.5.1 is the first release with these changes.

- Claude Code offers the bound skills as slash commands: type `/` in a session to see them, marked
  `(ctxtra)`. `ctxtra setup --agent claude` writes a small plugin to `~/.claude/skills/ctxtra/` that adds
  them. They are commands, not Claude's own skills: Claude's `Skill` tool can't load them, and their
  frontmatter has no effect. See [the guide](docs/guide.md#skills-as-slash-commands-in-claude-code).

  **Upgrade:** run `ctxtra setup --agent claude` to add the plugin. Until then, ctxtra commands remind you.

- A new Homebrew formula, `atelpis/tap/ctxtra-rc`, gets release candidates before each release, and every
  release. It installs the same `ctxtra` command. Its update notices include release candidates, with
  `brew upgrade atelpis/tap/ctxtra-rc`. `atelpis/tap/ctxtra` stays on releases.

- The release archives include `THIRD_PARTY_NOTICES.md`, with the licenses of the software that ctxtra
  includes.

- Building from source requires Go 1.27.4 or newer, which fixes a `net/http` server crash.
  Crypto dependencies were updated to address security advisories.

### Experimental

- Experimental MCP support gives sessions local command servers and remote HTTP servers by folder,
  through one `ctxtra` MCP entry. Enable it with `CTXTRA_EXPERIMENTAL_MCP=1` for commands, setup and
  agents. Use `ctxtra mcp add`, `list`, `show` and `remove` to manage servers. The format is experimental.

  **Upgrade:** to enable MCP, run `CTXTRA_EXPERIMENTAL_MCP=1 ctxtra setup` to add the agent entry
  (and trust the hooks again in Codex). Muse is not supported.

- Remote MCP servers support OAuth sign-in with `ctxtra mcp login` and `logout`, including sign-in
  over SSH with `--no-browser`. Agents share the sign-in and never open a browser themselves. Secrets
  are encrypted locally, with the key in the OS keychain or, when unavailable, in a local file with a
  warning. Any process running as you can read the credentials.

- `ctxtra mcp status` explains why a session lacks a server's tools: sign-in is needed, the server
  failed, or it missed the first tool list. Session notices also name servers needing sign-in.

## v0.4.1

- ctxtra's source repository is now `github.com/ctxtra/ctxtra`. The Homebrew tap stays at
  `atelpis/tap/ctxtra`.

**Upgrade:** If you installed with Go, run `go install github.com/ctxtra/ctxtra/cmd/ctxtra@latest`
once to switch to the new module path. Older binaries may still suggest the old install command.
Homebrew users can keep using `brew upgrade atelpis/tap/ctxtra`.

## v0.4.0

- The session summary groups instructions and skills by source. A file from a source is shown by its path
  in the source, such as `global/style.md`; a file in no source is in the `local` group, by its full path.
  The skills that `ctxtra skill install` adds are in the `ctxtra` group.

- Each group shows up to 30 entries, then `+N more` and a line that points to `ctxtra context files`.
  Before, the limit was 15 for each whole list. The new `summary-message` section of the config hides a
  list (`show-instructions`, `show-skills`) or changes its limit (`max-instructions`, `max-skills`); a limit
  of `0` shows only the number of entries of each source.

## v0.3.0

- Sessions tell you about a new ctxtra release, with the command that upgrades it. Every 4 hours at most, a
  session asks GitHub for the latest release in the background, and the next session shows the result; it
  never waits for the check. This sends your IP address and ctxtra version to GitHub. `check-updates: false`
  in the config turns it off, and `CTXTRA_NO_UPDATE_CHECK=1` stops it in one environment, such as CI.

- A pinned source with `check-updates: true` (or added with `source add --check-updates`) gets the same
  check: `git ls-remote` of its branch, which reads only the commit ID and fetches no files. Sessions
  then say when the branch has new commits, with the `source update` command that reviews them. The check
  uses your git credentials, so leave it off for a source whose ssh key needs a touch or an approval.
  `ctxtra check-updates` runs the checks now, and `doctor` shows what they found or why they failed.

- The "last checked N days ago" and "not checked for new commits on this machine yet" notices are gone.
  Turn on `check-updates` for a source to hear about its new commits. `doctor` says "last fetched"
  instead of "last checked".

- Session notices come after an empty line, each marked with `ℹ`, so they no longer look like part of the
  skills list. The command to run is on its own line, and notices no longer show commit IDs (`doctor`
  still does).

- `source add` and `source update` show content warnings: hidden characters, HTML comments, long encoded
  blocks, `curl | sh` and similar commands, secret paths like `~/.ssh` and `.env`, new URLs, and new
  executable files. `source update` checks only the added lines of the files it shows in full, and shows
  each warning after its line; `source add` checks the files your config uses from the new source.
  `source update --check --json` lists them in `diagnostics`. They only warn, and they don't show that a
  source is safe.

- `ctxtra source check DIR` checks a source repository's binding files in a checkout, the way sessions read
  them from a pinned source: broken YAML and bindings, config-only keys, paths outside the source, missing
  files, unusable skills, and symlinks and submodules that a pinned source leaves out. It reads only DIR,
  runs and fetches nothing, and exits with 1 on an error, so it fits CI. A glob that matches nothing is a
  warning. It doesn't check what the files say. `--json` is supported.

- A `ctxtra://` reference to another source in a pinned source's binding file now says that a binding
  file can't reference other sources, also when you don't have that source.

## v0.2.3

- `doctor` also reads the rotated hook log, `log.jsonl.1`, so it no longer shows "none yet" for runs logged
  before the log was rotated.

- The session-start summary and the size warnings of `explain` and `doctor` show files with the same name by
  full path: from `~` for local files, as `ctxtra://<source>/<path>` for files from a pinned source. Other
  files keep their short name. The count of problems now comes before the dropped files in the summary, so
  long paths can't hide it in Muse.

- Setup and config edits keep only the latest three backups per original file, pruning after a successful
  change. Cleanup errors leave extra copies without failing the config change. An unchanged latest
  backup is reused. New backup names identify the full original path, so files with matching names in different
  folders do not share retention. Import backups are unchanged. Older config backups cannot be safely
  matched to their original paths and are left for manual cleanup; they may contain tokens.

- `ctxtra-follow` tells agents to read bound skills at their listed locations, also when an instruction
  names one, instead of trying their native skill tool. The `ctxtra` skill now covers dropped files in
  troubleshooting and lists `source show` and `source update --check`.

- `doctor` checks every binding, also those that don't match its folder, and exits with 1 when one has a
  missing file or another problem.

## v0.2.2

- `doctor` and the `context` commands exit with 1 when `budget.max-chars` drops a file, as the docs say.
  `context show` also names the dropped files on stderr.

## v0.2.1

- The list of bound skills tells the agent that its skill tool can't load them, so it reads the `SKILL.md`
  instead. Before, an instruction that named a bound skill could make Claude Code call its Skill tool first
  and get "Unknown skill".

## v0.2.0

- An entry in `instructions` or `skills` that starts with `!` leaves out the paths it matches, for
  example `'!agents/drafts/**'`. ctxtra 0.1.0 reads such an entry as a path that matches nothing and adds
  the files anyway: upgrade before you use it.
- Building from source requires Go 1.27.3 or newer, which fixes a certificate validation vulnerability.
- Release downloads include a signed attestation bundle. [RELEASING.md](RELEASING.md) shows how to verify it.

## v0.1.0

The first release. ctxtra adds your instruction files and skills to coding-agent sessions, chosen by folder
and git remote, without changes to the repositories you work in.

- Works with Claude Code, Codex CLI, GitHub Copilot CLI, Cursor, OpenCode, Pi, Antigravity, Devin, Hermes
  Agent, Muse Code and DeepSeek Harness, on macOS and Linux.
- `ctxtra onboard` sets up the hooks, offers the ctxtra skill, and can import your agents' own
  instruction files.
- Team packages: a git repository pinned to the commit you accepted. `ctxtra source update` shows the diff
  before a new commit reaches your sessions.
- `ctxtra context show` prints what a session gets, and `ctxtra context explain` says why.

Install with `brew install atelpis/tap/ctxtra` or `go install github.com/atelpis/ctxtra/cmd/ctxtra@latest`.
