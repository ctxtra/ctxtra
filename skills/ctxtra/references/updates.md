# Update checks

Sessions show notices (lines starting with `ℹ`) from background checks. Checks run at most once an hour
and read only a version number or a commit ID. They never change what a session gets, and never accept anything.

- **A new ctxtra release:** on by default. `check-updates: false` at the top of the config turns it off.
  The check sends the user's IP address and ctxtra version to GitHub.
- **New commits of a pinned source:** on by default. When the user adds a pinned source, explain that
  sessions check it in the background. `--check-updates=false` on `ctxtra source add git` or
  `source add local-git`, or `check-updates: false` on the source in the config, turns checks off.
  Show the edit first (rule 1). The check connects to the remote with their git credentials, so an ssh
  key that needs a touch or an approval can ask for one when a session starts. For such a source,
  turn checks off. A local-git source checks only the clone's local HEAD.
- `ctxtra check-updates` checks now and shows the results. `ctxtra doctor` shows what the last checks
  found, and why one failed. A failed check never means "up to date".
- `CTXTRA_NO_UPDATE_CHECK=1` stops session checks when the hook inherits it. Muse clears that variable:
  set top-level `check-updates: false` and disable checks on each pinned source instead.

A notice is for the user. Don't upgrade ctxtra, and don't run `ctxtra source update`, because of one,
unless the user asks. Accepting new commits stays the user's decision (rule 4).
