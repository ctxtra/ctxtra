# ctxtra tasks

The rules and warnings in the ctxtra skill's `SKILL.md` apply to every step.

## First setup

For a user who wants to walk through setup in their own terminal, suggest `ctxtra onboard`.
It sets up hooks, offers the bundled skills, then offers to start an installed agent to help
import existing instructions. When you perform setup for the user, use the steps below.

1. `ctxtra version`. If it fails, ctxtra isn't installed: show the user the install steps from the
   project's README and stop.
2. `ctxtra doctor`: shows the config path, whether a config exists, and which agents have hooks.
3. Ask what the user wants in their sessions, and where: their own instructions in every session, rules for some
   repositories, a source repository. Ask for the files. Don't invent instruction files for them.
4. Add the first binding with `ctxtra binding add` (see "Add or change a binding" below). It creates the
   config.
   Start with one binding and add more later.
5. `ctxtra setup --dry-run`, then `ctxtra setup` (rule 8 in `SKILL.md`). Tell the user the extra steps that
   setup prints, for example trusting the hooks in Codex or enabling the Hermes plugin. Setup also prints
   next steps: if the user wants this skill in their sessions, run `ctxtra skill install ctxtra`.
6. Check with `ctxtra context files` and `ctxtra context explain` in a folder where the files should apply.
7. Tell the user: new sessions get the files. A running session, also this one, keeps what it got at its
   start.

## Import existing instructions

Read and follow `import.md` in this folder.

## Add or change a binding

1. Run `ctxtra doctor` to find the one config. Bindings live only there.
2. Choose `location` by what the user means: omit it for every session; use `repos` for repositories
   wherever cloned, including worktrees, or `dirs` for folders. Both are alternatives inside location.
   `agents` and `profiles` are additional conditions; preview them with `ctxtra context explain DIR
   --agent ID --profile NAME`. Every condition a binding has must hold.
   To use a source's named set, use `--collection SOURCE:NAME`; a collection of the config itself is named
   without `SOURCE:`. For the repositories a team lists, use `--repo-set SOURCE:SET`, or a bare name for the
   config's own repo sets.
3. List files and skill folders by name or use quoted glob patterns. Use `ctxtra context files` or
   `ctxtra context show` to check the expansion. Local globs include new matching files immediately; repo-source globs change
   after accepting a commit. A skill folder needs a `SKILL.md` with a `description` in its frontmatter.
   To leave some out, add an entry that starts with `!`, quoted: `'!agents/drafts/**'`.
4. A new binding in the config: `ctxtra binding add NAME --instruction FILE --skill DIR`, with
   `--dir PATTERN`, `--repo PATTERN` or `--repo-set SET` to limit its location, `--profile` and `--agent`
   for the other conditions, and `--collection` and `--mcp-server` for more content (each flag can repeat).
   `--source SOURCE` makes the content values names or paths in that source. Quote patterns and `!`
   entries, or the shell reads them. A new binding goes last, or where `--before NAME` or `--after NAME`
   says. It writes only a binding whose files exist and are trusted, and says what is wrong otherwise.
   For a shape the flags can't write, give the binding in YAML with `--file FILE`; `--replace` changes an
   existing binding in its place. Read `ctxtra help config` first.
5. Run `ctxtra context explain DIR` in a folder that should match, and in one that shouldn't.
6. To remove a binding of the config: `ctxtra binding remove NAME`.

## Add a source repository

1. Give the warning from "Remote repositories" in `SKILL.md`, and offer to read the repository first.
2. Tell the user that sessions check for new commits by default (`references/updates.md`).
   With their OK, run `ctxtra source add --repo URL`, with `--check-updates=false` if they want checks off.
   Add `--tag TAG` or `--rev REV` to stay at a version, and `--subdir PATH` for one folder of a shared
   repository. It writes the source and its accepted commit in `ctxtra.lock`, but adds no binding. It
   lists the manifest's collections and repo sets, with one example `binding add` line.
3. Bind what the user wants: the line from the team's README, which uses `--source URL`, or
   `ctxtra binding add NAME --collection SOURCE:NAME` or `--instruction SOURCE:PATH --skill SOURCE:DIR`,
   plus the location flags. Source manifests cannot target sessions.
4. With checks off, tell the user to run `ctxtra source update` in their terminal now and then.

## Set up another machine

1. Copy the config and its `ctxtra.lock` to `~/.config/ctxtra`. Without the lock, every repo source is
   inactive until the user accepts a version again.
2. `ctxtra source download` gets exactly the accepted commits. It asks nothing and never changes the lock.
3. `ctxtra setup --dry-run`, then `ctxtra setup` (rule 8).
4. Trust, approvals and sign-ins stay on each machine. Tell the user to run `ctxtra source trust NAME` for
   live folders with remotes, `ctxtra mcp approve REF` for source servers, and `ctxtra mcp login REF` in
   their own terminal (rule 3).
5. `ctxtra doctor` lists what is still missing. A local source folder that isn't on this machine is no
   error.

## A session didn't get a file, or got the wrong one

1. `ctxtra context explain DIR` in the session folder: does the binding match, and why not. Check for
   dropped files too: `max-ctx-chars` drops whole files from the end. `ctxtra context files DIR` lists
   what remains and names dropped files on stderr. The `profiles:` line says which profiles are active;
   add `--agent ID` for a binding with `agents`. Read `ctxtra help config` for `max-ctx-chars`.
2. `ctxtra doctor DIR`: errors (missing files, untrusted repositories, sources not downloaded or without a
   matching lock entry, servers to approve),
   dropped files, hook setup for the agent, and whether the hook ran in that folder. A `ERROR:` line that
   names a binding can be about a binding that doesn't match DIR.
3. Check the agent's limits in `ctxtra help agents`: files can also be dropped to fit the agent's
   capacity. The session-start message and hook log name them; `ctxtra doctor DIR` shows recent hook runs.
   Some agents show the model only the start of a long text, or don't add it again in a resumed session.
4. If the file is bound to another repository than the session folder's, for example a session started in
   one repository that then worked in another: the session gets only what its own folder matches. The
   optional `ctxtra-follow` skill gets the other repository's files when the agent works there. Offer
   `ctxtra skill install ctxtra-follow`, and say that the agent decides when to use it.

## Remove something

- `ctxtra source remove NAME` removes a source and its lock entry, and reports bindings that still use it.
  Fix those references with the user.
- `ctxtra binding remove NAME`, `ctxtra collection remove NAME`, `ctxtra repo-set remove NAME` and
  `ctxtra mcp remove NAME` remove one entry of the config. `ctxtra profile clear` empties the stored
  profile selection.
- To stop ctxtra in all agents: `ctxtra setup --remove`.

## Write an instruction file or a skill for the user

Instruction files are plain Markdown. Skills are folders with a `SKILL.md` that starts with YAML
frontmatter with a `description`. Neither needs anything specific to ctxtra. Keep the files where the user
keeps their own files, not in a repository other people push to, unless that is what the user wants.
Keep them short: everything bound is added to every matching session, and `ctxtra context explain` shows
the size.
