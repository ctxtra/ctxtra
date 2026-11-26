package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPAddRejectsStoredBindingBeforeMutation(t *testing.T) {
	for _, kind := range []string{"remote", "local"} {
		t.Run(kind, func(t *testing.T) {
			h := testenv.Home(t)
			keyring.MockInit()
			store := openStore()
			if _, err := store.EnsureKey(); err != nil {
				t.Fatal(err)
			}
			old := upstream.Server{ID: "linear", Name: "linear", URL: "https://old.example/mcp"}
			var command []string
			url := "https://new.example/mcp"
			if kind == "local" {
				old.URL, old.Command = "", []string{"old-mcp", "--old"}
				url, command = "", []string{"new-mcp"}
			}
			if _, err := store.Update(old, true, func(r *creds.Record) error {
				r.Token.Access, r.Client.Secret, r.Client.CallbackPort = "old-token", "old-secret", 9999
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			cfg := testenv.Config(t, h, "schema: ctxtra/config/v1\n")
			configBefore, _ := os.ReadFile(cfg)
			files, _ := filepath.Glob(filepath.Join(store.Dir, "credentials", "*.age"))
			recordBefore, _ := os.ReadFile(files[0])
			var out, stderr bytes.Buffer
			stdin := strings.NewReader("new-secret\n")
			options := mcpAddOptions{noLogin: true, clientID: "new-app", clientSecret: true, signIn: signInOptions{callbackPort: 8888}}
			if kind == "local" {
				options.clientID, options.clientSecret = "", false
				options.secretEnv = []string{"TOKEN"}
			}
			if code := mcpAdd(IO{In: stdin, Out: &out, Err: &stderr}, "linear", url, command, options); code != 1 || !strings.Contains(stderr.String(), "stored credentials for another") {
				t.Fatalf("add: %d, %s, %s", code, &out, &stderr)
			}
			configAfter, _ := os.ReadFile(cfg)
			recordAfter, _ := os.ReadFile(files[0])
			if !bytes.Equal(configBefore, configAfter) || !bytes.Equal(recordBefore, recordAfter) || stdin.Len() != len("new-secret\n") {
				t.Fatal("conflicting add mutated config, credentials or prompted for a secret")
			}
		})
	}
}

func TestMCPStillDeclaredChecksCommandAndOrigin(t *testing.T) {
	h := testenv.Home(t)
	cfg := testenv.Config(t, h, "schema: ctxtra/config/v1\nmcp-servers:\n  build: {command: [tools, --read]}\n")
	srv := upstream.Server{ID: "build", Name: "build", Command: []string{"tools", "--read"}}
	saved := false
	save := func() error { saved = true; return nil }
	if err := stillDeclared(srv)(save); err != nil || !saved {
		t.Fatalf("unchanged command: %v", err)
	}
	testenv.Write(t, cfg, "schema: ctxtra/config/v1\nmcp-servers:\n  build: {command: [tools, --write]}\n")
	saved = false
	if err := stillDeclared(srv)(save); err == nil || saved {
		t.Fatal("sign-in saved for an outdated command")
	}
	srv.Command = []string{"tools", "--write"}
	srv.Origin = "another-source"
	if err := stillDeclared(srv)(save); err == nil || saved {
		t.Fatal("sign-in saved for another origin")
	}
}

func TestMCPCleanupPreservesCredentialsWhenDeclarationIsUnreadable(t *testing.T) {
	h := testenv.Home(t)
	keyring.MockInit()
	store := openStore()
	if _, err := store.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	server := upstream.Server{ID: "work:build", Name: "build", Command: []string{"build-mcp"}}
	if _, err := store.Update(server, true, func(r *creds.Record) error {
		r.Secrets.Env = map[string]string{"TOKEN": "keep"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(h, "context")
	manifest := filepath.Join(root, manifest.FileName)
	testenv.Write(t, manifest, "schema: ctxtra/source/v1\nmcp-servers: [\n")
	file := testenv.Config(t, h, "schema: ctxtra/config/v1\nsources:\n  work: {dir: '"+root+"'}\n")
	var output bytes.Buffer
	a := &app{IO: IO{Out: &output}}
	for _, broken := range []string{"manifest", "config"} {
		if broken == "config" {
			testenv.Write(t, file, "schema: [\n")
		}
		if err := a.removeMCPLeftover(server.ID, false); err == nil || !strings.Contains(err.Error(), "cannot establish") {
			t.Fatalf("%s: %v", broken, err)
		}
		r, err := store.Load(server)
		if err != nil || r == nil || r.Secrets.Env["TOKEN"] != "keep" {
			t.Fatalf("%s cleanup changed credentials: %+v, %v", broken, r, err)
		}
	}
}

func TestMCPStatusReportsUnavailableReferencesLocally(t *testing.T) {
	h := testenv.Home(t)
	testenv.Config(t, h, "schema: ctxtra/config/v1\nmcp-servers:\n  build: {command: ['./relative-command']}\nbindings:\n  - {name: work, mcp-servers: [build, missing]}\n")
	var output bytes.Buffer
	a := &app{IO: IO{Out: &output}}
	a.mcpStatus(h, "")
	for _, ref := range []string{"build", "missing"} {
		if !strings.Contains(output.String(), "unavailable MCP reference \""+ref+"\"") {
			t.Fatalf("unavailable reference %s not reported: %s", ref, &output)
		}
	}
}

func TestMCPReplacementConsent(t *testing.T) {
	old := &creds.Record{ID: "linear", URL: "https://old.example/mcp"}
	srv := upstream.Server{ID: "linear", Name: "linear", URL: "https://new.example/mcp"}
	for _, tc := range []struct {
		name, answer      string
		terminal, allowed bool
	}{
		{"decline", "n\n", true, false},
		{"default-no", "\n", true, false},
		{"closed", "", true, false},
		{"no-terminal", "yes\n", false, false},
		{"accept", "yes\n", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := confirmReplacement(IO{In: strings.NewReader(tc.answer), Out: &output, Err: &output}, tc.terminal, srv, old)
			if (err == nil) != tc.allowed {
				t.Fatalf("consent: %v", err)
			}
			if tc.terminal && !strings.Contains(output.String(), "every agent and config") {
				t.Fatal("shared effects not explained")
			}
		})
	}
}
