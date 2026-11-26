package resolve

import (
	"testing"
)

// Of two servers with one name, the session gets the first in binding
// order; the other is a conflict, a notice and not an error, and is never
// served instead.
func TestMCPPrefixConflictKeepsFirstIdentity(t *testing.T) {
	h := newHome(t)
	h.write("context/ctxtra-source.yaml", "schema: ctxtra/source/v1\nmcp-servers:\n  linear: {url: https://package.example/mcp}\n  other: {command: [other-mcp]}\n")
	env := h.load(`sources:
  work: {dir: ~/context}
mcp-servers:
  linear: {url: https://config.example/mcp}
bindings:
  - {name: root, mcp-servers: [linear]}
  - {name: repeat, mcp-servers: [linear]}
  - {name: package, mcp-servers: ['work:linear', 'work:other']}
  - {name: repeat_package, mcp-servers: ['work:linear']}
`)
	res := start(env, h.dir, "")
	noErrors(t, res)
	if len(res.MCPServers) != 2 || res.MCPServers[0].ID != "linear" || res.MCPServers[1].ID != "work:other" {
		t.Fatalf("selected servers: %+v", res.MCPServers)
	}
	if len(res.MCPConflicts) != 1 || res.MCPConflicts[0].Winner != "linear" || res.MCPConflicts[0].Loser != "work:linear" {
		t.Fatalf("conflict: %+v", res.MCPConflicts)
	}
	if len(res.Notices) != 1 || res.Notices[0] != "MCP servers linear and work:linear are both named linear; the session gets linear, the first in binding order" {
		t.Fatalf("notices: %q", res.Notices)
	}
}
