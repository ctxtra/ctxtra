package cli

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPApprovalFailureExitCodes(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
	commit := strings.Repeat("a", 40)
	root := source.GenDir("https://example.invalid/context.git", commit)
	testenv.Write(t, filepath.Join(root, config.MCPManifest), "schema: ctxtra/v1\nmcp-servers:\n  build: {command: [build-mcp]}\n")
	testenv.Config(t, h, "schema: ctxtra/v1\nsources:\n  work: {git: https://example.invalid/context.git, branch: main, commit: '"+commit+"'}\n")
	old := Interactive
	t.Cleanup(func() { Interactive = old })
	for _, tc := range []struct {
		args     []string
		terminal bool
		code     int
	}{
		{[]string{"mcp", "approve", "work/build"}, false, 1},
		{[]string{"mcp", "approve", "work/build", "--no-input"}, true, 1},
		{[]string{"mcp", "approve", "work/build"}, true, 1},
		{[]string{"mcp", "approve", "unknown"}, true, 1},
		{[]string{"mcp", "revoke", "unknown"}, true, 1},
		{[]string{"mcp", "remove", "work/build"}, true, 1},
		{[]string{"mcp", "approve"}, true, 2},
		{[]string{"mcp", "revoke", "--unknown"}, true, 2},
	} {
		Interactive = func(IO) bool { return tc.terminal }
		var out, stderr bytes.Buffer
		n := Run(tc.args, IO{In: strings.NewReader("n\n"), Out: &out, Err: &stderr}, "test")
		if n != tc.code || (tc.code == 1 && strings.Contains(stderr.String(), "Usage:")) {
			t.Errorf("%v: exit %d, stderr %s", tc.args, n, &stderr)
		}
	}
}

func TestMCPAddRecoversLostCredentialKey(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
	keyring.MockInitWithError(errors.New("test: no keychain"))
	store := openStore()
	if _, err := store.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	old := upstream.Server{ID: "build", Name: "build", Command: []string{"old-mcp"}}
	if err := store.SetSecrets(old, map[string]string{"TOKEN": "old"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.Dir, "key")); err != nil {
		t.Fatal(err)
	}
	testenv.Config(t, h, "schema: ctxtra/v1\n")
	var output bytes.Buffer
	io := IO{In: strings.NewReader("new\n"), Out: &output, Err: &output}
	if n := mcpAdd(io, "build", "", []string{"missing-build-mcp"}, mcpAddOptions{noLogin: true, secretEnv: []string{"TOKEN"}}); n != 0 {
		t.Fatalf("add with lost key: exit %d, %s", n, &output)
	}
	server := upstream.Server{ID: "build", Name: "build", Command: []string{"missing-build-mcp"}, SecretEnv: []string{"TOKEN"}}
	env, _, err := openStore().Secrets(server)
	if err != nil || env["TOKEN"] != "new" {
		t.Fatalf("new credentials: %v, %v", env, err)
	}
}

func TestSourceRemovalReportsOnlyMCPReferences(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
	testenv.Config(t, h, "schema: ctxtra/v1\nbindings:\n  - name: mine\n    match: {dirs: ['~/work/**']}\n    mcp-servers: [work/build]\n")
	var output bytes.Buffer
	s := newSources(IO{Out: &output})
	s.stillUsed(config.Source{Name: "work"}, "ctxtra://work/")
	if !strings.Contains(output.String(), "MCP server work/build in binding mine") || strings.Contains(output.String(), "~/work/**") {
		t.Fatalf("remaining references: %s", &output)
	}
}

func TestSourceBindingListingExcludesManifest(t *testing.T) {
	h := testenv.Home(t)
	root := filepath.Join(h, "context")
	testenv.Write(t, filepath.Join(root, config.MCPManifest), "schema: ctxtra/v1\nmcp-servers: {}\n")
	testenv.Write(t, filepath.Join(root, "b.yaml"), "schema: ctxtra/v1\nbindings: []\n")
	var output bytes.Buffer
	newSources(IO{Out: &output}).listSourceBindingFiles("work", root)
	if strings.Contains(output.String(), config.MCPManifest) || !strings.Contains(output.String(), "  b.yaml\n") {
		t.Fatalf("binding listing: %s", &output)
	}
}

func TestMCPStatusDoesNotResolveConfigReferenceBySuffix(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
	root := filepath.Join(h, "context")
	testenv.Write(t, filepath.Join(root, config.MCPManifest), "schema: ctxtra/v1\nmcp-servers:\n  linear: {command: [linear-mcp]}\n")
	testenv.Config(t, h, "schema: ctxtra/v1\nsources:\n  work: {trusted-path: '"+root+"'}\nbindings:\n  - {name: mine, match: {global: true}, mcp-servers: [linear, work/linear]}\n")
	var output bytes.Buffer
	a := &app{IO: IO{Out: &output}}
	a.mcpStatus(h)
	if !strings.Contains(output.String(), "unavailable MCP reference \"linear\"") || strings.Contains(output.String(), "unavailable MCP reference \"work/linear\"") {
		t.Fatalf("reference status: %s", &output)
	}
}

func TestNoAuthLoginDoesNotClaimToReplaceCredentials(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
	keyring.MockInit()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer remote.Close()
	testenv.Config(t, h, "schema: ctxtra/v1\nmcp-servers:\n  linear: {url: '"+remote.URL+"'}\n")
	store := openStore()
	if _, err := store.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	old := upstream.Server{ID: "linear", Name: "linear", URL: "https://old.example/mcp"}
	if _, err := store.Update(old, true, func(r *creds.Record) error { r.Token.Access = "old-token"; return nil }); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(store.Dir, "credentials", "*.age"))
	before, _ := os.ReadFile(files[0])
	interactive := Interactive
	t.Cleanup(func() { Interactive = interactive })
	Interactive = func(IO) bool { return true }
	var output bytes.Buffer
	a := &app{IO: IO{In: strings.NewReader("yes\n"), Out: &output, Err: &output}}
	if n := a.mcpLogin("linear", false, signInOptions{}); n != 1 || !strings.Contains(output.String(), "stored credentials were not replaced") {
		t.Fatalf("no-auth replacement: exit %d, %s", n, &output)
	}
	after, _ := os.ReadFile(files[0])
	if !bytes.Equal(before, after) || strings.Contains(output.String(), "stored its secret values") {
		t.Fatalf("no-auth replacement changed or misreported credentials: %s", &output)
	}
}

func TestSourceSecretWarningsReachMaintainersAndReview(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
	root := filepath.Join(h, "context")
	testenv.Write(t, filepath.Join(root, config.MCPManifest), "schema: ctxtra/v1\nmcp-servers:\n  linear: {url: https://example.invalid/mcp, headers: {Authorization: embedded-value}}\n")
	var output bytes.Buffer
	if n := checkSource(IO{Out: &output, Err: &output}, root, "work", nil, false); n != 0 || !strings.Contains(output.String(), "1 warning") || !strings.Contains(output.String(), "manifest shouldn't hold") {
		t.Fatalf("maintainer warnings: exit %d, %s", n, &output)
	}
	cfg := config.LoadSource(root, "work", nil)
	output.Reset()
	newSources(IO{Out: &output}).mcpDefinitionWarnings("work", nil, cfg)
	if !strings.Contains(output.String(), "warning: mcp-servers.work/linear: headers.Authorization") {
		t.Fatalf("review warnings: %s", &output)
	}
}
