package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPLogoutUndeclaredSourceKeepsOtherConfigRegistration(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
	keyring.MockInit()
	testenv.Config(t, h, "schema: ctxtra/v1\n")
	root := filepath.Join(h, "context")
	testenv.Write(t, filepath.Join(root, config.MCPManifest), "schema: ctxtra/v1\nmcp-servers:\n  shared: {url: https://example.com/mcp}\n")
	other := filepath.Join(h, "other-config.yaml")
	testenv.Write(t, other, "schema: ctxtra/v1\nsources:\n  work: {trusted-path: '"+root+"'}\n")
	srv, ok := config.Load(other, newReader()).MCPServer("work/shared")
	if !ok {
		t.Fatal("other config did not declare source server")
	}
	store := openStore()
	if _, err := store.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(srv, true, func(r *creds.Record) error {
		r.Client = creds.Client{ID: "app", Secret: "client-secret", Registered: true, Redirect: "http://127.0.0.1:4567/callback", CallbackPort: 4567}
		r.Token.Access = "access"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if n := Run([]string{"mcp", "logout", "work/shared"}, IO{Out: &output, Err: &output}, "test"); n != 0 {
		t.Fatalf("logout failed: %s", &output)
	}
	r, err := store.Load(srv)
	if err != nil || r == nil || !r.Matches(srv) || r.Client.ID != "app" || !r.Client.Registered || r.Client.Redirect != "http://127.0.0.1:4567/callback" || r.Client.CallbackPort != 4567 || r.Client.Secret != "" || r.Token.Access != "" {
		t.Fatalf("orphan logout destroyed another config's registration: %+v, %v", r, err)
	}
}
