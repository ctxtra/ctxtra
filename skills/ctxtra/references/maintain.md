# Maintain a source repository

The user's config binds content. A source cannot define bindings or conditions.

1. Run `ctxtra source validate DIR` before pushing. It reads only DIR, without config, trust, credentials
   or network. It checks YAML, names, keys, entry shapes, MCP definitions and the files that collections
   name, as a session reads them from a commit. Errors exit 1; secret-looking values are warnings.
2. Use `schema: ctxtra/source/v1` with `mcp-servers`, `collections` or `repo-sets`. A manifest is optional
   when users bind files by path. A collection lists `instructions`, `skills`, `mcp-servers` and nested
   `collections`; users bind it as `SOURCE:NAME`, so you can move files without breaking their configs.
   A repo set lists the repositories the team works in (`host/path` patterns, `{repo, subdirs}`, quoted
   `!` entries); users link a binding to it with `location: {repo-sets: ['SOURCE:SET']}`.
3. Manifest paths stay relative to the source's top. They cannot name another source or the user's disk.
   Never commit secrets, credential state or approval records. Source server changes to endpoints,
   commands or environment need users to review and approve again on each machine.
4. A passing validation does not make the text or scripts safe. Read them before binding them.
