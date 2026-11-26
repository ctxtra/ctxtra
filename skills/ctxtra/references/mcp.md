# MCP servers

MCP needs no experimental opt-in. Read `ctxtra help config` before editing its definitions.
Use `ctxtra mcp list --json` for the catalog and `ctxtra mcp status DIR` for a folder's selection and
missing tools, and for how long the last session's first tool list took. "waits up to" in its output is the
agent's deadline, not the time it took. Status reads only local files and has no JSON flag.

Config servers use bare identities; source servers use full references such as `work:linear`. Recovery hints and
login/logout use that full reference. Tools retain the short prefix `linear__`. A same-prefix conflict keeps the
first binding and never falls back to the other account. Check explain and status; do not guess from tool names.

Pinned definitions need the user's separate connection approval, even for no-auth endpoints. Ask the user to run
`ctxtra mcp approve REF` or `ctxtra mcp login REF` in their own terminal. Never answer approval consent, pipe yes,
simulate a terminal or perform provider authorization for them. Source acceptance and agent tool permissions do not
grant connection approval. Show can connect or execute a server; list, status and explain do not.
An entry-wide tool permission covers servers bound later. A short-prefix rule can match different source accounts
in different folders. Setup keeps native provider entries, so both native and ctxtra tools can appear.

Source definitions live in top-level `ctxtra-source.yaml`. The manifest has `schema: ctxtra/source/v1` and an `mcp-servers` map. The user's config selects
servers as `SOURCE:SERVER`. Source manifests never define bindings.

Declarations with the same identity and target share credentials. Add reuses a matching stored record without
authorization. It asks only for declared secret names that have no stored value, and adds them to the record. It
never replaces stored values or tokens. It refuses client-secret or callback-port changes to that record; use login
after adding the definition to change shared credentials. Use different names for separate accounts.

Muse clears the hook and MCP environment. Setup records the four XDG folder values in both entries.
After changing one, run `ctxtra setup --agent muse`; `doctor` reports stale values. Muse cannot inherit
shell proxy/certificate settings, Linux keychain variables, or local servers' `pass-env` values.
Do not copy secrets into its agent config. Use encrypted secret declarations. Start a new Muse session
after sign-in, as with Codex. CLI diagnostics use the current shell's folders.

Logout deletes local credentials, not provider grants or connection approval. Revoke withdraws approval for new
connections, not an immediate live-session kill. Recommend new sessions after account changes or local-secret
rotation. Removing a source leaves credentials and prints cleanup commands; another declaration may share them.
