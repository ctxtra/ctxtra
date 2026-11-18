package cli

import (
	"bytes"
	"errors"
	"io"
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

type credentialHookWriter struct {
	io.Writer
	hook func()
}

func (w *credentialHookWriter) Write(p []byte) (int, error) {
	if w.hook != nil && bytes.Contains(p, []byte("warning: no OS keychain")) {
		hook := w.hook
		w.hook = nil
		hook()
	}
	return w.Writer.Write(p)
}

type credentialHookReader struct {
	io.Reader
	hook func()
}

func (r *credentialHookReader) Read(p []byte) (int, error) {
	if r.hook != nil {
		hook := r.hook
		r.hook = nil
		hook()
	}
	return r.Reader.Read(p)
}

func TestMCPAddRechecksBindingAtCredentialWrite(t *testing.T) {
	h := testenv.Home(t)
	keyring.MockInitWithError(errors.New("test: no keychain"))
	store := openStore()
	if _, err := store.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	target := upstream.Server{ID: "shared", Name: "shared", URL: "https://a.example/mcp"}
	file := testenv.Config(t, h, "schema: ctxtra/v1\n")
	before, _ := os.ReadFile(file)
	other := target
	other.URL = "https://b.example/mcp"
	var out, stderr bytes.Buffer
	writer := &credentialHookWriter{Writer: &stderr, hook: func() {
		if _, err := store.Update(other, true, func(r *creds.Record) error { r.Token.Access = "newer-other-login"; return nil }); err != nil {
			t.Fatal(err)
		}
	}}
	n := mcpAdd(IO{In: strings.NewReader("new-header\n"), Out: &out, Err: writer}, "shared", target.URL, nil,
		mcpAddOptions{noLogin: true, secretHeaders: []string{"Authorization"}, clientID: "app", clientSecret: false, signIn: signInOptions{callbackPort: 4567}})
	if writer.hook != nil {
		t.Fatal("intervening credential write never ran")
	}
	r, err := openStore().Load(other)
	if n == 0 || err != nil || r == nil || !r.Matches(other) || r.Token.Access != "newer-other-login" || r.Client.CallbackPort != 0 {
		t.Fatalf("add overwrote newer credentials: exit %d, record %+v, error %v, output %s %s", n, r, err, &out, &stderr)
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(before, after) {
		t.Fatal("conflicting add changed config")
	}
}

func TestMCPLoginRechecksBindingAfterPrompt(t *testing.T) {
	h := testenv.Home(t)
	keyring.MockInit()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer remote.Close()
	testenv.Config(t, h, "schema: ctxtra/v1\nmcp-servers:\n  shared: {url: '"+remote.URL+"', secret-headers: [Authorization]}\n")
	store := openStore()
	if _, err := store.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	target := upstream.Server{ID: "shared", Name: "shared", URL: remote.URL, SecretHeaders: []string{"Authorization"}}
	if err := store.SetSecrets(target, nil, map[string]string{"Authorization": "initial-header"}); err != nil {
		t.Fatal(err)
	}
	other := target
	other.URL = "https://other.example/mcp"
	reader := &credentialHookReader{Reader: strings.NewReader("new-header\n"), hook: func() {
		if _, err := store.Update(other, true, func(r *creds.Record) error { r.Token.Access = "concurrent-login"; return nil }); err != nil {
			t.Fatal(err)
		}
	}}
	var output bytes.Buffer
	a := &app{IO: IO{In: reader, Out: &output, Err: &output}}
	n := a.mcpLogin("shared", false, signInOptions{})
	r, err := store.Load(other)
	if n == 0 || err != nil || r == nil || !r.Matches(other) || r.Token.Access != "concurrent-login" {
		t.Fatalf("login overwrote newer credentials: exit %d, record %+v, error %v, output %s", n, r, err, &output)
	}
}

func TestMCPReplacementFailureKeepsOldRecord(t *testing.T) {
	for _, step := range []string{"client-secret prompt", "OAuth discovery"} {
		t.Run(step, func(t *testing.T) {
			h := testenv.Home(t)
			keyring.MockInit()
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="test"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer remote.Close()
			testenv.Config(t, h, "schema: ctxtra/v1\nmcp-servers:\n  shared: {url: '"+remote.URL+"', secret-headers: [Authorization], client-id: app}\n")
			store := openStore()
			if _, err := store.EnsureKey(); err != nil {
				t.Fatal(err)
			}
			old := upstream.Server{ID: "shared", Name: "shared", URL: "https://old.example/mcp"}
			if _, err := store.Update(old, true, func(r *creds.Record) error {
				r.Token.Access, r.Token.Refresh, r.Client.Secret = "old-login", "old-refresh", "old-client-secret"
				r.Generation = 123
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			files, _ := filepath.Glob(filepath.Join(store.Dir, "credentials", "*.age"))
			before, _ := os.ReadFile(files[0])
			interactive := Interactive
			t.Cleanup(func() { Interactive = interactive })
			Interactive = func(IO) bool { return true }
			input := "yes\nreplacement-header\n"
			if step == "OAuth discovery" {
				input += "replacement-client-secret\n"
			}
			var output bytes.Buffer
			a := &app{IO: IO{In: strings.NewReader(input), Out: &output, Err: &output}}
			if n := a.mcpLogin("shared", true, signInOptions{}); n != 1 {
				t.Fatalf("replacement unexpectedly succeeded: %s", &output)
			}
			if step == "OAuth discovery" && strings.Contains(output.String(), "no value on stdin") {
				t.Fatalf("did not reach OAuth after all prompts: %s", &output)
			}
			after, _ := os.ReadFile(files[0])
			if !bytes.Equal(before, after) {
				t.Fatalf("failed replacement changed old credentials: %s", &output)
			}
		})
	}
}
