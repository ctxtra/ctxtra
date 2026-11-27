package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPAddConfigFailurePreservesSharedCredentials(t *testing.T) {
	h := testenv.Home(t)
	keyring.MockInit()
	file := testenv.Config(t, h, "schema: ctxtra/config/v1\n# second config\n")
	testenv.Write(t, filepath.Join(h, "first-config.yaml"), "schema: ctxtra/config/v1\nmcp-servers:\n  shared: {url: https://example.com/mcp, client-id: old-app}\n")
	srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
	store := openStore()
	if _, err := store.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(srv, true, func(r *creds.Record) error {
		r.Client = creds.Client{ID: "old-app", Secret: "old-secret", CallbackPort: 8765, Registered: true}
		r.Token = creds.Token{Access: "old-access", Refresh: "old-refresh"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(store.Dir, "credentials", "*.age"))
	if err != nil || len(files) != 1 {
		t.Fatalf("credential files: %v, %v", files, err)
	}
	beforeCredentials, _ := os.ReadFile(files[0])
	beforeConfig, _ := os.ReadFile(file)
	testenv.Write(t, paths.BackupDir(), "backup directory is unavailable")
	var output bytes.Buffer
	n := Run([]string{"mcp", "add", "shared", srv.URL, "--no-login"}, IO{In: strings.NewReader(""), Out: &output, Err: &output}, "test")
	if n != 1 {
		t.Fatalf("add unexpectedly succeeded: %s", &output)
	}
	afterConfig, _ := os.ReadFile(file)
	afterCredentials, _ := os.ReadFile(files[0])
	if !bytes.Equal(beforeConfig, afterConfig) || !bytes.Equal(beforeCredentials, afterCredentials) {
		t.Fatalf("failed add changed config or shared credentials: %s", &output)
	}
}
