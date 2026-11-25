package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/source"
)

// mcpOrphans reports only absence from this config, not global non-use.
func mcpOrphans(cfg *config.Config) []string {
	if cfg.Ignored() {
		return nil
	}
	ids, err := creds.Open().Identities()
	if err != nil {
		return []string{"couldn't list leftover MCP credentials: " + err.Error()}
	}
	var notices []string
	for _, id := range ids {
		if strings.Contains(id, "/") && config.CheckMCPRef(id) == nil && !cfg.MCPDeclared(id) {
			notices = append(notices, fmt.Sprintf("MCP credentials for %s are not declared in this config. Explicit cleanup: ctxtra mcp remove %s", id, id))
		}
	}
	return notices
}

func (s *sources) mcpDefinitionWarnings(name string, before, after *config.Config) {
	catalog := func(cfg *config.Config) map[string]upstream.Server {
		out := map[string]upstream.Server{}
		if cfg != nil {
			for _, srv := range cfg.MCPServers {
				if srv.Source == name {
					out[srv.ID] = srv
				}
			}
		}
		return out
	}
	old, now := catalog(before), catalog(after)
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
	for _, note := range after.Notes {
		if strings.HasPrefix(note, "mcp-servers."+name+"/") {
			fmt.Fprintf(s.Out, "warning: %s\n", source.Sanitize(note))
		}
	}
	for _, id := range ids {
		prev, had := old[id]
		srv, has := now[id]
		switch {
		case !has:
			fmt.Fprintf(s.Out, "warning: removed MCP server %s; stored credentials remain for explicit cleanup\n", source.Sanitize(id))
		case !had:
			fmt.Fprintf(s.Out, "warning: new MCP server %s (%s); declaring it does not bind or connect it\n", source.Sanitize(id), approvalState(srv))
		case creds.Fingerprint(prev) != creds.Fingerprint(srv):
			fmt.Fprintf(s.Out, "warning: MCP server %s changed its operational definition; pinned servers need approval again, and a changed URL or argv needs new credentials\n", source.Sanitize(id))
		}
		if has {
			data, _ := json.MarshalIndent(srv, "", "  ")
			fmt.Fprintf(s.Out, "MCP definition %s:\n%s\n", source.Sanitize(id), source.Sanitize(string(data)))
		}
	}
}
