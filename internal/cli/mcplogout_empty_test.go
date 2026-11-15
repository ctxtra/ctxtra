package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPLogoutWithoutCredentialsDoesNotBlockAnotherConfigAdd(t *testing.T) {
	for _, declared := range []bool{false, true} {
		t.Run(map[bool]string{false: "undeclared", true: "declared"}[declared], func(t *testing.T) {
			h := testenv.Home(t)
			t.Setenv("CTXTRA_EXPERIMENTAL_MCP", "1")
			keyring.MockInit()
			srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
			cfg := "schema: ctxtra/v1\n"
			if declared {
				cfg += "mcp-servers:\n  shared: {url: " + srv.URL + "}\n"
			}
			testenv.Config(t, h, cfg)
			store := openStore()
			if _, err := store.EnsureKey(); err != nil {
				t.Fatal(err)
			}
			generation, err := store.Generation(srv)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			stdio := IO{In: strings.NewReader(""), Out: &output, Err: &output}
			if n := Run([]string{"mcp", "logout", "shared"}, stdio, "test"); n != 0 {
				t.Fatalf("logout: %d, %s", n, &output)
			}
			if r, err := store.Load(srv); err != nil || r != nil {
				t.Fatalf("empty logout created a binding: %+v, %v", r, err)
			}
			if _, err := store.SaveSignIn(srv, generation, "issuer", creds.Client{}, creds.Token{Access: "late"}); !errors.Is(err, creds.ErrSignInChanged) {
				t.Fatalf("empty logout did not cancel first sign-in: %v", err)
			}
			second := filepath.Join(h, "second.yaml")
			testenv.Write(t, second, "schema: ctxtra/v1\n")
			t.Setenv("CTXTRA_CONFIG", second)
			output.Reset()
			if n := Run([]string{"mcp", "add", "shared", srv.URL, "--no-login"}, stdio, "test"); n != 0 || strings.Contains(output.String(), "reusing") {
				t.Fatalf("empty logout blocked or falsely reused credentials: %d, %s", n, &output)
			}
		})
	}
}
