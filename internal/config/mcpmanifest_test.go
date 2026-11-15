package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

const linearManifest = "schema: ctxtra/v1\nmcp-servers:\n  linear: {url: https://example.com/mcp}\n"

func TestMCPManifestScope(t *testing.T) {
	mcpOnForTest(t)
	h := testenv.Home(t)
	root := filepath.Join(h, "context")
	testenv.Write(t, filepath.Join(root, MCPManifest), linearManifest)
	testenv.Write(t, filepath.Join(root, "b.yaml"), head+"  - {name: package, match: {global: true}, mcp-servers: [linear, personal/linear, root_only]}\n")
	file := testenv.Config(t, h, "schema: ctxtra/v1\nsources:\n  work: {trusted-path: '"+root+"'}\nmcp-servers:\n  linear: {url: https://root.example/mcp}\n  root_only: {command: [root-mcp]}\ninclude: [ctxtra://work/b.yaml]\nbindings:\n  - {name: mine, match: {global: true}, mcp-servers: [linear, work/linear]}\n")
	cfg := load(file)
	if got := cfg.Bindings[0].MCPServers; !slices.Equal(got, []string{"linear", "work/linear"}) {
		t.Fatalf("config refs: %v", got)
	}
	if got := cfg.Bindings[1].MCPServers; !slices.Equal(got, []string{"work/linear"}) {
		t.Fatalf("source refs: %v", got)
	}
	s, ok := cfg.MCPServer("work/linear")
	if !ok || s.Source != "work" || s.ID != "work/linear" || s.Name != "linear" || s.Pinned || s.Origin != "trusted-path:"+root {
		t.Fatalf("source metadata: %+v", s)
	}
	if len(cfg.Problems) != 2 || !strings.Contains(cfg.Problems[0], "only their own servers") || !strings.Contains(cfg.Problems[1], "work/root_only") {
		t.Errorf("namespace diagnostics: %v", cfg.Problems)
	}
}

func TestMCPManifestLazyCatalog(t *testing.T) {
	mcpOnForTest(t)
	h := testenv.Home(t)
	commit := strings.Repeat("a", 40)
	root := source.GenDir("https://example.com/team.git", commit)
	testenv.Write(t, filepath.Join(root, MCPManifest), linearManifest)
	cfg := load(testenv.Config(t, h, "schema: ctxtra/v1\nsources:\n  work: {git: https://example.com/team.git, branch: main, commit: '"+commit+"'}\n"))
	if len(cfg.MCPServers) != 0 || len(cfg.touched) != 0 {
		t.Fatal("unused manifest was loaded")
	}
	cfg.LoadMCPCatalog()
	s, ok := cfg.MCPServer("work/linear")
	if !ok || !s.Pinned || s.Origin != "git:https://example.com/team.git" || !cfg.touched[root] {
		t.Fatalf("catalog did not touch pinned content: %+v, %v", s, cfg.touched)
	}
	cfg.LoadMCPCatalog()
	if len(cfg.MCPServers) != 1 {
		t.Fatal("catalog loaded a manifest twice")
	}
}

func TestMCPManifestExplicitAliasAndPhysicalDuplicate(t *testing.T) {
	mcpOnForTest(t)
	h := testenv.Home(t)
	root := filepath.Join(h, "ctx")
	testenv.Write(t, filepath.Join(root, MCPManifest), linearManifest)
	testenv.Write(t, filepath.Join(root, "b.yaml"), head+"  - {name: all, match: {global: true}, mcp-servers: [linear]}\n")
	cfg := load(testenv.Config(t, h, "schema: ctxtra/v1\nsources:\n  second: {trusted-path: '"+root+"'}\n  first: {trusted-path: '"+root+"'}\ninclude: [ctxtra://first/b.yaml, ctxtra://second/b.yaml]\n"))
	if len(cfg.Bindings) != 1 || !slices.Equal(cfg.Bindings[0].MCPServers, []string{"first/linear"}) {
		t.Fatalf("explicit alias lost: %+v", cfg.Bindings)
	}
	if len(cfg.Problems) != 1 || !strings.Contains(cfg.Problems[0], "listed twice") {
		t.Fatalf("physical duplicate: %v", cfg.Problems)
	}
}

func TestMCPManifestBrokenDoesNotDropBinding(t *testing.T) {
	mcpOnForTest(t)
	h := testenv.Home(t)
	root := filepath.Join(h, "ctx")
	testenv.Write(t, filepath.Join(root, MCPManifest), "schema: ctxtra/v1\nmcp-servers: [\n")
	testenv.Write(t, filepath.Join(root, "b.yaml"), head+"  - {name: all, match: {global: true}, mcp-servers: [linear], instructions: [x.md]}\n")
	cfg := load(testenv.Config(t, h, "schema: ctxtra/v1\nsources:\n  own: {trusted-path: '"+root+"'}\ninclude: [ctxtra://own/b.yaml]\n"))
	if len(cfg.MCPServers) != 0 || len(cfg.Bindings) != 1 || len(cfg.Bindings[0].Instructions) != 1 || len(cfg.Problems) != 2 {
		t.Fatalf("broken manifest affected unrelated content: %+v, %v", cfg.Bindings, cfg.Problems)
	}
}

func TestMCPManifestCheck(t *testing.T) {
	mcpOnForTest(t)
	root := t.TempDir()
	testenv.Write(t, filepath.Join(root, MCPManifest), "schema: ctxtra/v1\nmcp-servers:\n  linear: {url: https://example.com/one}\n  linear: {url: https://example.com/two}\n  valid: {command: [installed-mcp]}\n")
	testenv.Write(t, filepath.Join(root, "b.yaml"), head+"  - {name: all, match: {global: true}, mcp-servers: [linear, valid, unknown, other/valid]}\n")
	cfg := LoadSource(root, "pkg", []string{filepath.Join(root, "b.yaml")})
	if len(cfg.MCPServers) != 1 || !slices.Equal(cfg.Bindings[0].MCPServers, []string{"pkg/valid"}) || len(cfg.Problems) != 4 {
		t.Fatalf("manifest check: %+v, %v", cfg.Bindings, cfg.Problems)
	}
}
