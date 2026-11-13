package resolve

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPPrefixConflictKeepsFirstIdentity(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
	root := filepath.Join(h, "context")
	testenv.Write(t, filepath.Join(root, "mcp-servers.yaml"), "schema: ctxtra/v1\nmcp-servers:\n  linear: {url: https://package.example/mcp}\n  other: {command: [other-mcp]}\n")
	testenv.Write(t, filepath.Join(root, "b.yaml"), "schema: ctxtra/v1\nbindings:\n  - {name: package, match: {global: true}, mcp-servers: [linear, other]}\n  - {name: repeat_package, match: {global: true}, mcp-servers: [linear]}\n")
	file := testenv.Config(t, h, "schema: ctxtra/v1\nsources:\n  work: {trusted-path: '"+root+"'}\nmcp-servers:\n  linear: {url: https://config.example/mcp}\nbindings:\n  - {name: root, match: {global: true}, mcp-servers: [linear]}\n  - {name: repeat, match: {global: true}, mcp-servers: [linear]}\ninclude: [ctxtra://work/b.yaml]\n")
	res, _ := Session(h, file)
	if len(res.MCPServers) != 2 || res.MCPServers[0].ID != "linear" || res.MCPServers[1].ID != "work/other" {
		t.Fatalf("selected servers: %+v", res.MCPServers)
	}
	if len(res.MCPConflicts) != 1 || res.MCPConflicts[0].Winner != "linear" || res.MCPConflicts[0].Loser != "work/linear" {
		t.Fatalf("conflict: %+v", res.MCPConflicts)
	}
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "without fallback") {
		t.Fatalf("visible conflict: %v", res.Problems)
	}
}
