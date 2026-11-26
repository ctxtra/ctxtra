# Changelog

## Unreleased

- `binding add` writes every key of a binding: `--repo-set`, `--profile`, `--agent` and `--collection` next to
  `--dir`, `--repo`, `--instruction`, `--skill` and `--mcp-server`, and `--subdir` for the one `--repo` given.
  `--source` takes a source name or a repository address (`URL//FOLDER` for a source's folder) and makes the
  content and repo set values refer to that source, so a team's README works whatever you named it.
  `--file FILE|-` takes any binding in YAML, `--replace` changes one in its place or adds it, and `--before` and
  `--after` place a new one. The command shows the binding as written, where it applies, what a session there
  gets, and content warnings for files of a repo source that it binds for the first time.
- `source add` lists the manifest's collections and repo sets, and prints one `binding add` line that uses them.
- Commands that write the config refuse a config with lone carriage-return line breaks, and a name that YAML
  would write as an explicit key, instead of writing what they can't edit again.
- Commands that write the config refuse a change that would make it or the lock larger than 4 MiB, which
  would leave sessions with nothing from the config.
- Profiles: `CTXTRA_PROFILES=iam,oncall` sets the active profiles for one launch, else the selection stored
  in `profiles.json` in the state folder applies. A session keeps the profiles it started with for its
  subagents and compaction in Claude Code and Codex; `/clear` takes them anew. The session message, the log,
  `context explain`, `doctor` and `mcp status` show the active profiles and where they came from; a name no
  binding uses is reported as a likely typo. `context` commands take `--profile NAME` to preview others.
- Agent IDs are now `claude-code`, `github-copilot` and `hermes-agent`; the earlier names are not aliases.
  Run `ctxtra setup` again after upgrading. This release starts the setup contract over: earlier hooks and
  proxies add nothing. Setup replaces them, and `doctor` lists each repair command. Once every agent is
  set up again, `~/.ctxtra` can be deleted.
- Setup always adds the MCP proxy for Claude Code, Codex, Cursor, OpenCode, Pi, Muse and Antigravity,
  even without servers. Servers bound later reach the next session without another setup.

- Files now follow XDG folders on Linux and macOS: config in `~/.config/ctxtra`, accepted source content
  and installed skills in `~/.local/share/ctxtra`, local state in `~/.local/state/ctxtra`, and git clones
  in `~/.cache/ctxtra`. Only absolute `XDG_*` values override these defaults. `doctor` shows the folders.
- The config is always `config.yaml` in the config folder. `CTXTRA_CONFIG` and `--config` are removed.
- Codex passes all four XDG variables to its MCP proxy; Muse records
  them in hook and proxy entries. `doctor` reports changed Muse values with the setup command to refresh them.
  Setup refreshes existing proxy entries even when the selected data folder has no source catalogs yet.
- A new config format, `schema: ctxtra/config/v1`. The `ctxtra/v1` format is not read: write the config again.
  Bindings are only in the config: `include` and binding files are gone. A binding has `location` (`dirs`,
  `repos`) instead of `match`; without `location` it applies to every session, so `global` is gone. Files and
  servers of a source are `SOURCE:PATH` and `SOURCE:SERVER`. A binding with `agents` applies only to those
  agents; `context` commands and `mcp status` take `--agent`.
- Sources are `repo` (a git repository, also your clone, at a branch) or `dir` (a folder read live). Accepted
  commits are in `ctxtra.lock` next to the config, not in the config. `source download` downloads exactly those
  commits, for example on a new machine. A `dir` source in a repository with remotes is read only after
  `ctxtra source trust NAME`, and stops when the remotes change.
- A source declares its MCP servers in `ctxtra-source.yaml`; `source validate DIR` checks it.
- Source removal names leftover MCP credentials and their cleanup commands. Binding errors name files that
  would exceed `max-ctx-chars`. Skill installation refuses a conflicting binding before writing the skill,
  including in a dry run.
- Renamed: `source add --repo|--dir`, `source update --dry-run`, `source validate`, `binding add --mcp-server`,
  `mcp add --pass-env`, `pass-env`, `max-ctx-chars`, `check-releases`, `session-message` (0 leaves a list out).
- Mistakes are errors, reported in the session message, the log, `context explain` and `doctor`; the part
  with the mistake is left out. Two skills or MCP servers with one name are notices: both skills reach the
  session, and the first server in binding order is served.
- A repo source follows a `branch`, a `tag` or a `rev`. A moved tag is reported and reviewed; a `rev` stays
  until you edit it. `source add` takes `--tag`, and `--rev` with any revision, which it writes as the full ID.
- `subdir` makes a folder of a repository the source's top, with `source add --subdir`. Commits that change
  only other folders are nothing new. Another `repo` or `subdir` is reviewed as a new source.
- `source add` names a source after the repository and, with `--subdir`, the folder. It checks the size limits
  for the whole repository before it writes anything.
- `source update --dry-run` writes only to the cache. The review reads commits from the cache, and also
  shows a newly matched skill whose `SKILL.md` is invalid, and a `!` entry that a commit leaves matching nothing.
- Live-folder trust notices give the trust command and the option to use reviewed commits. `source trust`
  explains that files, servers and repo sets apply without review. Trust checks include every URL of a
  remote. Repo sources refuse symlinks in explicit paths and manifests as well as in patterns.
- Trust records include push URLs as well as fetch URLs. File reads and displayed paths use the destination
  that passed the trust check.
- Collections: named sets of instructions, skills, MCP servers and nested collections, in a source's
  `ctxtra-source.yaml` or the config. A binding adds `SOURCE:NAME` or the config's own by name, and its `!`
  entries remove what its collections add. Cycles, missing collections and paths that match nothing are errors.
- `source validate DIR` checks the files that collections name, as a session reads them from a commit.
- `source add --repo` shows content warnings for the files that the manifest's collections offer.
- `source update` reports collections in use that a commit removes, and the servers, skills and files it
  newly adds to them, with the bindings that use them.
- Repo sets: named lists of repositories, in a source's `ctxtra-source.yaml` or the config. A binding links
  to one in `location.repo-sets`, as `SOURCE:SET` or the config's own by name, and follows the team's list
  after each accepted commit. `!` entries in `repos` and `repo-sets` leave out repositories of a set. A set
  that can't be read matches nothing, and in a `!` entry leaves out the binding; the session message names
  it with its binding.
- `source validate DIR` reports set entries that aren't repositories.
- `source update` shows the entries a commit adds to or removes from the repo sets in use, the sets it
  removes and their errors, with the bindings that use or exclude them, and warns about `HOST/**` entries.
- `source update --json` lists the review's new errors in `diagnostics`, with the bindings they affect.
- The config's `mcp-servers` can give a source's server a new name, `jira_iam: {use: 'iam-team:jira'}`, wherever
  the server comes from, also when a source's collection adds it. A new name keeps the server's approval and
  sign-in. `mcp status` gives the approve command for a server that needs approval.
- The notice about two MCP servers with one name suggests the config line that gives the source's server a new
  name. Of two skills with one name, Claude Code's slash commands offer the second as `/SOURCE-NAME`.
- `doctor` checks a session once for each agent that setup set up, so bindings with `agents` are checked too. Each
  error names the agents that have it. `doctor --json` lists each agent's files and skills in `sessions`.
- Over capacity and over `max-ctx-chars` are also reported by `mcp status`, and the session message names the
  bindings of the dropped files. In `--json`, a file dropped over `max-ctx-chars` is an error, not a warning.
- A source's server that isn't approved on this machine, or whose definition changed since its approval, is an
  error with its approve command in the session message, the log, `context explain`, `doctor` and `mcp status`.
  `mcp status` exits 1 when a session in the folder wouldn't get everything it should. `binding add` refuses
  such a server and gives the approve command to run first.

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
