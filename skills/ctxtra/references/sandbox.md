# When the sandbox blocks a change

ctxtra keeps config in `~/.config/ctxtra`, data and installed skills in `~/.local/share/ctxtra`,
state and backups in `~/.local/state/ctxtra`, and clones in `~/.cache/ctxtra`, plus agent configs.
`doctor`, `context`, `list` and `help` only read. Most dry runs write nothing. `source update --dry-run`
needs the network and may fetch into the cache; it writes nothing outside the cache, and accepts or dismisses
no commits.
Absolute XDG variables can move these folders.

When a change is blocked:

1. If your agent can ask the user for approval to run a command outside the sandbox, ask for it with the
   same command you showed under rule 1 of `SKILL.md`.
2. If it can't, give the user the ctxtra command to run themselves (in Claude Code: `! <command>`). For a
   binding, check it first with `ctxtra binding add ... --dry-run`, then give the same command without
   `--dry-run`. Don't give a script that writes the YAML (`cat >>`, `sed`, a heredoc): it skips the
   checks and the backup. For a change no command does, show the edit as a diff for the user to make.
3. Don't suggest that the user lets the sandbox write ctxtra's config, data or state folders without asking.
   An agent that can change the config without asking can change what every later session gets.
