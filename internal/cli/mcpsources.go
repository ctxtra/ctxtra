package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/source"
)

// mcpOrphans are notices about stored credentials of source servers that
// the config doesn't declare any more.
func mcpOrphans(env *resolve.Env) []string {
	if configError(env) != nil {
		return nil
	}
	ids, err := creds.Open().Identities()
	if err != nil {
		return []string{"couldn't list leftover MCP credentials: " + err.Error()}
	}
	servers, _ := env.Catalog.AllServers()
	var notices []string
	for _, id := range ids {
		if !strings.Contains(id, ":") || slices.ContainsFunc(servers, func(s catalog.Server) bool { return s.ID == id }) {
			continue
		}
		if src := env.Catalog.Source(strings.SplitN(id, ":", 2)[0]); src != nil && src.State != catalog.Active {
			continue // its manifest can't be read: it may still declare the server
		}
		notices = append(notices, fmt.Sprintf("MCP credentials for %s are not declared in this config. Explicit cleanup: ctxtra mcp remove %s", id, id))
	}
	return notices
}

// mcpDefinitionWarnings shows the MCP servers that the manifest of source
// name declares at a new commit, in after, against the accepted one, in
// before (nil for a new source): new, removed and changed definitions,
// and values that look like secrets.
func (s *sources) mcpDefinitionWarnings(name string, before, after *catalog.Catalog) {
	servers := func(cat *catalog.Catalog) map[string]upstream.Server {
		out := map[string]upstream.Server{}
		if cat != nil {
			found, _ := cat.SourceServers(name)
			for _, srv := range found {
				out[srv.ID] = srv.Server
			}
		}
		return out
	}
	old, now := servers(before), servers(after)
	ids := make([]string, 0, len(old)+len(now))
	for id := range old {
		ids = append(ids, id)
	}
	for id := range now {
		if _, found := old[id]; !found {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		prev, had := old[id]
		srv, has := now[id]
		switch {
		case !has:
			fmt.Fprintf(s.Out, "warning: removed MCP server %s; stored credentials remain for explicit cleanup\n", source.Sanitize(id))
		case !had:
			fmt.Fprintf(s.Out, "warning: new MCP server %s (%s); declaring it does not bind or connect it\n", source.Sanitize(id), approvalState(srv))
		case creds.Fingerprint(prev) != creds.Fingerprint(srv):
			fmt.Fprintf(s.Out, "warning: MCP server %s changed its operational definition; servers of repo sources need approval again, and a changed URL or argv needs new credentials\n", source.Sanitize(id))
		}
		if has {
			for _, n := range secretNotes(id, srv, "manifest") {
				fmt.Fprintf(s.Out, "warning: %s\n", source.Sanitize(n))
			}
			data, _ := json.MarshalIndent(srv, "", "  ")
			fmt.Fprintf(s.Out, "MCP definition %s:\n%s\n", source.Sanitize(id), source.Sanitize(string(data)))
		}
	}
}

// secretNotes are notes about the headers and variables of server id whose
// values look like secrets, which owner shouldn't hold.
func secretNotes(id string, s upstream.Server, owner string) []string {
	var out []string
	for _, h := range slices.Sorted(maps.Keys(s.Headers)) {
		if config.SecretHeader(h) {
			out = append(out, fmt.Sprintf("mcp-servers.%s: headers.%s looks like a secret, which the %s shouldn't hold; use secret-headers: [%s]", id, h, owner, h))
		}
	}
	for _, v := range slices.Sorted(maps.Keys(s.Env)) {
		if config.SecretVar(v) {
			out = append(out, fmt.Sprintf("mcp-servers.%s: env.%s looks like a secret, which the %s shouldn't hold; use secret-env: [%s]", id, v, owner, v))
		}
	}
	return out
}

// findServer is the server that ref names: a config server, the config's
// new name for a source's server, or SOURCE:SERVER.
func findServer(ref string) (upstream.Server, bool) {
	s, ok := loadEnv().Catalog.Server(ref)
	return s.Server, ok
}

// checkServerRef says what is wrong with a server reference: a config
// server's name, or SOURCE:SERVER.
func checkServerRef(ref string) error {
	source, name, qualified := strings.Cut(ref, ":")
	if !qualified {
		return config.CheckServerName(ref)
	}
	if err := config.CheckName(source); err != nil {
		return fmt.Errorf("server %q: %v", ref, err)
	}
	return config.CheckServerName(name)
}

// withLock runs fn under the lock of config writes.
func withLock(fn func() error) error { return configFiles().Locked(fn) }
