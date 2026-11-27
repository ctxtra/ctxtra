# Import existing instructions

The rules and warnings in the ctxtra skill's `SKILL.md` apply to every step.

The user's agents can have instruction files of their own. Move their rules into files that ctxtra adds,
so that every agent gets them. Do every step; don't skip the files of an agent.

1. Find the files. `ctxtra import --start AGENT` starts an agent with a prompt that lists them and the
   folder with their backup. Without that prompt, run `ctxtra import`: it backs up the files of every
   agent that is set up and lists them. Don't look for other files: the list is complete.
2. Read every file in full, and the files they import (a line `@path` in Claude Code's files). Rules 6 and
   7 in `SKILL.md` apply.
3. Ask the user where to keep the new file. Suggest `~/.config/ctxtra/instructions.md`, or their own
   instructions or dotfiles folder if they have one. A folder in a git repository needs a trusted source (rule 5).
4. Draft one file with every rule of every file. Don't write it yet:
   - Rules that say the same thing become one rule. Keep the user's words; don't add rules of your own.
   - Drop no rule. If two rules disagree, ask the user which one to keep.
   - A rule for one agent only (it names that agent's tools, commands or settings) doesn't fit every
     agent. List these rules and ask the user: keep them in that agent's file, or move them.
   - A Claude Code rule file with `paths` in its frontmatter applies only when Claude works on matching
     files; ctxtra adds a file to the whole session. Keep such a file in Claude Code, unless the user
     wants its rules in every session.
5. Show the user the new file and, for each old file, which rules moved, which were merged, and which stay.
   Wait for their OK.
6. Write the file, and bind it to every session: `ctxtra binding add NAME --instruction FILE`
   (rule 1). Check with `ctxtra context files` that a session gets it.
7. Ask whether to remove the moved rules from the old files. Recommend it, and say why in short: it saves
   tokens, and more importantly the agents don't get two copies that drift apart once the user changes
   only the new file. Say where ctxtra's backup is, and suggest that they make a backup of their own too.
   Remove nothing unless they agree; if they don't, tell them that these agents get the rules twice.
8. If they agree, remove the moved rules from the old files. Show each change and wait for their OK
   (rule 1). Empty a file that has nothing left; don't delete it. Codex reads `AGENTS.md` when
   `AGENTS.override.md` has no text: emptying the override makes Codex read `AGENTS.md` again, so clean
   both. A file that is a symlink changes at its target: say so first. Other tools can read the same file
   (Gemini CLI reads `~/.gemini/GEMINI.md`): say so first, too. Don't change the files that the old files
   import: removing the import line is enough.
9. Tell the user where the backup is, and that new sessions get the new file.
