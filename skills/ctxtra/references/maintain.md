# Maintain a source repository

When the user changes a source repository that others add as a pinned source:

1. Before they push, run `ctxtra source check DIR` on their clone. It reads the binding files the way
   users' sessions read them from the pinned source. It reports broken YAML and bindings, keys that work
   only in the config, paths outside the repository, missing files, skills without a `description`, and
   symlinks and submodules, which a pinned source leaves out. Fix the errors with the user.
2. Warnings don't fail the check: a glob that matches nothing, an exclusion that leaves nothing out, a
   `ctxtra://` reference to the repository itself. Ask the user whether each one is intended.
3. Pass `--name NAME` when users name the source differently from the folder. Prefer relative paths in
   the repository's binding files: they work under any name.
4. For CI, the example is in `ctxtra help config`, under "Check a source repository". Exit code 1 means an
   error.
5. Declare MCP servers once in root `mcp-servers.yaml`, with `schema: ctxtra/v1`
   and an `mcp-servers` map. Source bindings name only their own bare server names. Source check validates
   definitions and references without a user's config, sign-in, keychain or network. Never commit secrets or
   approval records. Endpoint, command and environment changes need users to review and approve again; wider
   targeting can activate an unchanged approved server in more folders.
6. A passing check doesn't make the repository safe. It doesn't review what instructions say or what skill
   scripts do. Say so if the user asks.
