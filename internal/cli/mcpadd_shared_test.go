package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPAddReusesSharedCredentialsWithoutPromptsOrAuthorization(t *testing.T) {
	for _, secretHeader := range []bool{false, true} {
		t.Run(map[bool]string{false: "oauth", true: "secret header"}[secretHeader], func(t *testing.T) {
			h := testenv.Home(t)
			keyring.MockInit()
			// Tool discovery may use the existing token, but unauthenticated
			// OAuth detection must not start another authorization.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want := "Bearer config-A-token"
				if secretHeader {
					want = "config-A-secret"
				}
				if got := r.Header.Get("Authorization"); got != want {
					t.Errorf("shared credentials not reused: Authorization = %q", got)
				}
				w.WriteHeader(http.StatusBadRequest)
			}))
			t.Cleanup(server.Close)
			testenv.Write(t, filepath.Join(h, "first-config.yaml"), "schema: ctxtra/config/v1\nmcp-servers:\n  shared: {url: '"+server.URL+"'}\n")
			file := testenv.Config(t, h, "schema: ctxtra/config/v1\n# second config\n")
			srv := upstream.Server{ID: "shared", Name: "shared", URL: server.URL}
			store := openStore()
			if _, err := store.EnsureKey(); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Update(srv, true, func(r *creds.Record) error {
				r.Client = creds.Client{ID: "config-A-app", Secret: "config-A-client-secret", CallbackPort: 8765, Registered: true}
				if secretHeader {
					r.Secrets.Headers = map[string]string{"Authorization": "config-A-secret"}
				} else {
					r.Token = creds.Token{Access: "config-A-token", Refresh: "config-A-refresh"}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			files, _ := filepath.Glob(filepath.Join(store.Dir, "credentials", "*.age"))
			before, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			input := strings.NewReader("config-B-secret\n")
			o := mcpAddOptions{interactive: true}
			if secretHeader {
				o.secretHeaders = []string{"Authorization"}
			}
			if n := mcpAdd(IO{In: input, Out: &output, Err: &output}, "shared", server.URL, nil, o); n != 0 {
				t.Fatalf("add: %d, %s", n, &output)
			}
			after, err := os.ReadFile(files[0])
			if err != nil || !bytes.Equal(before, after) || input.Len() != len("config-B-secret\n") {
				t.Fatalf("add changed shared credentials or consumed secret input: %v, %s", err, &output)
			}
			cfg, _ := os.ReadFile(file)
			if !strings.Contains(string(cfg), "shared:") || !strings.Contains(output.String(), "reusing the existing shared credentials") {
				t.Fatalf("missing declaration or shared-effects notice: %s\n%s", cfg, &output)
			}
		})
	}
}

func TestMCPAddRefusesExplicitSharedSettingsChanges(t *testing.T) {
	for _, flags := range [][]string{{"--client-id", "config-B-app", "--client-secret"}, {"--callback-port", "9999"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			h := testenv.Home(t)
			keyring.MockInit()
			file := testenv.Config(t, h, "schema: ctxtra/config/v1\n")
			srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
			store := openStore()
			if _, err := store.EnsureKey(); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Update(srv, true, func(r *creds.Record) error {
				r.Client = creds.Client{ID: "config-A-app", Secret: "config-A-secret", CallbackPort: 8765}
				r.Token = creds.Token{Access: "config-A-token", Refresh: "config-A-refresh"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			files, _ := filepath.Glob(filepath.Join(store.Dir, "credentials", "*.age"))
			beforeCredentials, _ := os.ReadFile(files[0])
			beforeConfig, _ := os.ReadFile(file)
			input := strings.NewReader("config-B-secret\n")
			var output bytes.Buffer
			args := append([]string{"mcp", "add", "shared", srv.URL, "--no-login"}, flags...)
			if n := Run(args, IO{In: input, Out: &output, Err: &output}, "test"); n != 1 || !strings.Contains(output.String(), "shared by agent sessions") {
				t.Fatalf("shared settings change not refused: %d, %s", n, &output)
			}
			afterCredentials, _ := os.ReadFile(files[0])
			afterConfig, _ := os.ReadFile(file)
			if !bytes.Equal(beforeCredentials, afterCredentials) || !bytes.Equal(beforeConfig, afterConfig) || input.Len() != len("config-B-secret\n") {
				t.Fatal("refused add changed config, credentials or consumed a secret")
			}
		})
	}
}
