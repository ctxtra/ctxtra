package setup

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestLoadMCPFromUnboundSourceCatalog(t *testing.T) {
	h := testenv.Home(t)
	commit := strings.Repeat("a", 40)
	root := source.GenDir("https://example.invalid/context.git", commit)
	testenv.Write(t, filepath.Join(root, manifest.FileName), "schema: ctxtra/source/v1\nmcp-servers:\n  build: {command: [nonexistent-mcp], pass-env: [BUILD_HOST]}\n")
	file := testenv.Config(t, h, "schema: ctxtra/config/v1\nsources:\n  work: {repo: 'https://example.invalid/context.git', branch: main}\n")
	testenv.Write(t, lock.Path(file), "schema: ctxtra/lock/v1\nsources:\n  work:\n    repo: https://example.invalid/context.git\n    branch: main\n    commit: "+commit+"\n")
	got := LoadMCP()
	if !slices.Equal(got.EnvPass, []string{"BUILD_HOST"}) {
		t.Fatalf("source-only setup: %+v", got)
	}
	testenv.Write(t, file, "schema: [\n")
	if got := LoadMCP(); len(got.EnvPass) != 0 {
		t.Fatalf("invalid config still enables MCP: %+v", got)
	}
}
