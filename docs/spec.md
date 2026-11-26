# Config spec: ctxtra/config/v1

> **Preview.** The format may still change without a migration path. ctxtra does not protect you from the
> content of the files you bind: an instruction file or skill is acted on like anything you type to the
> agent. Bind only files you wrote or reviewed.

For how this is meant to be used, step by step, see [scenarios.md](scenarios.md).

## Terms

- Session folder: the folder where an agent session starts.
- Config: the user's `config.yaml`. It declares sources, servers and ordered bindings.
- Binding: conditions for a session, and the instructions, skills and servers to add there.
- Source: a `repo` read at an accepted commit, or a `dir` read live.
- Lock: `ctxtra.lock`, which records accepted commits for repo sources.
- Manifest: a source's optional `ctxtra-source.yaml`. It describes content, never bindings.

Instructions are plain Markdown. Skills are folders with a `SKILL.md` file.

## File locations

These defaults apply on Linux and macOS. An XDG variable applies only when its value is an absolute path.
The hook, MCP proxy and background checks use the same folders. `ctxtra doctor` shows all four and the config file.

| Folder | Default | Variable | Holds |
|---|---|---|---|
| Config | `~/.config/ctxtra` | `XDG_CONFIG_HOME` | `config.yaml`, `ctxtra.lock` |
| Data | `~/.local/share/ctxtra` | `XDG_DATA_HOME` | accepted source content and installed skills |
| State | `~/.local/state/ctxtra` | `XDG_STATE_HOME` | write locks, setup records, backups, MCP approvals and credentials, update results, the stored profile selection, session records, hook slots and logs |
| Cache | `~/.cache/ctxtra` | `XDG_CACHE_HOME` | git clones for fetching and review |

Each variable supplies the parent folder; ctxtra appends `ctxtra`. Relative and empty values use the default.
Sessions never read the cache. Deleting it changes no session content; the next fetch downloads again.
Deleting data removes downloaded content and installed skills until `ctxtra source download` and
`ctxtra skill install`. Deleting state removes the stored profile selection, session records, trusted remotes,
server approvals, sign-ins and secret values, setup records and update results: select profiles, trust folders,
approve servers and sign in again. The session message says what to run. `CTXTRA_LOG` still overrides the log
file.

## Config

There is one config: `config.yaml` in the config folder. Bindings live only in this file.
Relative paths start at its real folder, after symlinks. There is no `include`, alternate config flag or
`CTXTRA_CONFIG` override.

```yaml
schema: ctxtra/config/v1
sources:
  team:
    repo: git@github.com:acme/team-ctx.git
    branch: main
  dotfiles:
    dir: ~/dev/dotfiles
bindings:
  - name: me
    instructions: [~/agent-context/me.md]
  - name: work
    location:
      dirs: ['~/work/**', '!~/work/clients/**']
      repos: ['github.com/acme/**']
    instructions: ['team:docs/*.md']
    skills: ['dotfiles:skills/*']
  - name: review
    agents: [claude-code, codex]
    skills: ['dotfiles:skills/review']
max-ctx-chars: 40000
check-releases: false
session-message:
  max-instructions: 10
  max-skills: 0
```

Only `schema` is required. `ctxtra/config/v1`, `ctxtra/lock/v1` and `ctxtra/source/v1` are different
file kinds. The earlier `ctxtra/v1` format is not read or converted.

`ctxtra collection add NAME` writes a collection with `--instruction`, `--skill`, `--mcp-server`,
`--collection` and `--source`, as `binding add` does. `ctxtra repo-set add NAME` writes a set with
`--repo` and `--subdir`. `--subdir` needs exactly one `--repo`; for several repositories with subdirs,
use `--file`. Both commands need at least one entry and accept `--file FILE` or `--file -` for one
entry in YAML instead of entry flags. `--replace` changes an entry in place or adds it when missing.
The same entry already there changes nothing; another entry with that name is an error. Each has
`remove NAME` and `list`; add and remove accept `--dry-run`.

`ctxtra config set KEY VALUE` changes `check-releases` (true or false), `max-ctx-chars`,
`session-message.max-skills` or `session-message.max-instructions` (whole numbers of 0 or more).
`ctxtra config unset KEY` removes the setting, so its default applies. Other keys and values of the
wrong type are errors and change nothing. A missing setting already uses its default.
Counts are read as decimal, including values with leading zeros. Setting a count compares those
decimal values when deciding whether the setting is already there.

`collection add` and `mcp add --use` succeed for an identical entry without checking current file or
source availability. This includes collection `--replace`. A config or lock that cannot be read
still stops the command.

Every `list` accepts `--json`. Commands exit 0 on success, 1 on a mistake or refused change, and 2
on bad flags or arguments.

Names of sources, bindings, collections, repo sets and profiles are 1–64 letters, digits, `.`, `_` or `-`,
starting with a letter.
Case counts. Server names have the stricter rule in [MCP servers](#mcp-servers).

`ctxtra binding add NAME` writes a binding from flags, one per key, each repeatable:
`--dir`, `--repo` and `--repo-set` for `location`, `--profile`, `--agent`, and the content flags `--collection`,
`--instruction`, `--skill` and `--mcp-server`. At least one content flag is required. A condition without flags
is left out, so a binding without `--dir`, `--repo` and `--repo-set` applies everywhere. A `!` entry is a value:
`--profile '!oncall'`. `--subdir PATTERN` writes the `subdirs` of the one `--repo` given; with several `--repo`, or
none, the command exits 1. Disk paths given to `--dir`, `--instruction` and `--skill` are relative to the current
folder and written as absolute paths, using `~` under home. Source references stay as written.

`--source SOURCE` makes the values of `--collection`, `--repo-set`, `--mcp-server`, `--instruction` and `--skill`
names or paths in that source, written with the config's name for it, after any `!`: `--source cat --skill
'!skills/old'` writes `'!cat:skills/old'`. SOURCE is a source name, or the address of a repo source when it
contains `/` or `:`; a `dir` source is chosen by its name. Addresses are compared as `host/path`, so `git@github.com:acme/ctx.git` and `https://github.com/acme/ctx`
are the same. `URL//FOLDER` picks the source with that `subdir`. No source, several, or a value that already
names a source exits 1 and changes nothing; with no source, the error gives the `ctxtra source add` command.

`--file FILE`, or `--file -` for stdin, gives the whole binding in YAML as in the config, without `name`, instead
of the entry flags. It is checked as the config is: a YAML tag, an anchor or an unknown key is refused. Its
relative paths are relative to the config's folder, as in the config.

A new binding goes last, or where `--before NAME` or `--after NAME` says. A binding name that exists exits 1,
unless the binding is the same after reading the YAML: then the command exits 0 and writes nothing. `--replace`
changes the binding in its place, and adds it when there is none. `--before` and `--after` can't move a
binding that `--replace` changes.

The command refuses a binding with an error, or whose content would be missing, untrusted or over capacity.
It then prints the binding as written, where it applies (every session, or which locations, agents and
profiles), and the files, skills and MCP servers a session there gets. Files and skills of a repo source that
no other binding used get content warnings, as `source add` shows them. `--dry-run` checks and shows the
binding and writes nothing. `ctxtra binding list` shows the entries as written; `ctxtra binding remove NAME`
removes one.

Writers keep text they do not change, including comments and order. A layout they cannot edit safely,
such as a flow-style root, is refused with an explanation. Write that part in block style or edit it by hand.
A config whose lines end in a lone carriage return is refused too; save it with `\n` or `\r\n` line breaks.
A change that would make the config or the lock larger than 4 MiB, which ctxtra reads at most, is refused.
Config and lock writes share an operating-system lock in the state folder. Each writer re-reads both files,
changes its own entries and replaces each file atomically. A symlinked config stays a symlink.
A lock with invalid YAML or another schema stops every write, also one that changes only the config, until
it is fixed or removed. A broken lock entry stops only writes that change the lock.

A changed config is checked before it is written, written atomically, and the
old one is kept in `~/.local/state/ctxtra/backups`. Setup and config edits keep the latest three backups per original
file, pruning only after the config is successfully replaced or removed. Cleanup is best effort: a
cleanup error can leave extra copies, but does not turn a completed config change into a failed one.
If the latest backup has the same content, it is reused. New backup names contain a SHA-256 hash of the
absolute original path, a short readable name and a UTC nanosecond timestamp. Parent-directory symlinks
are resolved before hashing. Timestamps advance even if the clock moves backward; names break ties.
A symlinked config stays a symlink, and its target path identifies its backups. Import backups and
unrelated files are not pruned. Older config backups lack the full-path identity and are left alone,
because their names can refer to several original paths. Remove them manually when no longer needed;
agent config backups may contain tokens.

## Sources and trust

A source has exactly one of `repo` and `dir`. Files from a source use `SOURCE:PATH`.
The path is relative to the source's top and cannot leave it with `..` or an absolute path.
Paths starting with `./`, `/` or `~` always mean disk paths, even with a colon in the name.
Other entries without a source prefix are relative to the config's real folder.
A disk path never maps to a reviewed copy automatically.

### Live folders

```bash
ctxtra source add --dir ~/dev/dotfiles
ctxtra source trust dotfiles
ctxtra binding add me --instruction dotfiles:agents/me.md
```

A `dir` is absolute or starts with `~`. Edits apply to new sessions immediately.
With no remote in its repository, it needs no trust record. With remotes, `source trust NAME` records
them on this machine. A change to the remotes stops live reading until the user trusts the folder again.
`source trust --dry-run NAME` previews that consent. It never converts a repo source to a dir source.
The command says that the folder's files, servers and repo sets apply as they are, without review.
If a folder gets a remote later, the session message, `doctor` and `context explain` name
`ctxtra source trust NAME` and the alternative of adding a repo source for reviewed commits.
If trusted remotes change, those reports say to trust the source again. Every fetch and push URL of every remote counts;
equivalent SSH and HTTPS addresses count as the same repository.

A disk file is read only when it is outside git, in a repository without remotes, or under a trusted dir source.
The config itself is always read. A remote repository outside trusted dir sources is refused with an error
and the commands for reviewed commits or live trust. A nested repository needs its own trust unless it has no
remote. A source covers its folder, not another clone or a worktree elsewhere.

Symlinks in live paths are followed only when every link and the destination pass the same trust check.
Reads open the destination path that passed the check. The displayed path and read cache use that path too.
A link in a trusted folder cannot make an untrusted file readable. Each symlink on the way to a `dir` passes
the check at its own location: outside git, in a repository without remotes, or in another dir source that is
trusted without it. Trusting the folder a link leads to doesn't cover the link, also in the same repository.
Otherwise the source is not read, `source trust` refuses it, and the error names the folder the link leads to.
A repo source leaves symlinks and submodules out, including links in an explicit path or at its manifest.
Repository trust is checked without running git; git config includes are not followed.

A local repo path or dir folder missing on this machine is not an error. Its entries add nothing.
`doctor` and `context explain` list it as not on this machine; the session message says nothing about it,
and update checks skip it.
A path that exists but is not a folder, or a repo path that is not a repository, is an error.

## Repo sources

```bash
ctxtra source add --repo git@github.com:acme/team-ctx.git
ctxtra binding add billing --repo github.com/acme/billing --instruction team-ctx:services-overview.md
ctxtra source update --dry-run team-ctx
ctxtra source update team-ctx
ctxtra source download
```

A repo source has `repo` and exactly one of `branch`, `tag` and `rev`. The address is an HTTPS or SSH URL,
`user@host:path`, or an absolute or `~` path to a local repository. `source add --repo` accepts relative
local paths too and writes them as absolute paths.

| Key | Follows | `source add` |
|---|---|---|
| `branch` | New commits of the branch. | `--branch BRANCH`, or by default the remote's default branch, or the local clone's checked-out branch. A detached local clone needs `--branch`. |
| `tag` | The tag. When someone moves it, the update check reports it and `source update` reviews the new commit. | `--tag TAG`. An annotated tag counts by the commit it points to. |
| `rev` | One commit: a full ID, 40 hex digits or 64 in a SHA-256 repository. Nothing is new until you edit it. | `--rev REV` takes any revision that names one commit (a short ID, a tag, a branch, `HEAD`, `refs/heads/NAME` or `refs/tags/NAME`, also with `~N` or `^N` after it, as in `main~1`) and writes its full ID. A name that is a tag and a branch means the tag. |

Local sources follow their named branch, never its upstream or uncommitted files.

`subdir: PATH` makes a folder of the repository the source's top, with `source add --subdir PATH`. Its
manifest, every `SOURCE:PATH` and the update review are under it; the rest of the repository isn't part of
the source. `subdir` is a relative path to a folder. A symlink is never followed as `subdir`, and a missing
folder is an error.

`source add` accepts the first commit without review, as cloning does. Read the source before adding it.
It writes the source and its accepted lock entry, lists executable files and source servers, and adds no binding.
It fetches the whole repository and checks it against the [limits](#how-ctxtra-runs-git) before it writes
anything: over them, the config, the lock and the store are unchanged.

Without `--name`, the source is named after the repository, and with `--subdir` after the repository and the
folder's last part: `agent-context-billing` for `teams/billing`. Adding the same repository, branch, tag or rev
and subdir again succeeds without writing, even with another requested name. A name that another source has is
an error that changes nothing and asks for `--name`. `--check-updates=false` disables checks on a new repo
source; a `rev` source has none.

Two repository addresses compare as lower-case `host/path`, without scheme, user, port, trailing `.git`
or slash. Local paths compare by their real paths. The original spelling is kept for fetching.

### The lock file

`ctxtra.lock` lives next to the config's real file, after symlinks:

```yaml
schema: ctxtra/lock/v1
sources:
  team:
    repo: git@github.com:acme/team-ctx.git
    branch: main
    commit: 9f8e7d6c5b4a39281706f5e4d3c2b1a098765432
```

Only repo sources have lock entries. Each records what the commit was accepted for: the address, the
`branch`, `tag` or `rev`, and `subdir`, then the full commit ID: 40 hex digits, or 64 for a SHA-256
repository. Entries are sorted by source name, with keys in that order.
A missing or mismatched entry makes the source inactive: another address, `branch`, `tag`, `rev` or `subdir`
in the config than the entry has. Sessions report the source and the `ctxtra source update NAME` command that
accepts a version. They never guess a commit. Changing a URL's spelling to the same repository does not
invalidate the lock.

Sessions read only downloaded locked commits. When one is missing, they name the source and say to run
`ctxtra source download`. This downloads exactly the locked commits, asks nothing and leaves the lock
unchanged. Sources not on this machine are skipped. Deleting the cache does not remove accepted content.

Adding a source writes the lock before the config. Unused lock entries are removed on the next write.
Update fetches and reviews outside the write lock. Before accepting it checks that the source and its
lock entry have not changed during the review; otherwise it stops and asks the user to run it again.

### Commands

| Command | Does |
|---|---|
| `ctxtra source add --repo URL [--branch BRANCH \| --tag TAG \| --rev REV] [--subdir PATH]` | Fetch and accept a commit, and write the source and its lock entry. |
| `ctxtra source add --dir DIR` | Add a live folder. |
| `ctxtra source list`, `ctxtra source show NAME` | Show sources and their state. Both take `--json`. |
| `ctxtra source update [NAME]...` | Review new commits and moved tags, and accept them in a terminal. |
| `ctxtra source update --dry-run [NAME]...` | Review without accepting or dismissing; writes only to the cache. Takes `--json` and `--full`. |
| `ctxtra source download [NAME]...` | Download exactly the lock's commits. |
| `ctxtra source trust NAME` | Record a dir source's remotes for live reading. |
| `ctxtra source remove NAME` | Remove the source and its lock entry; report bindings still using it. |
| `ctxtra source validate DIR` | Validate the one manifest at DIR, without config, trust or network. |

`source add`, `source remove` and `source trust` take `--dry-run`. A source removal keeps the user's
bindings and reports each dangling reference. Source credentials remain until explicitly removed.

### Source manifests

A source may have `ctxtra-source.yaml` at its top: with `subdir`, in that folder, and a manifest at the
repository's root is not read. ctxtra never searches deeper for manifests.
A source without one still works by file paths. A broken manifest leaves direct file references usable.

```yaml
schema: ctxtra/source/v1
mcp-servers:
  jira:
    url: https://mcp.atlassian.com/v1/mcp
    notes: Choose the work account
```

A manifest can declare `mcp-servers`, `collections` and `repo-sets`. It cannot declare sources or bindings.
Manifest paths refer only to files under that source; they cannot name another source or the user's disk.
A bare name in a collection's `collections` or `mcp-servers` is a collection or server of the same manifest.
See [Collections](#collections) and [Repo sets](#repo-sets).

### Check a source repository

```bash
ctxtra source validate .
ctxtra source validate . --json
```

Validation reads `DIR/ctxtra-source.yaml`, with the size limit that sessions use, and the files its
collections name, and nothing outside DIR. It
checks YAML, names, keys, entry shapes and MCP definitions. It reads each collection the way a session reads
it from an accepted commit, and reports what a session would: a path that matches nothing, a missing nested
collection, a cycle, a `!` entry that excludes nothing, and a skill whose `SKILL.md` has no valid header.
A symlink, a file in a nested repository or submodule, and `.git` are not in a commit that ctxtra reads,
and an entry that names one is an error that says so.
Secret-looking header and environment values are warnings. Errors exit 1.
A missing manifest is an error. A symlinked manifest is refused, as it would be in a repo source.

### Review

What counts as new depends on what the source follows:

- `branch`: a new commit that changes files under `subdir`. One that changes only other folders is
  nothing new: `source update` says the source is up to date.
- `tag`: a moved tag, always, even when nothing under `subdir` changed. A new commit on a branch is nothing.
- `rev`: nothing. The version changes only when you edit `rev`.
- A changed `branch`, `tag` or `rev` in the config is always reviewed as a change from the locked commit,
  and can be accepted when nothing under `subdir` changed: the review says so.
- A changed `repo` (another repository, not another spelling) or `subdir` is a new source: the review shows
  it from nothing, as `source add` does, with the files its collections offer in full and checked for
  content warnings, and its servers need approval and sign-in again. A source without a
  lock entry is reviewed the same way.

`source update` shows changes in every directly referenced file and every file inside a referenced skill,
under all bindings, whether their conditions match now or not. Patterns select files at both the old and
new commits, after the bindings' `!` entries and before content reads and limits, so additions, deletions
and skills whose `SKILL.md` became invalid are reviewed too. Newly broken references are errors in the
review, as a session would report them: a pattern that matches nothing, a `!` entry that now excludes
nothing, a `SKILL.md` that can't be parsed. Paths are relative to the source's top. Other changes are listed
by filename; `--full` shows their contents too. A branch that no longer continues the accepted commit gets a
force-push warning, and a moved tag says where it was. Nothing is accepted until the user answers yes in a
terminal.

For each of the source's collections that a binding uses, nested ones included, the review says when the new
commit removes it. For each that a binding names, directly or through the config's own collections, it lists
the MCP servers, skills and files that it newly adds, nested collections included, with the bindings: `collection essentials newly adds MCP server deploy, skill
skills/release; binding iam uses it`. Errors in those collections are reviewed like other new errors, so a
new cycle is named. `--json` lists these in `diagnostics`: a removed collection as a `warning`, new content as
a `notice`.

For each of the source's repo sets that a binding uses in `location.repo-sets`, the review shows the entries
the new commit adds to or removes from it, and says when the commit removes the set, with the bindings that
use or exclude it: `repo set clients is removed; binding work excludes it; once the commit is accepted, work
is left out`. An added entry of the form `HOST/**`, such as `github.com/**`, gets a warning that it matches
every repository on that host. A set that errors left out of the accepted commit matched nothing, so when
the new commit fixes it, the review says it can be read now and shows all its entries as added. Errors in
those sets are reviewed like other new errors. `--json` lists a
changed set as a `notice`, and a removed set and a `HOST/**` entry as a `warning`.

Each new error in the review names the bindings it affects when the error doesn't name them itself, and
`--json` lists it in `diagnostics` as an `error`.

The review reads both commits from ctxtra's copy of the repository in the cache. The new commit reaches the
store only when you accept it. The question goes to stderr. Nonterminal use and `--no-input` fail before
fetching; `--dry-run` reviews without asking, and writes only to the cache: not the config, the lock, the
store or the state folder.
Answering no records a dismissed commit. Sessions using that source show a notice; the next review still
offers it. Input ending without an answer accepts and dismisses nothing. Accepting writes `ctxtra.lock`.
It accepts every file in the commit, including files the review lists only by name.

### Content warnings

`source add` and `source update` point out text that is easy to miss when you read a diff. They only warn:
nothing is blocked, and a file can have a good reason for any of them. A security guide can show `curl | sh`,
for example.

| Warning | What it finds |
|---|---|
| `hidden character` | Characters you can't see: zero-width characters, text-direction marks, invisible tag characters, line separators, other Unicode format characters, and control characters. Also two or more variation selectors in a row. A tab, a carriage return at the end of a line, a byte order mark at the start of a file and a zero-width joiner in an emoji are not reported. |
| `HTML comment` | Text in `<!--` and `-->`: the agent reads it, but a rendered Markdown view hides it. A comment is reported at its first changed line; comments that share a line share the warning. |
| `encoded text` | 80 or more hex digits in a row, or 80 or more base64 characters (`A-Z`, `a-z`, `0-9`, `+`, `/`, `-`, `_`) in a row with an uppercase letter, a lowercase letter and a digit. Text in URLs doesn't count. |
| `remote code` | A download command (`curl`, `wget`, PowerShell's `iwr`, `irm`, `Invoke-WebRequest`, `Invoke-RestMethod`) whose output a shell or an interpreter runs: through a pipe (`curl ... \| sh`, also with `sudo`, `env` or a path like `/bin/sh`), after `&&`, inside `$( )`, `<( )` or backticks, or `iex (iwr ...)`. |
| `secret path` | Files and folders that hold keys or tokens: `.ssh`, `.gnupg`, `.aws`, `.azure`, `.kube`, `.config/gcloud`, `.docker/config.json`, `.netrc`, `.npmrc`, `.pypirc`, `.git-credentials`, `.env` and `.env.*`, and the key files `id_rsa`, `id_dsa`, `id_ecdsa` and `id_ed25519`. |
| `new URL` | An `http`, `https`, `ftp`, `ws` or `wss` URL that none of the files you use or the review shows had in the accepted commit. Each URL is reported once. In a first version, every URL is new. |
| `new executable file` | A file shown in full that the commit adds as executable or makes executable. A file listed by name says `executable` instead. |

- `source update` checks only the lines that the files shown in full add, and shows each warning after its
  line: `warning: line 14: remote code: ...`. It reads the whole new file to know where a comment starts.
- Files listed by name are not checked. `--full` shows and checks them.
- `source add --repo` checks the files the source offers: every file of its manifest's collections, nested
  ones and the files of their skills included, and files that config bindings already reference under that
  source name. Other files of the repository are not checked. It also shows errors in the collections.
  Then it lists the manifest's collections and repo sets, and one `binding add` line that uses them.
- `binding add` checks the files of a repo source that it binds and no other binding used before, and every
  regular file in the folder of each such skill: `warning: SOURCE:PATH:LINE: CHECK: TEXT`.
- A file that isn't UTF-8 text, or has a NUL byte, is not checked, and the review says so:
  `note: not checked for content warnings: binary or not UTF-8`.
- At most 50 warnings about a file's text are shown. A file with more gets a note with their number. A URL
  is new again in the next file when its warning wasn't shown.
- `source update --dry-run --json` lists them in `diagnostics`: `warning` with `SOURCE: PATH:LINE: CHECK: TEXT`,
  or `SOURCE: PATH: new executable file`, and `notice` for a file that isn't checked or has more than 50.
- Text from the files is shown with control and format characters escaped, as in the diff.
- The checks are fixed patterns. They use no network and no model: the same commits give the same warnings.

The warnings help you read. They can't show that a source is safe: plain text can tell an agent to do harm
without any of these patterns, and a pattern can be written so that it doesn't match. No warning only means
that these patterns weren't found. Read what you accept.

### Notices

A session that uses a repo source says when the last
[update check](#update-checks) found new commits, or that a tag moved. It also says when you dismissed a
commit:

```text
ℹ team-ctx has new commits on main
  run `ctxtra source update team-ctx` to review them.
ℹ obs-team: the tag v1.4.0 moved to another commit
  run `ctxtra source update obs-team` to review it.
ℹ team-ctx has a newer commit that you didn't accept
  run `ctxtra source update team-ctx` to review it again.
```

Claude Code, Codex, Muse and Pi show these notices at session start, after an empty line, each with `ℹ` and
the command to run on its own line. Notices name no commit IDs; `doctor` does. `context files` and
`doctor` show the notices too, with the command on the same line. With `check-updates: false`, there is no notice about new
commits: run `ctxtra source update` when you want to look.

When a new commit doesn't continue the accepted one, `source update` warns: the branch was force-pushed.
Accepting it is still your choice.

### Store

- `~/.cache/ctxtra/git/<id>.git`: a bare repository per source.
- `~/.local/share/ctxtra/store/<id>/<commit>/`: the files of each accepted commit, private and read-only:
  with `subdir`, only that folder's files, at their paths from it. Sessions
  read only these, and only through `SOURCE:PATH`: plain paths into the store are errors. A missing commit is reported
  (`ctxtra source download`), and the references to it are skipped; the rest of the binding still
  applies.
- `<id>` comes from the URL and `subdir`: a source whose URL changes doesn't use the old data, and two
  folders of one commit have their own.
- A commit is deleted from the store 30 days after the last session that used it started (`source update`
  cleans up). Accepted commits in the lock are never deleted. If the config
  cannot be read, nothing is deleted. A session marks a commit as used before it reads from it; while
  cleaning decides about that commit, the session waits up to a second, and else skips the source's files
  for this session. A session that runs longer than 30 days on an old commit can lose its skill files.
- `~/.local/state/ctxtra/state.json`: per source, by its address, its branch or tag and its `subdir`: the last
  fetch, the dismissed commit and what the update checks found; what the release check found. Losing it loses
  the notices.

### How ctxtra runs git

- It runs your `git`, so ssh keys, the ssh agent and credential helpers work. `source add`,
  `source update` and `source download` fetch, and git may prompt there. Sessions never fetch and never
  wait for the network. The [update checks](#update-checks), which run in their own process, run
  `git ls-remote`, which reads only the commit ID of a branch or tag. For a branch source with `subdir`, a
  check that finds a new commit also fetches the branch into the cache. git may not prompt in a check.
- Only the https and ssh transports, and the file transport only to fetch from a clone you named (a local repo source). No hooks, no submodules, no automatic maintenance. Nothing is checked
  out, so `.gitattributes` filters (git-lfs, smudge) never run. Diffs use no external diff and no textconv.
- Environment variables that point git at another repository or config are cleared.
- Every file path in a commit is checked before anything is written: no `..`, no `.git`, no control
  characters, no two file or folder names that a case-insensitive or Unicode-normalizing disk would merge.
  A commit over the limits (10,000 entries, 10 MiB per file, 100 MiB in total) is refused as a whole. A
  repository larger than 1 GiB with its history is refused too, and git's output is limited. The limits count
  the whole repository, also for a source with `subdir`. `source add` and `source update` check them before
  they write anything outside the cache.
- Control characters in diffs, paths and messages are shown as escapes, so they can't hide lines in your
  terminal.

## Update checks

ctxtra can tell you about a new ctxtra release, and about new commits or a moved tag of a repo source. A
check reads only a version number or a commit ID, changes nothing in the config, and accepts nothing: you
still upgrade ctxtra and review commits yourself. For a branch source with `subdir`, a check that finds a new
commit also fetches the branch into ctxtra's cache, to tell whether anything changed under `subdir`.

| Check | Default | Turn it on or off | What it asks |
|---|---|---|---|
| New ctxtra release | on | `check-releases: false` at the top of the config | GitHub's API, for the latest release (prereleases left out); from the `ctxtra-rc` formula, also for the list of releases, for the newest release candidate |
| New commits of a source, or a moved tag | on | `check-updates: false` on the source; a `rev` source has nothing to check | `git ls-remote` of the configured branch or tag, remotely or in the named local clone |

Sessions never wait for a check. At session start, the hook shows what earlier checks found, and starts a
check in the background when the last one is an hour old. The background check runs as a separate
process, `ctxtra check-updates`, which checks only what was due, and the next session shows its result.
Parallel sessions start one check: the first one records the attempt. A failed check also waits an hour
before the next try.

The notices go only to you, never to the model, in the agents that show a message at session start (see
[Notices](#notices)):

```text
ℹ ctxtra update v0.3.0 is available - https://github.com/ctxtra/ctxtra/releases
  run `brew upgrade atelpis/tap/ctxtra` to update.
ℹ team-ctx has new commits on main
  run `ctxtra source update team-ctx` to review them.
```

- The release notice comes first. It compares with the version of the ctxtra that runs, so it goes away
  after an upgrade. Its second line, the upgrade command, is there only when ctxtra was installed with
  Homebrew or `go install`. Development builds (`dev`, or a version from a commit) aren't checked at session start.
- A release candidate, such as `v0.6.0-rc.1`, is a version too. It is older than its release: `v0.6.0-rc.2`
  comes before `v0.6.0`. Other prerelease names (`-beta.1`) are development builds.
- A ctxtra installed with the `ctxtra-rc` formula is told about release candidates too, with
  `brew upgrade atelpis/tap/ctxtra-rc`. Other installs are told only about releases, and a release candidate
  that an earlier `ctxtra-rc` check found is no notice for them.
- The source notice compares with the accepted commit. A commit you dismissed is no new notice, and neither
  is a commit that changes nothing under the source's `subdir`.
- A failed check adds no notice and never counts as up to date: the notice from the last check that worked
  stays. `doctor` shows why the check failed, or couldn't start, and when the last one that worked ran and
  what it found. For a repo source with `check-updates: false`, `doctor` says that its update checks
  are off.
- `ctxtra check-updates` runs the checks now and shows their results. It exits with 1 when a check fails,
  or when it can't save the results for sessions.
- `CTXTRA_NO_UPDATE_CHECK=1` stops the checks that sessions start when the hook inherits the variable.
  Muse clears its hook environment: use top-level `check-releases: false` for release checks and
  `check-updates: false` on each repo source instead. Notices from earlier checks still show.
- Only the config can turn checks on or off. A config that is
  ignored as a whole (see [Validation](#validation)) runs no checks, because its `check-releases: false`
  may be what failed.

What a check sends:

- The release check sends your IP address and ctxtra version (in the User-Agent) to GitHub.
- A source check connects to the source's remote with your git credentials, like `source update`. git
  may not prompt there: a key with a passphrase that isn't in your ssh agent fails the check. But an ssh
  agent that asks for approval (for example 1Password) or a hardware key can still ask you to approve or
  touch it when a session starts. Set `check-updates: false` for such a source.

## Bindings and conditions

Bindings are a list with unique names. Their order is the order of content in a session.
Only the user's config decides conditions: a source cannot target sessions.

```yaml
bindings:
  - name: me
    instructions: [~/agent-context/me.md]
  - name: billing
    location:
      dirs: ['~/work/**', '!~/work/clients/**']
      repos:
        - repo: github.com/acme/mono
          subdirs: ['services/billing/**', '!services/billing/legacy/**']
    agents: [claude-code, codex]
    instructions: ['team:billing/*.md']
```

`location`, `profiles` and `agents` are conditions. All present conditions must hold. Inside each list,
any positive entry holds, minus every quoted `!` entry. A missing condition does not limit the binding.
A binding without conditions applies to every session. Empty conditions are errors, not defaults.

### Location

Location has a folder check (`dirs`) and a repository check (`repos` and `repo-sets` together). Either
check can make it hold.
An exclusion affects only its own check. A list with only exclusions holds everywhere but those places.
A bad positive entry matches nothing; a bad exclusion leaves out the binding so it cannot apply wider.

`dirs` entries are absolute or start with `~`. They match the session folder, or the repository's main
checkout, so worktrees elsewhere still match. Symlinks in the fixed prefix are resolved.
Folder comparisons follow the session volume's case rule. If that rule is unknown, positive checks
compare exactly and exclusions ignore case, with a notice explaining it.

`repos` entries are `host/path` patterns or `{repo: PATTERN, subdirs: [PATTERN, ...]}`.
The latter checks both the remote and the folder relative to the worktree top (`.` at its top).
Only negative subdirs mean the rest of that repository. Remote forms compare by the same rule as sources.
Any remote can match. A session outside git does not match a positive repos entry.

Patterns use doublestar globs: `*` stays in one segment, `**` crosses segments, `x/**` includes `x`,
and `{a,b}` selects alternatives. Without `/**`, a folder pattern matches only that folder.
For submodules the main checkout is the submodule. With `git init --separate-git-dir`, it is unknown,
so dirs checks only the session folder. If git does not answer within 3 seconds, the folder counts as outside git.

### Repo sets

A repo set is a named list of repositories, in the form of `location.repos`: patterns,
`{repo, subdirs}` and quoted `!` entries. A team's manifest and the config can both have a `repo-sets`
map. A binding links to a set in `location.repo-sets`: `SOURCE:SET` for a source's set, a bare name for
the config's own.

```yaml
# ctxtra-source.yaml of iam-team
schema: ctxtra/source/v1
repo-sets:
  services:
    - github.com/acme/iam-*
    - '!github.com/acme/iam-sandbox'
    - {repo: github.com/acme/mono, subdirs: [services/iam/**]}
```

```yaml
# the config
repo-sets:
  clients: [github.com/acme/client-*]
bindings:
  - name: iam-repos
    location:
      repo-sets: ['iam-team:services']
      repos: [github.com/me/iam-playground, '!github.com/acme/iam-legacy']
    collections: ['iam-team:base']
  - name: tracker
    location: {repo-sets: ['!clients']}
    mcp-servers: [linear]
```

- A set matches a repository when one of its positive entries matches and none of its `!` entries does.
  A set's own `!` entries only say which repositories are in it.
- `repos` and `repo-sets` are one check: a `!` entry in `repos` removes a repository that a set adds, and
  `'!SOURCE:SET'` leaves out every repository of the set. Moving entries into a set never changes the result.
- The set is linked, not copied: when the team adds a repository to it, sessions there get the binding
  after `source update` accepts the commit, with no change to the config.
- A set lists repositories only. A folder, a `SOURCE:SET` reference or a bad pattern is an error, and the
  set's other entries apply, also in a set that a binding excludes. A set without entries matches nothing.
- A set that can't be read is an error: it doesn't exist, it has an error that leaves it out (an unknown
  key, for example), or its source can't be used. In a positive entry it matches nothing. In a `!` entry it
  leaves out the binding, so the binding never reaches the repositories it excludes, also on a new machine
  before `source download`. A set of a source that is not on this machine does the same, without an error.
- `context explain` names the set and the set entry that matched or left out the session. The session
  message names each set that can't be read, with its binding and what that does to it, among the first
  three errors it names (see [Validation](#validation)).

### Profiles and agents

`agents: [claude-code, codex]` selects those agents, ignoring case. The IDs are the ones setup uses.
An unknown ID leaves out the binding with an error. A subagent has its parent's agent ID.
`agents: ['!cursor']` selects every other agent. `context files`, `context show`, `context explain`
and `mcp status` take `--agent ID`. Without it, bindings with agents do not apply.

`doctor` checks a session in its folder once for each agent that setup set up, with that agent's capacity.
Each session error ends with the agents that have it, such as `(codex)`; errors of bindings that apply to no
agent there have none. Agents whose sessions get the same files, skills and servers are listed together;
otherwise each agent's are listed under its name. With no agent set up, it checks a session of no agent, where
bindings with agents don't apply. `doctor --json` has the list in `sessions`, each with its `agent`, `files` and
`skills`.

`profiles: [iam]` holds when `iam` is active; `profiles: ['!oncall']` holds when `oncall` is not. Names
are case-sensitive.

### Active profiles

A new session's active profiles come from, in this order:

1. `CTXTRA_PROFILES`, when it is set, even to an empty value: a comma-separated list of names. Spaces around
   names and empty entries are ignored, and a name given twice counts once. An invalid name is an error and is
   left out; the other names apply.
2. The stored selection, `profiles.json` in the state folder: on this machine, outside the config. A file that
   can't be read is an error, and no profile comes from it.
3. Otherwise, no profile is active.

An active profile that no binding uses, a `!` entry included, is an error: likely a typo. The other profiles
still apply.

`ctxtra profile set NAME...` replaces the stored selection. It refuses invalid names and names no binding
uses, lists the names the config uses, and changes nothing on failure. A `!` entry counts as a use.
`ctxtra profile clear` empties the selection. Both refuse to overwrite a selection file they can't read,
and name it. Neither changes the config. `ctxtra profile list` lists every name the bindings use, whether
it is active now, the active selection's origin and the bindings that use each name. It takes `--json`.

`context files`, `context show`, `context explain`, `doctor`, `mcp status` and the MCP proxy use the profiles a
new session would get. The `context` commands take `--profile NAME`, repeatable, to show a session with only
those profiles active; nothing is stored.

Codex's MCP environment allow-list includes `CTXTRA_PROFILES`, including an empty value, so the proxy
selects the same profiles as the hook. Run `ctxtra setup --agent codex` again for an entry that lacks it;
`doctor` reports the missing variable. Muse clears both processes' shell environment, so both use the
stored selection. Setup does not record a profile variable in Muse's config.

When `ctxtra-follow` is bound, the session's text gives its exact `ctxtra context show FOLDER` command,
with `--agent ID`, one `--profile NAME` per active profile, the session folder in `--exclude-session`, and
`--part 1`. A session with no profile gets a `CTXTRA_PROFILES=` prefix, so a later selection doesn't apply.
Keep those options when following work into another folder or reading another part.

### A session keeps its profiles

A session keeps the profiles it started with, also when the variable or the stored selection changes while it
runs. ctxtra keeps one record per session in `sessions/` in the state folder, named by a hash of the agent and
the session ID, readable by the user only. It holds only the profile names and their origin: files stay live.

| Hook event | Profiles |
|---|---|
| Session start, `/clear` | The current ones; they replace the record. |
| Subagent start | The parent session's record. |
| Compaction, resume | The session's record. |
| No session ID, or no record | The current ones; recorded when there is a session ID. |

When a record can't be read or written, the event gets the current profiles, the session starts, and the
log has the error. Session IDs are the agent's own; ctxtra never guesses them.

- Claude Code, Codex and Copilot give the parent's session ID at subagent start. Copilot uses `sessionId`,
  not its child `agentId`. Resume and compaction keep the record when a hook runs.
- Muse gives the child's `session_id` and the native parent in `MUSE_SESSION_ID`. Without a distinct parent
  ID, its subagent uses the current selection. A child's later events keep its own record.
- OpenCode, Hermes and DSH plugins forward `session_id` and `ctxtra_parent_session`. Run setup again for
  those agents to get this behavior. Older plugins without these fields use the current selection.
  `doctor` reports OpenCode protocols below 2, Hermes below 4 and DSH below 2 with the setup command;
  it exits 1 until the plugin is updated. Newer protocols are accepted.
  OpenCode's context callbacks use an existing session record, also after a plugin restart or cache eviction;
  a new child inherits its parent's record. Its `/clear` creates a new session ID and selects profiles anew.
- Antigravity records each `conversationId`, but gives no parent: each subagent takes the current selection.
- Cursor, Pi and Devin use the current selection at every hook event.

With hook slots, the slot that resolves the session start chooses the profiles, and the other slots share
its text. Where an agent keeps the hook's text in its history and runs no resume or compaction hook, there
is no new lookup. Copilot can lose the hook's text at compaction without calling the hook again. Hermes's
default in-place compaction keeps the session ID; rotation mode has not been verified.

A record no hook event used for 30 days is deleted. Sessions start that cleanup in the background at most once
a day (`ctxtra hook --clean-sessions`); a hook never waits for it.

The session message names the active profiles and their origin when a profile is active or has an error, and
lists the profile errors. The log records `profiles`, `profiles_from` and, when they come from the session's
record, `profiles_kept` for every hook event. `context explain`, `doctor` and `mcp status` print `profiles:` at
the top; `context files` and `context show` print it on stderr when a profile is active or has an error. Their
`--json` output has `profiles` with `active` and `from`.

Where a session's profiles can differ from what you expect:

- The MCP proxy can't tell which session it belongs to: it uses the current profiles when it starts. On resume,
  its servers can follow a newer profile than the instructions. After a switch and `/clear`, it keeps the
  servers of the profiles it started with until the session restarts.
- An agent started from another terminal (for example by Orca, Rex or Herdr) uses the current selection unless
  started with `CTXTRA_PROFILES`.
- Agents that don't pass the shell environment to hooks (Muse, IDE agents started from the Dock) see only the
  stored selection.

## Instructions and skills

- `instructions` and `skills` are lists. A binding can also add only MCP servers.
- Each path is absolute, starts with `~`, is relative to the config's real folder, or is a `SOURCE:PATH` reference.
- `instructions` and `skills` support the same doublestar globs as location. Quote patterns in YAML
  and on the command line, for example `instructions: ['agents/**/*.md']` or
  `ctxtra binding add mine --instruction '~/agent-context/**/*.md'`. Skill patterns select folders containing a regular `SKILL.md`, so `skills/**` can select skills
  recursively without selecting their parent folders.
- Glob metacharacters in a path are interpreted as patterns; for literal braces use character classes,
  for example `agents/[{]draft[}].md` to select `agents/{draft}.md`. Metacharacters in a resolved parent
  folder (such as the home or config folder) remain literal.
- Entries apply in list order; each pattern expands in lexical path order. Overlapping entries are
  deduplicated at their first position. A pattern with no matches is reported and skipped.
- An entry that starts with `!` leaves out the paths it matches: `instructions: ['agents/**/*.md',
  '!agents/drafts/**']`. It removes what the other entries of the same list add, wherever it is in the
  list. It is a path or a pattern, resolved like the other entries, and is compared with the added paths
  as text: symlinks are not followed. When `~/alias` links to `~/notes`, `!~/alias/x.md` does not leave
  out `~/notes/x.md`. Quote it: YAML reads an unquoted `!` as a tag. For a relative path whose name starts
  with `!`, write `./!name`.
- An exclusion that leaves nothing out is reported, and the binding still applies. A list of only
  exclusions without `collections` in the binding, and an exclusion that can't be resolved (an unknown source, a source that isn't available,
  a path outside the repo source), are errors, and the binding is skipped: without the exclusion, it
  would add more than intended. `context explain` lists the paths that exclusions left out.
- Live glob expansion follows symlinks only when each link and destination pass the trust check.
  An untrusted folder is left out with an error; other matches remain. Repo sources never follow symlinks.
- Each pattern is limited to 10,000 directory entries and operations, with a maximum traversal depth of
  64. A traversal error or exceeded limit skips the whole pattern, with an error.
- Repo-source patterns expand only against the selected commit. `source update` reviews matching files from
  both the old and proposed commits, including newly matched files. Local patterns are expanded live:
  new matching files apply immediately. Use `context files`, `context show` or `context explain` to inspect the result.
- Each file is added once per session, at its first position, and the same for each skill. Two different skills with
  the same name both reach the session and produce a notice.
- `SKILL.md` must start with YAML frontmatter that has a `description`. `name` is optional (default: the
  folder name). Other keys are ignored.
- Claude Code, Codex, Muse, Cursor, Copilot, Antigravity, Hermes, DSH and Devin get the file contents and a list of the skills (name, description, path),
  and read a `SKILL.md` when a task needs it. The list says that these skills are not the agent's own: its skill
  tool and slash commands can't load them. Pi gets instruction files as context files and skills as native skills.
  OpenCode gets instruction files in the system prompt and skills as native skills, which its `skill` tool loads.
- Claude Code also gets the skills as slash commands, from the plugin that setup writes to
  `~/.claude/skills/ctxtra/` (`$CLAUDE_CONFIG_DIR/skills/ctxtra/`), which Claude loads as `ctxtra@skills-dir`.
  Its mod runs `ctxtra hook claude-code PluginStart` at session start, with `ctxtra_plugin` in the input, and
  registers a command for each skill in the answer's `skills` (`name`, `description`, `path`). Of two skills
  with one name, the first keeps it and the next is `SOURCE-NAME`, with its source's name in front (its
  binding's outside sources), as Claude Code names the commands of added folders: `/release` and
  `/obs-team-release`. Claude Code's mods take only letters, digits, `_` and `-` in a name, so `-` joins the
  parts where Claude Code itself writes `:`. A command name that the session has already (a built-in, the user's own skill or command, another plugin's) is skipped. Running
  `/NAME ARGS` gives the model the `SKILL.md` text after its frontmatter as hidden context, with `$ARGUMENTS`
  replaced by `ARGS` (or `ARGS` added at the end), and starts a turn. A session without a surface (`claude -p`,
  the Agent SDK) gets no commands. The commands are not Claude's own skills: its `Skill` tool can't load them,
  and their frontmatter has no effect. Setup leaves a folder there that isn't ctxtra's alone, with a warning.

## Collections

A collection is a named set of what a binding adds: `instructions`, `skills`, `mcp-servers` and nested
`collections`, with patterns and `!` entries as in a binding. A source's manifest and the config can both
have a `collections` map.

```yaml
# ctxtra-source.yaml of iam-team
schema: ctxtra/source/v1
collections:
  base:
    instructions: [docs/conventions/*.md]
  essentials:
    collections: [base]
    instructions: [docs/*.md, '!docs/draft-*.md']
    skills: [skills/*]
    mcp-servers: [jira]
mcp-servers:
  jira:
    url: https://mcp.atlassian.com/v1/mcp
```

```yaml
# the config
collections:
  go-kit:
    instructions: [go-notes.md, 'go-style:go/*.md']
bindings:
  - name: iam
    collections: ['iam-team:essentials']
    instructions: ['!iam-team:docs/testing.md']
  - name: go
    location: {dirs: [~/dev/**]}
    collections: [go-kit]
```

| Entry | In a manifest | In the config |
|---|---|---|
| `docs/x.md` | A file in the source | A file relative to the config's real folder |
| `~/x.md`, `/abs/x.md` | Error | A file on disk |
| `SOURCE:docs/x.md` | Error | A file in that source |
| `base` in `collections` | A collection of the manifest | The config's own collection |
| `SOURCE:base` in `collections` | Error | That source's collection |
| `jira` in `mcp-servers` | A server of the manifest | The config's own server |

- A binding names a source's collection as `SOURCE:NAME`, and the config's own by its bare name. A
  `collections` entry is a pattern over names: `'iam-team:team-*'` adds each matching collection, in name order.
- A collection adds its nested collections first, in list order, then its own entries, minus its `!` entries.
  A binding adds its collections first, in list order, then its own instructions and skills. Each file, skill
  and server counts once, at its first position.
- A `!` entry in a binding's content list removes what it matches from everything the binding adds, its
  collections included. `'!iam-team:team-legacy'` in `collections` leaves out that collection wherever it is
  nested; a file it shares with another collection still comes through that one. A list of only `!` entries
  needs `collections` in the binding: without them there is nothing to remove from.
- A source's server that a collection adds gets the config's name for it, if the config's `mcp-servers` gives
  one with `use`.
- Errors in a collection leave out only the entry: a path that matches nothing, a nested collection that
  doesn't exist, a `!` entry that excludes nothing, an entry that names another source in a manifest. A
  collection with an unknown key adds nothing. Collections that nest each other form a cycle: each adds
  nothing, and the error names them. A pattern in a collection's `collections` matches that collection
  too: `all: {collections: ['*']}` nests itself.
- A binding entry that names a collection the source doesn't have, or a source without a manifest, is an
  error, and the rest of the binding applies.
- Each collection is read once per session, however many bindings and collections nest it, and expanded
  once for each set of `!` entries that leave out collections nested in it. Past 10,000 new expansions
  for one binding's collections, a collection adds nothing, with an error.

## Order and capacity guard

Bindings apply in list order. Each file and each skill is added once, at its first position.
Two distinct skills with the same name both reach the session and produce a notice.

`max-ctx-chars` is a guard against adding too much text, not a trimming setting. It counts Unicode code
points, headers included. Zero or missing means no configured limit. Going over it is an error.
The session still starts with whole files dropped from the end until the text fits; the skill list stays.
The session message, log, `context explain`, `doctor` and `mcp status` report it as an error that names the
bindings of the dropped files, and all but `mcp status` name the files too. Doctor exits 1. In `--json`, each
dropped file is a diagnostic of severity `error`.

## Capacity

Each agent gets a limited amount of text in full, its capacity. Above it, whole files are dropped from the
end, the same way as with `max-ctx-chars`; with both, the lower limit applies. The skill list is never
dropped. Going over it is an error, reported as for `max-ctx-chars`. `mcp status --agent ID` reports that
agent's capacity; without `--agent`, every agent that wouldn't get all of the text.

| Agent | Capacity | How the text gets there |
|---|---|---|
| Claude Code | 360,000 characters, counted in UTF-16 | 40 hooks per event, a part of up to 9,000 each |
| Muse | 360,000 characters, counted in UTF-16 | 40 hooks per event, a part of up to 13,000 bytes each, counted as in the JSON answer, and a summary of up to 2,000; text that isn't ASCII, or has many quotes or backslashes, needs more parts, so fewer files fit |
| Codex | 1,000,000 bytes | one hook with `additionalContextLimit` (250,000 tokens of 4 bytes); Codex 0.145.0 or later |
| Hermes | 1,000,000 characters | a plugin callback per part of up to 9,000 characters, or `hooks.output_spill.max_chars` if lower; the capacity shrinks with it |
| Antigravity | 650,000 bytes | one hook, an injected step per part of up to 40,000 bytes |
| Copilot | 140,000 bytes | one hook |
| Pi, OpenCode, DSH | 1,000,000 characters | one plugin |
| Cursor | 1,000,000 characters | one hook; not measured in full: in the Agent CLI every sampled codeword of 800,000 characters arrived, the desktop app doesn't wait for the hook (see [the guide](guide.md#limits)) |
| Devin | 1,000,000 characters | one hook; not measured |

Characters are Unicode code points; Claude Code and Muse count UTF-16 units, where an emoji or another
character outside the Basic Multilingual Plane counts 2. Copilot counts tokens, and ctxtra counts bytes of
UTF-8 for it: a tokenizer that works on bytes makes at most one token of a byte, so bytes are an upper bound
on the tokens of any text. Copilot passed about 150,000 tokens, and 140,000 bytes leave room for its own
prompt. The bound costs English text, about 140,000 characters fit where about 600,000 would, but no
estimate per character holds for every text: Copilot dropped all of 194,000 characters of short words and
punctuation (`x!x!`). Non-Latin text counts more in the agents that count bytes: a CJK character is 3
bytes in Codex, Antigravity and Copilot. Copilot drops all of the text, without a word, when it doesn't fit
the model's context window; ctxtra drops files before that.
When Copilot compacts a long conversation, ctxtra's text is gone for the rest of the session: Copilot
doesn't run the hook again.

Text that fits one part is given as it is. Otherwise each part starts with a label, `[ctxtra part 3 of 12]`,
and the first one says that the parts are one text, in part order. A file that continues from the part
before starts with `Contents of PATH, continued (part 2 of 4):`. A part ends before a file when little room
is left, else at a line end; a line longer than a part is split between characters.

Claude Code and Muse run the hooks of an event at once. The first one resolves the session and writes the
parts to `~/.local/state/ctxtra/slots/`, readable by you only; the others read them there, so the session is resolved
and logged once. The hook input doesn't tell two session starts of the same session, event and source
apart, for example two `/clear` in a row, or a retry after a start that didn't finish. So the parts are
pinned: a hook that starts within 3 seconds after they were written, or after a hook last read them, uses
them, and a hook that starts later resolves the session again, in a folder of its own. The hooks of one
session start begin within a second or so of each other, so they read one text, never parts of two. The
cost: a second session start within 3 seconds gets the text as it was at the first one, also when a file
changed in between; its log entry says `reused` with how much earlier. Each read starts the 3 seconds
again, so session starts that keep coming less than 3 seconds apart keep getting that first text, however
old it gets. The file stays after the hooks ended: a session start deletes the folders that no hook used
for a minute, never one that a hook used more recently. When the folder can't be written, for example
in a sandbox, each hook resolves the session on its own. Claude Code orders the texts by when the hooks
end, so each hook waits for the one before it, up to 2 seconds; the labels keep the text correct in any
order. Each hook's command says how many hooks its setup has (`--slot 3 --of 40`, and `ctxtra_slots` from
the Hermes plugin), and the text is split into at most that many parts.

A Claude Code or Muse hook without `--slot K --of N`, a Hermes plugin that doesn't send `ctxtra_slots`, and
a plugin that doesn't send `ctxtra_plugin` add nothing, and the log says why: no release writes such a
setup, and without the slot count each hook would get the whole text. `ctxtra hook` ignores command-line arguments it doesn't know, so hooks that a later
version set up still run after a downgrade.

When the binary that a hook runs is gone, Claude Code, Codex, Muse and Devin show `ctxtra: the ctxtra binary
that this hook runs is missing; reinstall ctxtra, or run ctxtra setup --agent claude-code with the new binary`
(with the agent's name) at session start, and Pi shows it as a warning. The other agents add nothing, without
a word. `ctxtra doctor` and the [setup notices](#setup-notices) show the missing binary. Setup leaves a config
alone that has a ctxtra hook changed by hand, and says so, instead of adding a second hook next to it.

## MCP servers

ctxtra gives sessions MCP servers, scoped by folder like instructions and skills.

`ctxtra setup` adds one MCP server named `ctxtra-proxy` to each agent that supports MCP (see [Setup](#setup)). The agent starts it like any local MCP server.
It gives the session the tools of the MCP servers bound to the session folder, and forwards each call.

### Declarations and references

The config declares its own servers in `mcp-servers`. Sources declare theirs in the same map in
`ctxtra-source.yaml`, with `schema: ctxtra/source/v1`. Sources need no manifest for direct file references.
Catalog reads never connect, start commands or fetch content.

```yaml
schema: ctxtra/source/v1
mcp-servers:
  linear:
    url: https://mcp.linear.app/mcp
    notes: Choose the work workspace
```

A server has exactly one of `url` and `command`. Remote settings are `headers`, `secret-headers`, `client-id` and
`scopes`; local settings are `env`, `pass-env` and `secret-env`. Both accept `notes`. Secret declarations list
names, not values. Commands use an absolute program path or an installed name in `PATH`, never a relative path.
Names are 1–20 lowercase letters, digits or `_`, start with a letter, and have neither `__` nor a trailing `_`.
Duplicate names in a declaration map invalidate both definitions.

`ctxtra mcp add NAME URL --header NAME=VALUE --scope SCOPE` writes remote headers and OAuth scopes.
Both flags repeat and preserve each value as text. They do not apply to local command servers.
`ctxtra mcp add NAME --use SOURCE:SERVER` gives a source's server a new name. It writes only `use`;
it cannot be combined with a URL, command or definition flags. It contacts no server and changes no
credentials or approvals. `--dry-run` checks without writing. Adding the same definition or alias
again exits 0 and changes nothing.

A config server's identity is `linear`; a source `work` has `work:linear`. Each has its own sign-in.
Bindings select them with `mcp-servers: [linear, 'work:linear']`. A binding can contain only servers.
Adding a source or a definition alone selects nothing.

Tools keep the unqualified prefix: `work:linear` exports `linear__TOOL`. Repeated selections of one identity
add it once. Two selected identities with the same name keep the first in binding order, with a notice
in the session message, explain, doctor and status. The notice names both and suggests a new name for the
source's server, such as `jira_iam_team: {use: 'iam-team:jira'}`. Doctor does not fail for the clash.
A winner that fails or lacks sign-in or approval never falls back to another account.

The config's `mcp-servers` can give a source's server a new name: `jira_iam: {use: 'iam-team:jira'}`, with no
other key. The server is then served as `jira_iam`, with tools `jira_iam__TOOL`, wherever it comes from: a
binding's `iam-team:jira` or `jira_iam`, a config collection, or a source's collection that adds `jira`. Both
names select one server, served once. Its identity stays `iam-team:jira`: sign-in, approval and `mcp approve`
use that, and a new name keeps the approval. Two entries that `use` one server are both errors, each naming
the other; the server keeps its own name. A `use` of a server the source doesn't declare is an error of the
bindings that select it.

The catalog is loaded on demand. Sessions read the manifests their bindings reference; list, doctor and setup
can enumerate all sources. Repo-source manifests use the accepted commit and store touch/size limits, never a fetch.
`source validate` validates the manifest and references without user config, credentials or network access. A source
used only for MCP servers still participates in source-update notices.

### Approval of repo-source servers

Every repo-source server needs separate machine-local approval before any MCP request, OAuth discovery or local
execution, even a remote endpoint needing no authentication. Until then, and after its definition changes, the
server is an error of the binding that selects it, with its `ctxtra mcp approve` command: in the session message,
the log, `context explain`, `doctor` and `mcp status`, which exit 1. The session keeps its name, so a server with
the same name is never served in its place, and the proxy doesn't start it. `binding add` refuses such a server, as
any binding a session wouldn't get in full, and gives the `ctxtra mcp approve` command to run first; approval
works before any binding uses the server. Accepting a source version is not this approval.
Config and dir-source definitions need no extra approval. Agent-native tool permission remains separate and
comes too late to authorize server startup.

`ctxtra mcp approve REF` shows the origin and operational declaration and asks in the user's terminal, default No.
It connects to nothing and creates no credential key. Login offers the same separate consent before contacting an
unapproved server. Show refuses until approved. Unattended input and `--no-input` cannot grant approval.

Approval covers full identity, source origin and the operational definition: URL, headers, client ID, scopes and
secret names, or argv, environment, inherited-variable names and secret-variable names. Notes, the commit and the
config's new name for the server do not invalidate it. Approval lives separately from sign-in state in a versioned record. An explicit decline suppresses
that definition's repeated summary notice, not its state in diagnostics or any prefix-conflict warning.

Source review shows operational definition changes. Accepting an update is all-or-nothing. A changed endpoint or
command needs approval again, and its old credentials cannot go to the new target. Changing the user's binding can give an unchanged approved server to more folders without another approval. Existing proxies keep their startup
selection and definitions.

`ctxtra mcp revoke REF` withdraws approval, not credentials or provider grants. New connections refuse it. Close
existing sessions to stop their connections; immediate live revocation is not guaranteed. Logout does not revoke
approval. Trusted sources read their manifests live without either commit acceptance or this approval.

### The session's servers

- The proxy's working directory when the agent starts it is the session folder. The proxy resolves bindings for
  it with the same rules and the same config as the hook (the config folder's `config.yaml`), so `dirs`,
  `repos` with `subdirs`, and worktrees work the same. It never asks the agent for roots.
- The servers are fixed when the proxy starts, like the agents' own MCP servers. Changing directory, adding
  folders during the session (for example `/add-dir`), ctxtra-follow and config changes add or remove none.
- On resume, the agent starts the proxy in the folder of the resume command. Claude Code, Codex and Antigravity
  don't run the hook on resume, so after a resume from another folder, the instructions come from the old
  folder and the servers from the new one.
- The proxy offers only tools. It forwards tool lists (titles, annotations and schemas unchanged), calls,
  cancellation and progress, and a server's tool list changes. It doesn't forward a server's instructions,
  prompts or resources, and offers servers no roots.
- The proxy answers the agent's first tool list when every bound server has answered, or at the agent's
  deadline, with the tools of the servers that answered. The deadline counts from the proxy's start and is
  shorter than the agent waits for the list, because an agent that gives up first gets no tools of any server:
  1.5 seconds in Claude Code, 8 in Pi, 10 in the others (Codex waits that long because its entry is
  `required`). A server that answers later is added with a tool list change notification; agents that ignore
  it (Codex) don't get its tools in that session.
- ctxtra's own start, from the proxy's start until it has found the folder's servers, stays under 300 ms; a test
  checks it. Most of it is two git calls in the session folder: about 0.05 s in all on an idle machine, up to
  0.2 s while the agent starts next to it. The rest of the wait for the first list is reading each server's
  credentials (about 0.03 s from the keychain) and the servers' own start.
- A problem never breaks a session: with a broken config, a failing server or no bound server, the proxy starts
  and serves the tools it can, or none, and logs the problem. A server whose tool list has more than 2,000 tools
  or 8 MiB, or that pages without end, is left out. A server that ends during the session is stopped with the
  processes it started, and its tools go.

### Local servers

A local server runs as the proxy's child, in the session folder, in a process group of its own. Its
environment is only: its `env` values, its `pass-env` variables and secret values, and from the proxy's
environment `PATH`, `HOME`, `USER`, `LANG`, `TMPDIR`, `HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`, `ALL_PROXY` (each
also lower case), `SSL_CERT_FILE` and `SSL_CERT_DIR`. Its stderr is dropped: it can hold secrets, and agents show
none of it.

The program must be an absolute path or a name in `PATH`: a relative path could come from the session's
repository. A tool like `npx` still prefers a project's own packages, so a repository can change what such a
server runs.

When the agent closes the proxy's input, or the proxy gets SIGTERM, running calls end, and each local server's
process group gets SIGTERM, and SIGKILL after 2 seconds if any process of the group is still there, also a
server that is still starting. When the proxy is killed, its servers get the end of their input, as when an agent is killed: a server
that exits then stops.

### Network and logs

The hook never uses the network. The proxy connects only to the bound remote servers and their authorization
servers. It logs to ctxtra's log (`$CTXTRA_LOG` or `~/.local/state/ctxtra/log.jsonl`) with the agent's name: its start,
the session folder, the bound servers, the number of config problems (`ctxtra doctor` lists them), each
server's connection, late servers, servers that end during the session and how each local server's process
ended, and its own end. It never logs tool arguments, results, secrets or config
problems as text; a failed call is logged by its kind or code only, because error messages can repeat the
arguments. `doctor`'s last hook runs leave these lines out.

A remote server is reached with streamable HTTP, and with the older SSE transport when that fails. ctxtra
checks every request, not only the configured URL:

- Requests go only to the server's origin. A redirect to another origin is refused, and so is an SSE message
  endpoint on another origin. Tokens and secret headers go only to the server; refresh tokens and client secrets
  only to the authorization server's token endpoint, which never gets a redirect followed.
- HTTPS only. Plain HTTP only to loopback, for a server on loopback.
- Addresses are checked when connecting, so a DNS change can't get around the check. Link-local addresses,
  such as the cloud metadata address `169.254.169.254`, are always refused. Private addresses (RFC 1918, CGNAT,
  ULA) are allowed only when the server itself is on one, and loopback only when the server is on loopback.
- `HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY` and `ALL_PROXY` (also lower case) are honored, and the certificates of
  `SSL_CERT_FILE` and `SSL_CERT_DIR` are trusted next to the system's. Through a proxy, ctxtra checks the target's
  addresses as this machine resolves them, and refuses a name that doesn't resolve here unless it is the
  server's own. The proxy resolves the name again: a proxy that resolves it to another address isn't covered.

### Sign-in

A remote server that answers a request without a token with 401 and a Bearer challenge uses OAuth; an older
SSE server is asked with GET. Only `ctxtra mcp add` and `ctxtra
mcp login`, in the user's terminal, sign in; the proxy and the hook never do. Without a terminal, or with
`--no-input`, they fail and change nothing; `--no-login` adds the server without signing in.

Sign-in follows the MCP authorization spec (2026-07-28) for clients:

- Discovery: the server's protected resource metadata, where its 401 says or at the well-known places, names
  the authorization server. Without that metadata, as with Atlassian, ctxtra looks for authorization server
  metadata at the server's origin. The metadata must name the issuer it was found for, offer PKCE with `S256`,
  and use HTTPS for every endpoint.
- Scopes: the server's configured scopes; else those of its 401; else those of its protected resource metadata.
  ctxtra adds `offline_access` when the authorization server lists it, so that it issues a refresh token, and
  the scopes of the last sign-in and of a 403 `insufficient_scope`.
- Client: the user's own app (`--client-id`, and `--client-secret` for its secret), else ctxtra's client ID
  metadata document, else dynamic registration. ctxtra's document isn't hosted yet, so ctxtra registers
  dynamically. A server that offers neither, such as GitHub or Slack, needs the user's own app.
- The browser opens the authorization link (`$BROWSER`, else `open` or `xdg-open`). ctxtra waits for the
  redirect on `127.0.0.1`, on a random port or on `--callback-port`, which is stored for later logins. It
  accepts one redirect with the right `state` and refuses others without ending the sign-in; it waits 5
  minutes. The browser gets a self-contained page that says whether the sign-in succeeded and to go back to
  the terminal. It loads nothing, and it never shows the redirect's values, except the provider's error when it
  is an OAuth error code. With `--no-browser`, ctxtra prints the link and accepts the address the browser ends at, pasted in
  the terminal, for example over SSH.
- The answer's `iss` must be the issuer, and is required when the authorization server says it sends it. The
  token request sends the `resource` parameter. A failed check stores nothing.
- A dynamic registration is reused while the redirect address is the same, and made again for a new one.

After sign-in, ctxtra shows the issuer, the server's notes, and the account when the provider's answer names
one. A second server of the same issuer gets a note before the browser opens: the provider may use the
browser's active account, so a private window helps to sign in with another one. When no refresh token came,
ctxtra says the user signs in again when the sign-in expires.

In a session, a server that needs sign-in at its start has no tools; the proxy reads its state file every 3
seconds and adds its tools once it is signed in (Codex ignores the list change: a new session gets
them). A refresh token that the provider rejects, or a 401 after one refresh, fails the call with `run ctxtra
mcp login NAME`. A 403 `insufficient_scope` records the scopes it asks for, for the next login, and fails the call
the same way.

`ctxtra mcp login NAME` signs in again; for a server without OAuth, or with `--secrets`, it asks for the secret
values again. Use a source server's full reference, for example `login work:linear`. Login does not override a
source server's client ID. `logout REF` deletes local tokens and secret values, including the client secret, but keeps
client registration, callback-port settings and their stored binding, even when the selected config no longer declares
that identity or declares a different target. The provider isn't told. Reauthorization does not reset an agent
conversation or an already-open provider session; start new sessions after account changes. Local processes retain
old environment secrets until they exit. A shorter configured scope list does not revoke previously granted access.

### Sign-in state and missing tools

The session summary counts the selected MCP servers in its first
line, for example `✓ ctxtra added 10 instructions, 2 skills and 1 MCP server`.
An `MCP servers:` list follows the instructions and skills. It isn't grouped: a server from a source has an
identity of the form `SOURCE:NAME`, a server of the config just its name. It shows up to 30 identities in binding
order, then `+N more` and `Full MCP list: ctxtra context explain`.
The count includes servers awaiting sign-in or approval, and excludes suppressed identities.
With no selected servers, the summary stays unchanged. The hook reads only local state.

The session summary names each bound server whose state file says it needs sign-in, after an empty line, like
the [notices](#notices):

```text
⚠ linear_work needs sign-in: run ctxtra mcp login linear_work
```

The hook reads only the state files: no key, no keychain, no network. A server that no session has tried yet
has no state, and no line. A server that needs approval has its approval line instead. For a remote server, or
one with `secret-env`, that line also says it may need sign-in, since only a request tells whether it uses OAuth,
and names `ctxtra mcp login`, which asks for approval first. Deleting the state folder removes both approval and
sign-in:

```text
⚠ iam-team:jira needs approval and may need sign-in: run ctxtra mcp login iam-team:jira
```

The proxy records, per session folder, what the last session started there got from each bound server: its
tools in the first tool list, later (too slow for it, or signed in during the session; Codex and Muse don't
get those tools), or none, and why. It also records when it answered the agent's first tool list, and when it
had found the folder's servers (ctxtra's own start). Both count from the proxy's start; an agent that asks for
the list late adds its own wait. A server's time includes reading its credentials (the keychain, or a token
refresh), which the record keeps apart. `ctxtra mcp status [DIR]` shows that record with the folder's servers
and their sign-in state, from local files only:

```text
the last session here (codex, 2026-11-10 14:08:19):
  first tool list after 10.0 s (waits up to 10.0 s); ctxtra's own start took 0.05 s
  linear_work  ready after 0.4 s (reading its credentials: 0.03 s)
  pg  later: too slow for the first tool list, after 15.2 s; this session didn't get its tools
```

"waits up to" is the agent's deadline. A record of an older version has only that. Credential times under 10 ms
aren't shown.

The records are in `~/.local/state/ctxtra/mcp/status`, one per folder, so a session in one folder never changes another's.

The agent learns it too: the proxy's instructions (the MCP `instructions` field, which it sends after waiting
at most half a second for the folder's servers) name the bound servers and say to run `ctxtra mcp status` when
a tool of theirs is missing, and name each server whose state says it needs sign-in. Claude Code shows them in
the system prompt; Codex in the description of the `ctxtra-proxy` server's tools, so only when the session has at least
one of them.
`ctxtra mcp list` and `doctor` show each server's sign-in state and where the key of the credentials is, with the
warning for a key file.

### Credentials

A server's secret values, its OAuth client registration and its tokens are stored in `~/.local/state/ctxtra/mcp`, never in
the config or in an agent's config. ctxtra asks for secret values in the terminal without showing them, or
reads them from stdin, one per line.

| Path | Content |
|---|---|
| `credentials/<server>.v1.age` | The server's credentials, encrypted. `v1` is the format version. |
| `state/<server>.json` | No secrets: binding, whether the server is signed in, and why it needs sign-in. |
| `approval/<server>.json` | Machine-local approval or decline and its versioned operational fingerprint; no credentials. |
| `approval/<server>.lock` | Separate lock for approval writes and reads; never held during credential or network work. |
| `generations/<server>.json` | Secret-free generation marker that keeps pending authorization invalid after removal. |
| `locks/<server>.lock`, `locks/.store.lock` | Locks for changes of one server's files, and for creating the key. |
| `key.json` | No secrets: where the key is, `keychain` or `file`. |
| `key` | The key, only when `key.json` says `file`. |

The folder is readable by the user only (0700), and the files too (0600). Every save replaces the file in one
step and is synced to the disk before ctxtra uses the new values: a failed save fails the command or the
call. Credential readers wait for a writer to finish, including any rollback. Approval readers fail closed without
waiting when an approval write is in progress, so hooks never wait for credential or network work.

The key is an [age](https://age-encryption.org) X25519 key in the OS keychain: the login keychain on macOS, the
Secret Service on Linux (service `ctxtra`, account `mcp-credentials`). Without a usable keychain, the key is the
file `key`, and ctxtra warns that any process of the user can read the credentials then. Only the commands that
set secret values or sign in create the key, and they record its place in `key.json`. Every other command and
the proxy use that place and never create or move the key. When the keychain that holds the key can't be
reached, for example over SSH or without a session bus, ctxtra says so and doesn't fall back to a file. A
file key stays the key when a keychain becomes usable later. The proxy waits for the keychain at most 3
seconds, and on Linux never unlocks a locked Secret Service collection, which would show a prompt. macOS can't
read a locked keychain without a prompt; the login keychain is unlocked while the user is logged in. When the
proxy can't get the key, the servers that need it are left out. When the
key is gone, the credentials can't be read: the next command that creates the key moves them to
`credentials/unreadable`, and their servers need their values or sign-in again.

Each credential record binds full identity, source origin and canonical URL or exact command line. A mismatch sends
nothing and needs new authorization or secret values. Config records have an empty origin. Repo-source origins use the normalized repository address,
local repo paths use their real path, and dir origins use the real folder. Commit is excluded.
Renaming a source or replacing its origin requires new credentials.
No command copies rotating refresh grants between identities.

Server declarations with the same identity, origin and target share credentials. Use different names or
source aliases for separate accounts. Add rejects a conflicting stored binding before changing config, secrets or
callback settings. For a matching stored binding, add reuses the shared record without starting authorization, and
reports that reuse. It asks only for declared secret names the record has no value for, and merges them into the
record under the generation check. It never replaces stored values or tokens. Without those values, for example with
`--no-input` or empty stdin, add fails and changes nothing. A retained registration alone is not reported as usable
credentials. Add rejects explicit client-secret or callback-port changes; use login after declaring the definition
to change shared credentials. Login alone can replace a conflicting binding after explicit terminal consent
explaining the shared effects.
Binding and generation checks run under the same identity lock as the credential write, so an intervening sign-in
cannot be silently replaced. Add keeps that lock while writing config, then saves secret settings only if the config
write succeeds. If the settings save fails, it restores the previous config; a failure to restore it is reported.
Login stages requested secret values until all prompts and OAuth succeed, then saves them with the new tokens in one write. Cancelling or failing replacement leaves the old credential record unchanged.
A versioned, secret-free generation marker survives removal. It invalidates pending authorization even when no
credentials existed yet or the removed legacy record had generation zero. Generations never move backwards,
even after removal and re-addition or when the clock does not advance. Markers do not count as stored credentials.
An interrupted authorization can be retried with login, not add. A definition saved before OAuth fails remains;
command success alone is not proof of a completed sign-in. Human-readable server descriptions, notes and sign-in
metadata escape terminal control characters. JSON catalog output preserves the metadata values.

Removing a config server deletes its credentials and reports dangling bindings. A source's server definitions are
read-only to mcp remove. Removing a source keeps the binding entries that use it: each is reported, and stays an error
until the user removes it. The command lists cleanup commands and keeps the source's credentials for explicit cleanup.
`mcp remove SOURCE:NAME` can delete leftover credentials once that
identity is undeclared in the config. Existing sessions can still use those credentials until restarted.
It is not provider-side grant revocation. Logout without a stored credential record only advances cancellation
history and clears local sign-in state. It does not create a placeholder binding.

Sessions in every agent refresh a server's tokens at the same time safely: a refresh holds that server's lock
while it reads the credentials again, asks for new tokens and saves them, and other sessions wait for it and
use the new tokens. Reads of credential generations and usable tokens take shared locks and retain the cached
encryption key. They do not create authorization markers. An unexpired token without a refresh grant stays usable
until its expiry. Refresh attempts and credential or sign-in state changes take the exclusive lock.
The locks use `flock`, which can be unreliable on a network home folder (NFS).

The hook never reads the credentials or the key. Like the agents' own MCP sign-ins, the credentials protect
against backups, sync tools and casual reads, not against the user's own processes: any process of the user
can read the key, through the keychain's command-line tool or the key file.

The file names, the format version in them, the lock paths and the state file's meaning are a contract between
ctxtra versions, so sessions of an older and a newer ctxtra can refresh the same server. Within a format
version, ctxtra only adds fields and keeps the fields it doesn't know. An older ctxtra that finds credentials in
a newer format fails that server's calls with an upgrade hint, and changes nothing.

### Setup

`ctxtra setup` always adds the entry to each MCP-supported agent it sets up, even with no servers in the
config or sources. A server bound later reaches the next session without another setup, with one exception:
Codex passes the proxy only the variables in its entry's `env_vars`, so a local server with a new `pass-env`
variable needs `ctxtra setup --agent codex` again. The Codex session message, the setup notices and `doctor` say
so; the hook reads only Codex's config file for it. Setup never copies
the user's servers into agent configs. `--remove` removes the entry.

| Agent | File | Entry |
|---|---|---|
| Claude Code | `~/.claude.json` (`$CLAUDE_CONFIG_DIR/.claude.json`) | `mcpServers.ctxtra-proxy: {type: stdio, command, args}` |
| Codex | `~/.codex/config.toml` (`$CODEX_HOME`) | `[mcp_servers.ctxtra-proxy]`: `command`, `args`, `env_vars`, `startup_timeout_sec`, `required`, between marker comments |
| Cursor | `~/.cursor/mcp.json` | `mcpServers.ctxtra-proxy: {command, args}` |
| OpenCode | `~/.config/opencode/opencode.json` | `mcp.servers.ctxtra-proxy: {type: local, command, pool: false}` |
| Pi | `~/.pi/agent/mcp.json` | `mcpServers.ctxtra-proxy: {command, args}` |
| Muse | `~/.config/muse/settings.json` (`$XDG_CONFIG_HOME/muse/settings.json`) | `mcpServers.ctxtra-proxy: {type: stdio, command, args, mode: optional, env}`; top-level `schema_version: 1` when absent |
| Antigravity | `~/.gemini/config/mcp_config.json` | `mcpServers.ctxtra-proxy: {command, args}` |

Muse clears the environment of hooks and MCP servers. Setup records the four `XDG_*` values in its hook
commands and MCP entry. Both processes then find the same folders as the shell that ran setup. `doctor`
reports a recorded value that differs from the current shell, with `ctxtra setup --agent muse` to refresh it.
Setup refreshes an existing proxy entry even when missing source content leaves no server catalog to load.

Muse's MCP entry also copies setup-time `CTXTRA_LOG` and `CTXTRA_NO_UPDATE_CHECK` when set.
Setup expands `~` and makes the log path absolute. Setup owns these keys and the four XDG keys inside `env`,
and keeps user-added keys. It does not copy secret `pass-env` values, proxy or certificate variables,
or Linux Secret Service variables. Use explicit non-secret `env` settings or encrypted `secret-env`
declarations for local servers. Start a new session after signing in. Hooks use the log in the state folder.
Background checks inherit the hook's XDG values.

Agents write these files themselves too (Claude Code often writes `~/.claude.json`): when a file changes
between setup's read and its write, setup makes its edit again on the new content, so the agent's change stays.

The command is the ctxtra binary and its arguments are `mcp proxy AGENT`. Setup owns only the keys in the table
and keeps every other key the user adds to the entry, such as an approval setting. An entry named `ctxtra-proxy`
that doesn't run `ctxtra mcp proxy` is the user's: setup and `--remove` leave its file alone and say so.

Earlier versions named the entry `ctxtra`. Earlier setup entries are not served; run setup again.
Setup renames it to `ctxtra-proxy`, in its place and with the keys the user added (in Codex, with its tables
and markers too). When both names are there, setup removes the old entry. `--remove` removes either. An entry
named `ctxtra` that doesn't run `ctxtra mcp proxy` is the user's, and setup leaves it alone.
Setup keeps existing native provider entries, so an agent can show both native and ctxtra tools for the same provider.
Agent tool permissions are separate from connection approval. An entry-wide allow rule covers servers bound later.
A short-prefix rule can match different source identities and accounts in different folders; use the full identity
in `ctxtra mcp status DIR` to check which server a folder selects.

In Codex, the entry is a block between `# >>> ctxtra MCP proxy` and `# <<< ctxtra MCP proxy` lines. Setup
rewrites only its own keys' lines in the block, and keeps the rest of the file byte for byte. `--remove` removes
the block, keys you added to ctxtra's table after it, and `[mcp_servers.ctxtra-proxy.*]` tables. Before it writes,
it checks that the new file is valid TOML and says the same as the old one but for those keys; otherwise it
changes nothing, because Codex refuses an invalid config. Codex starts MCP servers with an allow-listed
environment, so `env_vars` lists the four XDG folder variables, `CTXTRA_LOG`,
the Secret Service's (`DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR`), the proxy and certificate variables, and
every local server's `pass-env` names. `startup_timeout_sec` is 30, and `required = true`: Codex waits about 1
second for an optional server's tools, and for a required one up to `startup_timeout_sec`. A required server that
can't start stops the Codex session, so a moved or deleted ctxtra binary does too, until `ctxtra setup` runs again;
the setup notices report it.

OpenCode's entry isn't pooled, so its shared service starts one proxy per session folder.

The entries, the keys setup owns, and the arguments of `ctxtra mcp proxy` are part of the setup contract (see
[Setup compatibility](#setup-compatibility)): every later version accepts them, and the proxy ignores arguments
it doesn't know.

The setup notices and `doctor` report the entry:

| When | Notice |
|---|---|
| A hook setup is missing its proxy | `claude-code: the ctxtra MCP proxy is missing; run ctxtra setup --agent claude-code` |
| The entry runs another binary | `claude-code: the ctxtra MCP proxy runs /usr/local/bin/ctxtra, not this ctxtra (/opt/homebrew/bin/ctxtra); to switch, run ctxtra setup --agent claude-code` |
| The entry is gone from its file | `claude-code: the ctxtra MCP proxy is gone from ~/.claude.json; if that wasn't on purpose, run ctxtra setup --agent claude-code; ...` |
| Codex's entry doesn't pass a local server's `pass-env` variable | `codex: the ctxtra MCP entry doesn't pass DBHOST to the proxy; run ctxtra setup --agent codex` |
| The entry has its old name | `claude-code: the ctxtra MCP proxy has its old name ctxtra; run ctxtra setup --agent claude-code` |
| An entry named ctxtra-proxy isn't setup's | `~/.claude.json: the MCP server ctxtra-proxy doesn't run ctxtra mcp proxy; rename or delete it, then run ctxtra setup --agent claude-code` |

### Security

ctxtra holds the sign-ins of many services, the most sensitive data it keeps. What protects them, and what
doesn't:

- **Folder scoping prevents mistakes; it isn't a security boundary.** A binding decides which servers a
  session gets, so an agent doesn't use the wrong account by accident. An agent with a shell can still run
  `ctxtra mcp proxy` in another folder, or run `ctxtra mcp` commands, and use every server of the config.
- **Any process of the user can read the credentials,** through the keychain's command-line tool or the key
  file. With the key in the keychain, encryption protects them from backups, sync tools and casual reads, as
  the agents' own sign-ins are. With a key file, a backup or sync of `~/.local/state/ctxtra/mcp` holds the key too, and
  whoever gets it can read every credential.
- **The proxy sees every call and its result,** and forwards them. It logs neither: only which servers
  started, how they ended, and failed calls by their kind or code, with URLs cut to their origin.
- **Credentials stay on their origin** and go only over HTTPS, or plain HTTP to a server on loopback: see
  [Network and logs](#network-and-logs).
- **Local servers get little of the agent's environment:** their own variables, the fixed base set, and the
  variables they name in `pass-env`. A token in the agent's environment reaches a server only when that
  server names it, or when it is part of a base variable, such as a proxy URL with a password. They run in the session folder, and a tool like `npx`
  can run code of the session's repository.
- **`ctxtra mcp show` prints a local server's stderr** in the terminal, which can hold secrets. An agent that
  runs it reads that output. `ctxtra mcp status`, which the proxy's instructions point agents to, shows only
  a server's origin or program name, never a URL's path, its query or a command's arguments.
- **Repo-source servers need local approval.** A source catalog can declare programs. The proxy runs them only
  after explicit machine-local consent to the operational definition. Bindings, in the user's config, and source
  collections select servers but cannot declare or override them. Config and dir-source definitions do not need
  this extra approval.
- **A problem in the proxy never fails a session:** a broken config, a failing server or missing credentials
  cost the tools they concern. Only a proxy that can't start at all, such as a moved ctxtra binary, stops a
  Codex session, whose entry is required.
- **ctxtra's client identity.** When ctxtra's client ID metadata document is used, it is the identity that
  authorization servers show and trust for ctxtra, and whoever controls its domain controls that identity.
  Until the domain exists, ctxtra registers itself with each authorization server instead.

### Tool names

A session sees each tool as `<server>__<tool>`. The name uses only `A-Z`, `a-z`, `0-9` and `_`, and is at most
38 characters: Muse shortens longer tool names and then can't call them, and the strictest model APIs allow 64
characters, of which Claude Code and Pi use 19 for `mcp__ctxtra-proxy__` (Pi writes `mcp__ctxtra_proxy__`).

- A server name is 1 to 20 characters of `a-z`, `0-9` and `_`, starts with a letter, and has no `__` and no `_`
  at the end.
- A tool name that has other characters, or makes the whole name too long, is shortened: other characters become
  `_`, the name is cut to fit, and `_` and 6 hex digits of a SHA-256 hash of the server and the tool name are
  added. The cut never goes into `<server>__`.
- The name depends only on the server and the tool, so it is the same in every session, agent and machine, and
  the agents' permission rules keep working. Only when two names in one session would be equal, the shortened
  ones get longer hashes: 10 digits, then up to 16.
- A shortened tool's description starts with its server and its own name: `[gh: create-issue] Create an issue.`

| Server | Tool | Session sees |
|---|---|---|
| `linear_work` | `list_issues` | `linear_work__list_issues` |
| `gh` | `create-issue` | `gh__create_issue_ef677c` |

## Setup compatibility

What `ctxtra setup` writes is a contract between the agents and the `ctxtra` binary: the hook entries
(events, matchers, command lines, the slot count, timeouts, other keys such as `additionalContextLimit`), the
plugin files (the events they send, their input fields, the answer fields they read, their constants), and the
arguments of `ctxtra hook`. A new version changes only the binary, and the binary keeps serving every setup
that a released version wrote with what that setup was promised.

This release starts the contract over. Entries from earlier releases are not served, including entries for
agents whose names did not change. Run `ctxtra setup` again. Setup recognizes earlier entries by their
commands and replaces them, so an agent never runs both. `doctor` reports each earlier entry with its
`ctxtra setup --agent NAME` command and exits 1. When `~/.ctxtra` exists, it says that folder can be deleted
once every agent is set up again. The earlier state in that folder is not read or converted.

Agent IDs are `claude-code`, `github-copilot` and `hermes-agent`; the earlier names `claude`, `copilot` and
`hermes` are not aliases. `codex`, `cursor`, `opencode`, `pi`, `antigravity`, `devin`, `muse` and `dsh` keep
their names. These IDs apply to `--agent`, `ctxtra hook` and `ctxtra mcp proxy`, and to import's `--start`.
Setup writes `--setup-version=1` in hook and proxy commands and marks managed plugins with that version.
Commands without the marker add nothing. Contract and upgrade fixtures start with these setups.

From this baseline, an upgrade keeps existing setups working with the same capacity. New features may need another
`ctxtra setup`; a capacity increase may need it too. In either case, the release notes explain when to
run setup, and `ctxtra doctor` reports what the existing setup is missing. In Codex, changing the hooks
also requires trusting them again.

The MCP entry (see [MCP servers](#mcp-servers)) and the arguments of
`ctxtra mcp proxy` are part of the contract too.

Answers only grow: fields are added, never renamed or given a new meaning, and a field that a released setup
reads is sent while that setup is supported. The hooks and plugins ignore fields they don't know. The
arguments and event names that a released setup sends are accepted forever, and `ctxtra hook` ignores
arguments it doesn't know, so an older binary still runs a newer setup.

### Guided onboarding

`ctxtra onboard` is the interactive getting-started flow. It previews the hook file changes
and asks before installing hooks (with the same backups and agent detection as `ctxtra setup`),
explains and offers each bundled skill that isn't already bound, then offers instruction
import if no binding adds instructions yet.
When the ctxtra skill is bound and an installed agent can be started, it asks whether to
start one and lets the user choose among available agents. Import uses the same backups,
prompt and launcher as `ctxtra import --start AGENT`. Declining a step leaves it unchanged.
Declining hook installation stops onboarding before any changes. Horizontal rules separate
the remaining steps from the preceding output. EOF never accepts a question. Rerunning
onboarding skips skills that are already bound.

Onboarding requires terminal input and output and rejects `--no-input` before making changes.
Scripts and agents use `setup`, `skill install` and `import` separately. `setup` remains
non-interactive and prints pending commands plus a pointer to guided onboarding.

### Setup notices

After every command that runs, but `hook`, `setup`, `onboard`, `doctor`, `source validate`, `check-updates`, `help`, `version` and `completion`, ctxtra
prints a line on stderr for each setup that needs you, with the command that repairs it. `--help`, usage
errors, a command run with `--dry-run` and a `source` command that fails print none: they change nothing,
and the notices would. The lines never change the exit code; `doctor`
shows the same lines under each agent instead:

| When | Notice |
|---|---|
| The binary in the setup is gone | `claude-code: the hooks run ~/Downloads/ctxtra-0.3/ctxtra, which doesn't exist; run ctxtra setup --agent claude-code` |
| The setup runs another binary | `claude-code: the hooks run /usr/local/bin/ctxtra, not this ctxtra (/opt/homebrew/bin/ctxtra); to switch, run ctxtra setup --agent claude-code` |
| An earlier release set it up | `claude-code: SessionStart hook 0:0 in ~/.claude/settings.json is from an earlier ctxtra; run ctxtra setup --agent claude-code` |
| Part of the setup is missing | `claude-code: SessionStart has 3 of the 40 ctxtra hooks; run ctxtra setup --agent claude-code`, `dsh: the patch row that loads ctxtra's plugin is missing; run ctxtra setup --agent dsh`, `claude-code: ctxtra's plugin, which offers the skills as slash commands, is missing from ~/.claude/skills/ctxtra; run ctxtra setup --agent claude-code` |
| The Hermes plugin isn't enabled | `hermes-agent: the ctxtra plugin is installed but not enabled; run hermes plugins enable ctxtra` |
| A setup is gone from its file | `claude-code: ctxtra's hooks are gone from ~/.claude/settings.json; if that wasn't on purpose, run ctxtra setup --agent claude-code; if it was, ctxtra setup --remove --agent claude-code stops this notice` |
| An agent was found since the last setup | `cursor: found since the last ctxtra setup; to add ctxtra to it, run ctxtra setup --agent cursor` |

A new agent is told about once: ctxtra then adds it to `detected` in `~/.local/state/ctxtra/setup.json`, unless a setup
is running or the file can't be written, and then tells again next time. `doctor` still lists it as not set
up. Each command names its agent, so a repair doesn't set up agents you left out, and a command that changes
Codex's hooks says that Codex will ask to trust them. The binary isn't compared when this ctxtra runs from a
temporary folder (`go run`). Apart from that record, the notices only read files: regular files of at most
16 MiB, so a FIFO or a huge file in place of a config is one line that it can't be checked, never a command
that hangs.

`setup` records in `~/.local/state/ctxtra/setup.json` where it set up each agent (`installed`, by config path, until
`setup --remove` removes it), and every agent it found (`detected`, also those left out with `--agent`). The
notices use it to tell a setup that is gone from one that never was, and to find new agents (and add the ones
they told about to `detected`): without the file there are no such notices, and a broken one is one notice, `~/.local/state/ctxtra/setup.json is unreadable: notices
about new or removed setups are off until the next ctxtra setup`. The file never decides a change of an
agent config. One setup runs at a time (`~/.local/state/ctxtra/setup.lock`); `--dry-run` doesn't write the file. When
setup can't write it, setup fails with exit code 1, and the config changes it made stay.

## Validation

Every mistake is an error, reported in the session message where available, the log, `context explain`
and `doctor`. Doctor exits 1. The session message counts the errors and names the first three, each on a line
that starts with ⚠: first those with a command that fixes them (a profile, a source that can't be read, a repo set,
a server to approve), then the others as `doctor` writes them, with their file and line. A last line says how many
more errors `doctor` shows. Each line is one line of at most 240 characters, with control characters escaped. A longer
line is cut before its end, which keeps the command to run or what is left out; a name too long to repeat there is
left out of it ("the binding is left out"). `doctor` and the log have the whole text. A bad entry leaves out the smallest usable part:

| Error | Left out |
|---|---|
| Wrong schema, invalid YAML, several documents, anchors, aliases or merge keys | The whole file. |
| Duplicate key | The part containing it. |
| Unknown config key | That key. |
| Broken source or unusable lock | Its source content. |
| Broken binding, unknown agent, invalid profile or empty condition | The binding. |
| Duplicate binding name | Both bindings. |
| Missing file, unmatched pattern, unknown source or path leaving a source | That entry. |
| Broken positive location entry | It remains and matches nothing. |
| Broken negative location entry | The binding. |
| Repo set that can't be read | In a positive entry, nothing: it matches nothing. In a `!` entry, the binding. |
| Repo set entry that isn't a repository, or a bad pattern | That entry; the rest of the set applies. |
| Untrusted file or unsafe symlink | The file. |
| Manifest containing sources or bindings | The whole manifest. |
| Unapproved repo-source MCP server | The server. |
| Over capacity | The session gets what fits and reports the error. |
| Invalid name in `CTXTRA_PROFILES` | That name. |
| Stored profile selection that can't be read | The stored selection. |
| Active profile that no binding uses | Nothing: the other profiles apply. |

Text values must be YAML strings; a number or boolean where text is needed is an error that says to quote it.
YAML 1.2 words such as `on` and `no` are strings. Quote entries starting with `!`; YAML tags are errors.
Missing local source folders are not errors. Same-named skills or servers are notices.
A command that would write an error exits 1 and changes nothing.

## Command line

`ctxtra help` lists the commands, and `ctxtra help COMMAND` (the same as `ctxtra COMMAND --help`) shows one
with its flags and examples. The commands that change the config are grouped by what they change:
`source`, `binding`, `collection`, `repo-set` and `mcp`, each with `add`, `list` and `remove`, and
`config set` and `unset` for program settings. `profile` changes the stored selection, never the config.

- Exit codes: 0 when the command did what it was asked; 1 when it failed, or when `doctor`, `mcp status` or a `context`
  command found that a session wouldn't get everything it should (for `doctor`, a session in any folder),
  `source validate` found an error, or a check of `check-updates` failed; 2 for a usage error (an unknown command
  or flag, a missing argument), with the command's usage hint on stderr.
- Results go to stdout; errors, warnings, notices and questions go to stderr. In a report (`doctor`,
  `context explain`, the review of `source update`, `source validate`), its errors and warnings are part of the result, on
  stdout.
- `--json` (on `doctor`, `context files`, `context explain`, the `list` commands, `source show`,
  `source update --dry-run` and `source validate`) prints one JSON object: `schema_version`, `data`, and `diagnostics`, a list of
  `{severity, message}` with the severity `error`, `warning` or `notice`. New fields can be added; a field
  doesn't change its meaning within a `schema_version`.
- ctxtra asks a question only when stdin and stdout are a terminal. `--no-input` turns every question off:
  a step that needs an answer then fails. No option answers a question for you.
- `--dry-run` on the commands that change the config, the agents' setup or the installed skills checks the
  change and shows it, and writes nothing. `source update --dry-run` may fetch into the cache and writes
  nothing else; it accepts and dismisses nothing.
- Paths in arguments are relative to the current folder; in the config they are relative to the config's
  folder.

## Key lengths

The keys ctxtra creates meet NIST's minimums through 2030: credentials are encrypted with age, which uses an
X25519 key and ChaCha20-Poly1305 (128 bits of security or more), and sign-in uses PKCE with SHA-256.

**HTTPS.** MCP servers, their authorization servers and the release check use TLS 1.2 or newer, and Go's
certificate checks. By default, ctxtra trusts the system's certificates. Public certificate authorities don't
issue certificates with RSA keys under 2048 bits, or elliptic curves under P-256. Go refuses RSA keys under
1024 bits. Smaller keys than the minimums are possible only under a certificate you trust yourself, through
`SSL_CERT_FILE`, `SSL_CERT_DIR` or the system.

To refuse them completely, run ctxtra in Go's FIPS 140-3 mode:

- Set `GODEBUG=fips140=on` where your agents start, for example in your shell profile. An agent that clears
  the environment, such as Muse for its hooks, drops it; the release check started from such a session then
  uses the defaults.
- Or build ctxtra with it on by default: `GOFIPS140=latest go install github.com/ctxtra/ctxtra/cmd/ctxtra@latest`.

In this mode, every certificate of a server's chain needs an RSA key of 2048 bits or more, P-256, P-384,
P-521, Ed25519 or ML-DSA. The mode changes more than key lengths, so a server may need to support it:
connections use AES, not ChaCha20; key exchange uses P-256, P-384, P-521 or a hybrid with ML-KEM, not X25519
alone; and TLS 1.2 needs extended master secret. Don't use `fips140=only`: it refuses X25519, so ctxtra
can't read or save credentials.

**Git.** ctxtra runs your `git` (see [How ctxtra runs git](#how-ctxtra-runs-git)), so its TLS library and
your ssh decide which keys a server may use. To refuse RSA keys under 2048 bits over ssh, set
`RequiredRSASize 2048` in `~/.ssh/config` (OpenSSH 9.1 or newer). Over HTTPS, see the settings of git's TLS
library, for example OpenSSL's security level 2.

A lock entry's `commit` is a git object ID, so its hash is the repository's object format: SHA-1 (160
bits) in most repositories, SHA-256 in SHA-256 repositories. ctxtra supports both. By default, git computes
SHA-1 with collision detection, which catches the known collision attacks.

## Limits

| Limit | Value |
|---|---|
| Any one file (config, instruction file, `SKILL.md`) | 4 MiB |
| Everything read for one session, the config included | 16 MiB |
| Text an agent gets in full | its capacity, see [Capacity](#capacity) |
| Skill description | 1024 bytes, longer ones are cut and end with `…` |

## Reference

| Config key | Meaning |
|---|---|
| `schema` | Required: `ctxtra/config/v1`. |
| `sources.NAME.repo` | A repository, read at accepted commits. |
| `sources.NAME.branch`, `sources.NAME.tag`, `sources.NAME.rev` | Exactly one: the branch or tag to follow, or the full commit ID to stay at. |
| `sources.NAME.subdir` | The folder of the repository that is the source's top. |
| `sources.NAME.dir` | A live folder; mutually exclusive with repo. |
| `sources.NAME.check-updates` | Repo-source checks; default true. |
| `mcp-servers` | The config's own server definitions. |
| `mcp-servers.NAME.use` | A new name for a source's server: `'SOURCE:SERVER'`, with no other key. |
| `bindings` | Ordered bindings with unique names. |
| `max-ctx-chars` | Integer at least 0; 0 means no configured limit. |
| `check-releases` | Release checks; default true. |
| `session-message.max-instructions`, `session-message.max-skills` | Per-source list limits; default 30, 0 hides the list. |
| `collections` | The config's own collections; see [Collections](#collections). |
| `repo-sets` | The config's own repo sets; see [Repo sets](#repo-sets). |

| Binding key | Meaning |
|---|---|
| `name` | Required, unique in the config. |
| `location.dirs`, `location.repos`, `location.repo-sets` | Folder or repository conditions. |
| `profiles`, `agents` | Additional conditions, AND with location. |
| `collections`, `instructions`, `skills`, `mcp-servers` | Content to add; at least one content list must add something. |

The session folder is the hook input's `cwd`, then Cursor's first `workspace_roots` entry,
then Antigravity's first `workspacePaths` entry, then the hook's working folder.
DSH's home is `$DSH_HOME` or `~/.dsh`. Muse records all four XDG values during setup.
`CTXTRA_LOG` overrides the log path. `CTXTRA_PROFILES` sets the active profiles for one launch.
