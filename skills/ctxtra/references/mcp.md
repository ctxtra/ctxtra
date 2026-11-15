# Experimental MCP servers

MCP requires `CTXTRA_EXPERIMENTAL_MCP=1` for commands and agents. Read `ctxtra help config` before editing its
definitions. Use `ctxtra mcp list --json` for the catalog and `ctxtra mcp status DIR` for a folder's selection and
missing tools. Status reads only local files and has no JSON flag.

Config servers use bare identities; source servers use full references such as `work/linear`. Recovery hints and
login/logout use that full reference. Tools retain the short prefix `linear__`. A same-prefix conflict keeps the
first binding and never falls back to the other account. Check explain and status; do not guess from tool names.

Pinned definitions need the user's separate connection approval, even for no-auth endpoints. Ask the user to run
`ctxtra mcp approve REF` or `ctxtra mcp login REF` in their own terminal. Never answer approval consent, pipe yes,
simulate a terminal or perform provider authorization for them. Source acceptance and agent tool permissions do not
grant connection approval. Show can connect or execute a server; list, status and explain do not.
An entry-wide tool permission covers servers bound later. A short-prefix rule can match different source accounts
in different folders. Setup keeps native provider entries, so both native and ctxtra tools can appear.

Source definitions live in root `mcp-servers.yaml`. Every source binding, including trusted-path, can name only its
own bare server names. Your config can bind a full source reference without including its package bindings.
There are no package settings or rename overrides. Do not edit pinned store files or copy tokens between identities.

Configs with the same identity and target share credentials. Add reuses a matching stored record without
authorization. It asks only for declared secret names that have no stored value, and adds them to the record. It
never replaces stored values or tokens. It refuses client-secret or callback-port changes to that record; use login
after adding the definition to change shared credentials. Use different names for separate accounts.

Logout deletes local credentials, not provider grants or connection approval. Revoke withdraws approval for new
connections, not an immediate live-session kill. Recommend new sessions after account changes or local-secret
rotation. Removing a source leaves credentials and prints cleanup commands; another config may share them.
