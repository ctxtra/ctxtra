package creds

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

func TestSettingsRefuseConflictingBindingsAtomically(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir()}
	if _, err := s.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	old := upstream.Server{ID: "shared", Name: "shared", URL: "https://one.example/mcp"}
	if _, err := s.Update(old, true, func(r *Record) error {
		r.Token.Access, r.Client.Secret = "old-token", "old-client-secret"
		r.Generation = 123
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	other := old
	other.URL = "https://two.example/mcp"
	before, _ := os.ReadFile(s.recordPath(old.ID, version))
	for _, set := range []func() error{
		func() error { return s.SetSecrets(other, nil, map[string]string{"Authorization": "new"}) },
		func() error { return s.SetClientSecret(other, "new") },
		func() error { return s.SetCallbackPort(other, 1234) },
		func() error {
			_, err := s.SaveSignIn(other, 123, "issuer", Client{}, Token{Access: "new"})
			return err
		},
	} {
		if err := set(); err == nil {
			t.Fatal("conflicting write succeeded without replacement consent")
		}
		after, _ := os.ReadFile(s.recordPath(old.ID, version))
		if !bytes.Equal(before, after) {
			t.Fatal("refused write changed encrypted credentials")
		}
	}
}

func TestSettingsCheckExpectedGenerationBeforeAnyFieldChanges(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir()}
	if _, err := s.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	srv := upstream.Server{ID: "shared", Name: "shared", Command: []string{"shared-mcp"}}
	if err := s.SetSecrets(srv, map[string]string{"TOKEN": "old"}, nil); err != nil {
		t.Fatal(err)
	}
	generation, err := s.Generation(srv)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.recordPath(srv.ID, version))
	wrongGeneration := generation - 1
	clientSecret, callbackPort := "new-client-secret", 5678
	settings := Settings{Secrets: &Secrets{Env: map[string]string{"TOKEN": "new"}}, ClientSecret: &clientSecret, CallbackPort: &callbackPort}
	if _, err := s.SaveSettings(srv, WriteGuard{ExpectedGeneration: &wrongGeneration, Replace: true}, settings); !errors.Is(err, ErrSignInChanged) {
		t.Fatalf("stale settings: %v", err)
	}
	after, _ := os.ReadFile(s.recordPath(srv.ID, version))
	if !bytes.Equal(before, after) {
		t.Fatal("stale settings partially changed credentials")
	}
	r, err := s.SaveSettings(srv, WriteGuard{ExpectedGeneration: &generation}, settings)
	if err != nil || r.Secrets.Env["TOKEN"] != "new" || r.Client.Secret != clientSecret || r.Client.CallbackPort != callbackPort || r.Generation <= generation {
		t.Fatalf("settings commit: %+v, %v", r, err)
	}
}

func TestReplacementSettingsRequireConsentAndGeneration(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir()}
	if _, err := s.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	old := upstream.Server{ID: "work/shared", Name: "shared", Origin: "git:one", URL: "https://one.example/mcp"}
	if _, err := s.Update(old, true, func(r *Record) error {
		r.Token.Access, r.Token.Refresh = "old-token", "old-refresh"
		r.Client.Secret, r.Generation = "old-client", 123
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	other := old
	other.Origin = "git:two"
	settings := Settings{Secrets: &Secrets{Headers: map[string]string{"Authorization": "new"}}}
	if _, err := s.SaveSettings(other, WriteGuard{Replace: true}, settings); err == nil {
		t.Fatal("replacement accepted without inspected generation")
	}
	generation := int64(123)
	r, err := s.SaveSettings(other, WriteGuard{ExpectedGeneration: &generation, Replace: true}, settings)
	if err != nil || !r.Matches(other) || r.Secrets.Headers["Authorization"] != "new" || r.Token.Access != "" || r.Token.Refresh != "" || r.Client.Secret != "" {
		t.Fatalf("explicit replacement copied old authorization: %+v, %v", r, err)
	}
}
