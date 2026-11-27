package creds

import (
	"errors"
	"reflect"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

func TestLogoutPreservesSharedRegistrationAcrossDeclarations(t *testing.T) {
	for _, mode := range []string{"undeclared", "other URL", "other origin", "other command"} {
		t.Run(mode, func(t *testing.T) {
			keyring.MockInit()
			s := &Store{Dir: t.TempDir()}
			if _, err := s.EnsureKey(); err != nil {
				t.Fatal(err)
			}
			original := upstream.Server{ID: "work:shared", Name: "shared", Source: "work", Origin: "repo:github.com/acme/example", URL: "https://example.com/mcp"}
			client := Client{ID: "registered-app", Secret: "client-secret", Registered: true, Redirect: "http://127.0.0.1:4567/callback", CallbackPort: 4567, TokenURL: "https://example.com/token", AuthMethod: "client_secret_basic"}
			if _, err := s.Update(original, true, func(r *Record) error {
				r.Client, r.Issuer = client, "https://example.com"
				r.Token = Token{Access: "access", Refresh: "refresh"}
				r.Secrets = Secrets{Env: map[string]string{"TOKEN": "secret"}, Headers: map[string]string{"Authorization": "secret"}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			pending, err := s.Generation(original)
			if err != nil {
				t.Fatal(err)
			}
			selected := original
			switch mode {
			case "undeclared":
				selected = upstream.Server{ID: original.ID, Name: original.ID}
			case "other URL":
				selected.URL = "https://other.example/mcp"
			case "other origin":
				selected.Origin = "repo:github.com/acme/other"
			case "other command":
				selected.URL, selected.Command = "", []string{"other-mcp", "--account", "other"}
			}
			if err := s.Logout(selected); err != nil {
				t.Fatal(err)
			}
			r, err := s.Load(original)
			client.Secret = ""
			if err != nil || r == nil || !r.Matches(original) || !reflect.DeepEqual(r.Client, client) || r.Issuer != "https://example.com" {
				t.Fatalf("logout lost shared registration or binding: %+v, %v", r, err)
			}
			if r.Token.Access != "" || r.Token.Refresh != "" || len(r.Secrets.Env) != 0 || len(r.Secrets.Headers) != 0 || r.Generation <= pending {
				t.Fatalf("logout retained secrets or failed to cancel pending authorization: %+v", r)
			}
			state, err := s.State(original)
			if err != nil || state.SignedIn || state.Reason != "signed out" {
				t.Fatalf("shared identity lost signed-out state: %+v, %v", state, err)
			}
			if _, err := s.SaveSignIn(original, pending, r.Issuer, client, Token{Access: "stale"}); !errors.Is(err, ErrSignInChanged) {
				t.Fatalf("pending authorization survived logout: %v", err)
			}
		})
	}
}
