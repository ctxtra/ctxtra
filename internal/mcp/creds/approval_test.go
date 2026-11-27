package creds

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

func TestApprovalIsSeparateAndLocal(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	srv := upstream.Server{ID: "work:linear", Name: "linear", Source: "work", Origin: "repo:github.com/acme/repo", Pinned: true, URL: "https://example.com/mcp"}
	var needed *ApprovalError
	if err := s.RequireApproval(srv); !errors.As(err, &needed) || needed.ID != srv.ID {
		t.Fatalf("missing approval: %v", err)
	}
	for _, allowed := range []bool{false, true, false} {
		if err := s.DecideApproval(srv, allowed); err != nil {
			t.Fatal(err)
		}
		a, err := (&Store{Dir: s.Dir}).Approval(srv)
		if err != nil || a.Version == 0 || a.Allowed != allowed {
			t.Fatalf("persisted decision: %+v, %v", a, err)
		}
		if err := s.RequireApproval(srv); (err == nil) != allowed {
			t.Fatalf("allowed=%v: %v", allowed, err)
		}
	}
	for _, name := range []string{"key", "key.json", "credentials", "state"} {
		if _, err := os.Stat(filepath.Join(s.Dir, name)); !os.IsNotExist(err) {
			t.Fatalf("approval touched %s: %v", name, err)
		}
	}
	info, err := os.Stat(filepath.Join(s.Dir, "approval", fileID(srv.ID)+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("approval permissions: %v, %v", info, err)
	}
	srv.Pinned = false
	if err := s.RequireApproval(srv); err != nil {
		t.Fatalf("trusted/config needs no extra approval: %v", err)
	}
}

func TestApprovalFingerprint(t *testing.T) {
	base := upstream.Server{ID: "work:linear", Name: "linear", Source: "work", Origin: "repo:github.com/acme/repo", Pinned: true,
		URL: "https://example.com/mcp", Headers: map[string]string{"X-Tenant": "one", "X-Mode": "read"},
		ClientID: "app", Scopes: []string{"read"}, SecretHeaders: []string{"Authorization"}}
	for name, change := range map[string]func(*upstream.Server){
		"ID":      func(s *upstream.Server) { s.ID = "personal/linear" },
		"origin":  func(s *upstream.Server) { s.Origin = "repo:github.com/acme/other" },
		"url":     func(s *upstream.Server) { s.URL += "/other" },
		"headers": func(s *upstream.Server) { s.Headers = map[string]string{"X-Tenant": "two"} },
		"client":  func(s *upstream.Server) { s.ClientID = "other" },
		"scopes":  func(s *upstream.Server) { s.Scopes = []string{"read", "write"} },
		"secrets": func(s *upstream.Server) { s.SecretHeaders = []string{"X-Token"} },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			change(&changed)
			if Fingerprint(changed) == Fingerprint(base) {
				t.Fatal("operational change retained approval")
			}
		})
	}
	same := base
	same.Notes = "Another workspace reminder"
	same.Headers = map[string]string{"X-Mode": "read", "X-Tenant": "one"}
	if Fingerprint(same) != Fingerprint(base) {
		t.Fatal("notes or map insertion order changed approval")
	}
	renamed := base
	renamed.Name = "linear_work"
	if Fingerprint(renamed) != Fingerprint(base) {
		t.Fatal("a new name in the config changed the approval of SOURCE:SERVER")
	}
	local := upstream.Server{ID: "work:build", Name: "build", Origin: "repo:github.com/acme/repo", Pinned: true, Command: []string{"tools", "--read"}}
	for name, change := range map[string]func(*upstream.Server){
		"argv":       func(s *upstream.Server) { s.Command = []string{"tools", "--write"} },
		"env":        func(s *upstream.Server) { s.Env = map[string]string{"MODE": "write"} },
		"pass-env":   func(s *upstream.Server) { s.EnvPass = []string{"TOKEN"} },
		"secret-env": func(s *upstream.Server) { s.SecretEnv = []string{"TOKEN"} },
	} {
		t.Run(name, func(t *testing.T) {
			changed := local
			change(&changed)
			if Fingerprint(changed) == Fingerprint(local) {
				t.Fatal("command change retained approval")
			}
		})
	}
}

func TestFailedApprovalSaveDoesNotGrant(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	srv := upstream.Server{ID: "work:tools", Name: "tools", Pinned: true, Command: []string{"tools"}}
	old := syncDir
	t.Cleanup(func() { syncDir = old })
	syncDir = func(string) error { return errors.New("I/O failure") }
	if err := s.DecideApproval(srv, true); err == nil {
		t.Fatal("failed save reported success")
	}
	if err := s.RequireApproval(srv); err == nil {
		t.Fatal("failed save left a readable approval")
	}
}

func TestNewerApprovalIsNotOverwritten(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	srv := upstream.Server{ID: "work:tools", Name: "tools", Pinned: true, Command: []string{"tools"}}
	path := filepath.Join(s.Dir, "approval", fileID(srv.ID)+".json")
	if err := writeDurable(path, []byte(`{"version":2,"allowed":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.RequireApproval(srv); err == nil {
		t.Fatal("unknown approval format granted access")
	}
	if err := s.DecideApproval(srv, true); err == nil {
		t.Fatal("unknown format was overwritten")
	}
}
