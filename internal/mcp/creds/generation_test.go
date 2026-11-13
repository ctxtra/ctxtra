package creds

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

func TestDeletionInvalidatesGenerationZeroAndSurvivesReadd(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "first login", true: "legacy record"}[legacy], func(t *testing.T) {
			keyring.MockInit()
			s := &Store{Dir: t.TempDir(), Now: func() time.Time { return time.Unix(100, 0) }}
			if _, err := s.EnsureKey(); err != nil {
				t.Fatal(err)
			}
			srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
			if legacy {
				if _, err := s.Update(srv, true, func(r *Record) error { r.Token.Access = "legacy"; r.Generation = 0; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			pending, err := s.Generation(srv)
			if err != nil || pending != 0 {
				t.Fatalf("initial generation: %d, %v", pending, err)
			}
			if err := s.Delete(srv); err != nil {
				t.Fatal(err)
			}
			current, err := s.Generation(srv)
			if err != nil || current <= pending {
				t.Fatalf("deletion generation: %d, %v", current, err)
			}
			if _, err := s.SaveSignIn(srv, pending, "issuer", Client{}, Token{Access: "stale"}); !errors.Is(err, ErrSignInChanged) {
				t.Fatalf("generation-zero authorization survived removal: %v", err)
			}
			if _, err := s.SaveSettings(srv, WriteGuard{ExpectedGeneration: &pending}, Settings{Secrets: &Secrets{}}); !errors.Is(err, ErrSignInChanged) {
				t.Fatalf("generation-zero secret prompt survived removal: %v", err)
			}
			if r, err := s.Load(srv); err != nil || r != nil {
				t.Fatalf("late authorization recreated credentials: %+v, %v", r, err)
			}
			r, err := s.SaveSignIn(srv, current, "issuer", Client{}, Token{Access: "fresh"})
			if err != nil || r.Generation <= current {
				t.Fatalf("fresh authorization after removal: %+v, %v", r, err)
			}
			pending = r.Generation
			if err := s.Delete(srv); err != nil {
				t.Fatal(err)
			}
			current, err = s.Generation(srv)
			if err != nil || current <= pending {
				t.Fatalf("deletion reused a generation with a fixed clock: %d, %v", current, err)
			}
			if _, err := s.SaveSignIn(srv, current, "issuer", Client{}, Token{Access: "readded"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveSignIn(srv, pending, "issuer", Client{}, Token{Access: "stale"}); !errors.Is(err, ErrSignInChanged) {
				t.Fatalf("old authorization survived remove and readd: %v", err)
			}
		})
	}
}

func TestDeleteWithoutAKeyStillInvalidatesPendingWrites(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
	if err := s.Delete(srv); err != nil {
		t.Fatal(err)
	}
	if generation, err := s.Generation(srv); err != nil || generation == 0 {
		t.Fatalf("keyless deletion lost cancellation history: %d, %v", generation, err)
	}
	if _, err := os.Stat(s.keyInfoPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deletion created a credential key: %v", err)
	}
}

func TestGenerationMarkerFailsClosed(t *testing.T) {
	for _, data := range []string{`{"version":2,"generation":42}`, `{"version":1,"generation":-1}`, `{`} {
		t.Run(data, func(t *testing.T) {
			s := &Store{Dir: t.TempDir()}
			srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
			if err := writeDurable(s.generationPath(srv.ID), []byte(data)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Generation(srv); err == nil {
				t.Fatal("invalid generation marker accepted")
			}
			if err := s.Delete(srv); err == nil {
				t.Fatal("invalid generation marker overwritten")
			}
			if _, err := s.SaveSignIn(srv, 0, "issuer", Client{}, Token{Access: "stale"}); err == nil {
				t.Fatal("invalid generation marker bypassed by authorization")
			}
			after, _ := os.ReadFile(s.generationPath(srv.ID))
			if string(after) != data {
				t.Fatal("failed operation changed generation marker")
			}
		})
	}
}

func TestInspectRetainsLegacyGenerationBeforeKeylessDeletion(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir()}
	if _, err := s.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
	if _, err := s.Update(srv, true, func(r *Record) error { r.Generation = 123; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.generationPath(srv.ID)); err != nil {
		t.Fatal(err)
	}
	_, pending, err := s.Inspect(srv)
	if err != nil || pending != 123 {
		t.Fatalf("legacy snapshot: %d, %v", pending, err)
	}
	keyring.MockInitWithError(errors.New("key unavailable"))
	other := &Store{Dir: s.Dir}
	if err := other.Delete(srv); err != nil {
		t.Fatal(err)
	}
	if _, err := other.SaveSignIn(srv, pending, "issuer", Client{}, Token{Access: "stale"}); !errors.Is(err, ErrSignInChanged) {
		t.Fatalf("legacy authorization survived keyless deletion: %v", err)
	}
}

func TestFailedCredentialSaveCanBeRetriedWithCurrentGeneration(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir()}
	if _, err := s.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
	if err := s.SetSecrets(srv, nil, map[string]string{"Authorization": "old"}); err != nil {
		t.Fatal(err)
	}
	oldGeneration, err := s.Generation(srv)
	if err != nil {
		t.Fatal(err)
	}
	oldSync := syncFile
	t.Cleanup(func() { syncFile = oldSync })
	syncFile = func(f *os.File) error {
		if filepath.Dir(f.Name()) == filepath.Join(s.Dir, "credentials") {
			return errors.New("credential sync failed")
		}
		return oldSync(f)
	}
	if err := s.SetSecrets(srv, nil, map[string]string{"Authorization": "failed"}); err == nil {
		t.Fatal("credential save unexpectedly succeeded")
	}
	syncFile = oldSync
	r, generation, err := s.Inspect(srv)
	if err != nil || r.Secrets.Headers["Authorization"] != "old" || generation <= oldGeneration {
		t.Fatalf("failed save snapshot: %+v, generation %d, %v", r, generation, err)
	}
	if _, err := s.SaveSignIn(srv, oldGeneration, "issuer", Client{}, Token{Access: "stale"}); !errors.Is(err, ErrSignInChanged) {
		t.Fatalf("authorization survived an intervening mutation attempt: %v", err)
	}
	if _, err := s.SaveSettings(srv, WriteGuard{ExpectedGeneration: &generation}, Settings{Secrets: &Secrets{Headers: map[string]string{"Authorization": "retry"}}}); err != nil {
		t.Fatalf("fresh retry failed: %v", err)
	}
}

func TestBindingChangesAdvanceGeneration(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir(), Now: func() time.Time { return time.Unix(100, 0) }}
	if _, err := s.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	old := upstream.Server{ID: "shared", Name: "shared", URL: "https://old.example/mcp"}
	if _, err := s.Update(old, true, func(r *Record) error { r.Generation = 123; return nil }); err != nil {
		t.Fatal(err)
	}
	pending, err := s.Generation(old)
	if err != nil {
		t.Fatal(err)
	}
	other := old
	other.URL = "https://other.example/mcp"
	if _, err := s.Update(other, true, func(r *Record) error { r.Generation = pending; r.Token.Access = "other"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSignIn(old, pending, "issuer", Client{}, Token{Access: "stale"}, SignInChange{Replace: true}); !errors.Is(err, ErrSignInChanged) {
		t.Fatalf("replacement consent survived another binding change: %v", err)
	}
}
