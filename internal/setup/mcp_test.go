package setup

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestLoadMCPFromUnboundSourceCatalog(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
	commit := strings.Repeat("a", 40)
	root := source.GenDir("https://example.invalid/context.git", commit)
	testenv.Write(t, filepath.Join(root, config.MCPManifest), "schema: ctxtra/v1\nmcp-servers:\n  build: {command: [nonexistent-mcp], env-pass: [BUILD_HOST]}\n")
	file := testenv.Config(t, h, "schema: ctxtra/v1\nsources:\n  work: {git: https://example.invalid/context.git, branch: main, commit: '"+commit+"'}\n")
	got := LoadMCP()
	if !got.Want || !slices.Equal(got.EnvPass, []string{"BUILD_HOST"}) {
		t.Fatalf("source-only setup: %+v", got)
	}
	for _, off := range []string{"gate", "invalid config"} {
		if off == "gate" {
			t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "")
		} else {
			t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
			testenv.Write(t, file, "schema: [\n")
		}
		if got := LoadMCP(); got.Want || len(got.EnvPass) != 0 {
			t.Fatalf("%s still enables MCP: %+v", off, got)
		}
	}
}
