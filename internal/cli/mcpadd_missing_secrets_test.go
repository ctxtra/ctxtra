package cli

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// sharedSecretsCase is a server whose shared record has a token and the
// value of one secret name, and a new definition that adds another name.
type sharedSecretsCase struct {
	srv     upstream.Server
	opts    mcpAddOptions
	stored  creds.Secrets
	missing string
}

func newSharedSecretsCase(t *testing.T, h string, local bool) sharedSecretsCase {
	t.Helper()
	c := sharedSecretsCase{
		srv:     upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"},
		opts:    mcpAddOptions{noLogin: true, secretHeaders: []string{"Authorization", "X-Api-Key"}},
		stored:  creds.Secrets{Headers: map[string]string{"Authorization": "existing-secret"}},
		missing: "X-Api-Key",
	}
	if local {
		c.srv.URL, c.srv.Command = "", []string{filepath.Join(h, "absent-mcp")}
		c.opts = mcpAddOptions{secretEnv: []string{"OLD_TOKEN", "NEW_TOKEN"}}
		c.stored = creds.Secrets{Env: map[string]string{"OLD_TOKEN": "existing-secret"}}
		c.missing = "NEW_TOKEN"
	}
	store := openStore()
	if _, err := store.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(c.srv, true, func(r *creds.Record) error {
		r.Token = creds.Token{Access: "existing-token", Refresh: "existing-refresh"}
		r.Secrets = creds.Secrets{Env: maps.Clone(c.stored.Env), Headers: maps.Clone(c.stored.Headers)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMCPAddFillsMissingSharedSecrets(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "header", true: "environment"}[local], func(t *testing.T) {
			h := testenv.Home(t)
			keyring.MockInit()
			testenv.Config(t, h, "schema: ctxtra/v1\n")
			c := newSharedSecretsCase(t, h, local)
			store := openStore()
			_, before, err := store.Inspect(c.srv)
			if err != nil {
				t.Fatal(err)
			}
			// One line: asking for a stored name too would find no value.
			var output bytes.Buffer
			if n := mcpAdd(IO{In: strings.NewReader("new-secret\n"), Out: &output, Err: &output}, "shared", c.srv.URL, c.srv.Command, c.opts); n != 0 {
				t.Fatalf("add: %d, %s", n, &output)
			}
			if !strings.Contains(output.String(), "stored the values of its new secret names") {
				t.Fatalf("no notice about the new secret values: %s", &output)
			}
			r, after, err := store.Inspect(c.srv)
			if err != nil || r == nil {
				t.Fatalf("inspect: %v", err)
			}
			if after <= before {
				t.Fatalf("generation did not advance: %d, then %d", before, after)
			}
			if r.Token.Access != "existing-token" || r.Token.Refresh != "existing-refresh" {
				t.Fatalf("token changed: %+v", r.Token)
			}
			defined := c.srv
			if local {
				defined.SecretEnv = c.opts.secretEnv
			} else {
				defined.SecretHeaders = c.opts.secretHeaders
			}
			env, headers, err := store.Secrets(defined)
			if err != nil {
				t.Fatal(err)
			}
			got := headers
			want := map[string]string{"Authorization": "existing-secret", "X-Api-Key": "new-secret"}
			if local {
				got, want = env, map[string]string{"OLD_TOKEN": "existing-secret", "NEW_TOKEN": "new-secret"}
			}
			if !maps.Equal(got, want) {
				t.Fatalf("secrets = %v, want %v", got, want)
			}
		})
	}
}

func TestMCPAddWithoutMissingSharedSecretChangesNothing(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "header", true: "environment"}[local], func(t *testing.T) {
			h := testenv.Home(t)
			keyring.MockInit()
			file := testenv.Config(t, h, "schema: ctxtra/v1\n")
			c := newSharedSecretsCase(t, h, local)
			c.opts.noInput = true
			store := openStore()
			files, _ := filepath.Glob(filepath.Join(store.Dir, "credentials", "*.age"))
			beforeCredentials, _ := os.ReadFile(files[0])
			beforeConfig, _ := os.ReadFile(file)
			var output bytes.Buffer
			if n := mcpAdd(IO{In: strings.NewReader(""), Out: &output, Err: &output}, "shared", c.srv.URL, c.srv.Command, c.opts); n != 1 || !strings.Contains(output.String(), c.missing) {
				t.Fatalf("add without the missing value did not fail: %d, %s", n, &output)
			}
			afterCredentials, _ := os.ReadFile(files[0])
			afterConfig, _ := os.ReadFile(file)
			if !bytes.Equal(beforeCredentials, afterCredentials) || !bytes.Equal(beforeConfig, afterConfig) {
				t.Fatal("failed add changed shared credentials or config")
			}
		})
	}
}
