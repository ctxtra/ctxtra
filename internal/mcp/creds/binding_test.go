package creds

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/internal/mcp/oauthtest"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

func TestSourceOriginBindsCredentials(t *testing.T) {
	fake := oauthtest.New(t, oauthtest.Options{Lifetime: time.Hour})
	s, root, tok := signedInStore(t, fake, t.TempDir())
	srv := root
	srv.ID, srv.Name, srv.Source, srv.Origin = "work:linear", "linear", "work", "repo:github.com/acme/one"
	saveSignIn(t, s, srv, fake, tok)
	if err := s.SetSecrets(srv, nil, map[string]string{"Authorization": "secret"}); err != nil {
		t.Fatal(err)
	}
	srv.SecretHeaders = []string{"Authorization"}
	if token, err := s.Token(context.Background(), srv); err != nil || token != tok.AccessToken {
		t.Fatalf("same source binding: %q %v", token, err)
	}
	for _, origin := range []string{"repo:github.com/acme/two", "dir:/context", "repo:/clone", ""} {
		other := srv
		other.Origin = origin
		if token, err := s.Token(context.Background(), other); err == nil || token != "" {
			t.Fatalf("origin %q received credentials: %q %v", origin, token, err)
		}
		if _, _, err := s.Secrets(other); err == nil {
			t.Fatalf("origin %q received secret headers", origin)
		}
		if st, err := s.State(other); err != nil || st.SignedIn {
			t.Fatalf("origin %q inherited signed-in state: %+v %v", origin, st, err)
		}
		before, _ := os.ReadFile(s.recordPath(srv.ID, version))
		if err := s.CheckBinding(other); err == nil {
			t.Fatal("conflicting binding was accepted")
		}
		after, _ := os.ReadFile(s.recordPath(srv.ID, version))
		if !bytes.Equal(before, after) {
			t.Fatal("checking binding mutated credentials")
		}
	}
	if fake.Refreshes() != 0 {
		t.Fatal("origin check made a token request")
	}
	if token, err := s.Token(context.Background(), root); err != nil || token != tok.AccessToken {
		t.Fatalf("config identity no longer works: %q %v", token, err)
	}
}

func TestLegacyConfigOrigin(t *testing.T) {
	var r Record
	if err := json.Unmarshal([]byte(`{"id":"linear","url":"https://example.com/mcp"}`), &r); err != nil {
		t.Fatal(err)
	}
	root := upstream.Server{ID: "linear", Name: "linear", URL: "https://example.com/mcp"}
	if !r.Matches(root) {
		t.Fatal("old config record requires migration")
	}
	root.Origin = "repo:github.com/acme/one"
	if r.Matches(root) {
		t.Fatal("source inherited a config record")
	}
}

func TestBindingIncludesServerKind(t *testing.T) {
	r := Record{ID: "one", URL: "https://example.com", Command: []string{"tools"}}
	if r.Matches(upstream.Server{ID: "one", Command: []string{"tools"}}) || r.Matches(upstream.Server{ID: "one", URL: "https://example.com"}) {
		t.Fatal("ambiguous record matched both server kinds")
	}
}

func TestRevokeDuringAuthorizationPreventsSave(t *testing.T) {
	fake := oauthtest.New(t, oauthtest.Options{Lifetime: time.Hour})
	s, root, tok := signedInStore(t, fake, t.TempDir())
	srv := root
	srv.ID, srv.Name, srv.Source, srv.Origin, srv.Pinned = "work:linear", "linear", "work", "repo:github.com/acme/one", true
	if err := s.DecideApproval(srv, true); err != nil {
		t.Fatal(err)
	}
	gen, err := s.Generation(srv)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DecideApproval(srv, false); err != nil {
		t.Fatal(err)
	}
	var approval *ApprovalError
	if _, err := s.SaveSignIn(srv, gen, fake.URL, Client{ID: "app"}, Token{Access: tok.AccessToken}); !errors.As(err, &approval) {
		t.Fatalf("revoked authorization saved: %v", err)
	}
	if r, err := s.Load(srv); err != nil || r != nil {
		t.Fatalf("credentials were created: %+v %v", r, err)
	}
}
