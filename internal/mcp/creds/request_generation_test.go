package creds

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

func TestCredentialGenerationUsesSharedReadsAndCachedKey(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir(), LockTimeout: 25 * time.Millisecond}
	if _, err := s.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
	if _, err := s.Update(srv, true, func(r *Record) error {
		r.Generation, r.Token.Access = 123, "old-token"
		r.Token.Obtained, r.Token.Expiry = time.Now().Add(-time.Hour), time.Now().Add(time.Minute)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A markerless legacy record must not be migrated by request traffic.
	if err := os.Remove(s.generationPath(srv.ID)); err != nil {
		t.Fatal(err)
	}
	s.forgetKey()
	oldGet := getKey
	var gets atomic.Int64
	getKey = func() (string, error) { gets.Add(1); return oldGet() }
	t.Cleanup(func() { getKey = oldGet })
	func() {
		unlock, err := fsutil.LockShared(s.lockPath(fileID(srv.ID)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		for range 20 {
			generation, err := s.CredentialGeneration(srv)
			if err != nil || generation != 123 {
				t.Fatalf("shared generation read: %d, %v", generation, err)
			}
			if token, err := s.Token(context.Background(), srv); err != nil || token != "old-token" {
				t.Fatalf("shared token read: %q, %v", token, err)
			}
		}
	}()
	if n := gets.Load(); n != 1 {
		t.Fatalf("request reads queried keychain %d times; want 1", n)
	}
	if _, err := os.Stat(s.generationPath(srv.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("request read created a generation marker: %v", err)
	}
	if generation, err := s.Generation(srv); err != nil || generation != 123 {
		t.Fatalf("authorization snapshot: %d, %v", generation, err)
	}
	if _, err := os.Stat(s.generationPath(srv.ID)); err != nil {
		t.Fatalf("authorization snapshot did not retain legacy generation: %v", err)
	}
}

func TestCreateOnlySettingsAndAuthorizationPreserveGenerationZeroRecord(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir()}
	if _, err := s.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
	generation, err := s.Generation(srv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(srv, true, func(r *Record) error {
		r.Generation, r.Token.Access = 0, "existing-token"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.recordPath(srv.ID, version))
	if err != nil {
		t.Fatal(err)
	}
	committed := false
	if _, err := s.CommitSettings(srv, WriteGuard{ExpectedGeneration: &generation, CreateOnly: true}, &Settings{}, func() error {
		committed = true
		return nil
	}); err == nil || committed {
		t.Fatalf("create-only settings reached config commit: %v, %v", committed, err)
	}
	if _, err := s.SaveSignIn(srv, generation, "issuer", Client{}, Token{Access: "replacement"}, SignInChange{CreateOnly: true}); err == nil {
		t.Fatal("create-only authorization replaced an existing record")
	}
	after, err := os.ReadFile(s.recordPath(srv.ID, version))
	if err != nil || string(before) != string(after) {
		t.Fatalf("refused write changed existing credentials: %v", err)
	}
}
