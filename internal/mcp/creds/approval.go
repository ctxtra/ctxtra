package creds

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

const approvalVersion = 1

// Approval records terminal consent separately from credentials and sign-in state.
type Approval struct {
	Version     int    `json:"version"`
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Allowed     bool   `json:"allowed"`
}

// Fingerprint covers operational settings, identity and origin, not notes,
// the source commit or the name the config serves a source's server under:
// a new name for SOURCE:SERVER keeps its approval. JSON orders map keys
// deterministically.
func Fingerprint(srv upstream.Server) string {
	srv.Notes, srv.Name = "", ""
	data, _ := json.Marshal(struct {
		Version int
		Server  upstream.Server
	}{approvalVersion, srv})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Store) approvalLockPath(id string) string {
	return filepath.Join(s.Dir, "approval", fileID(id)+".lock")
}

// Approval reads durable local consent without a key. A busy writer fails
// closed without waiting; approval locks never cover credential or network work.
func (s *Store) Approval(srv upstream.Server) (Approval, error) {
	if _, err := os.Stat(filepath.Join(s.Dir, "approval", fileID(srv.ID)+".json")); errors.Is(err, os.ErrNotExist) {
		return Approval{}, nil
	}
	unlock, err := fsutil.LockShared(s.approvalLockPath(srv.ID), 0)
	if err != nil {
		return Approval{}, err
	}
	defer unlock()
	return s.readApproval(srv)
}

func (s *Store) readApproval(srv upstream.Server) (Approval, error) {
	data, err := fsutil.ReadBounded(filepath.Join(s.Dir, "approval", fileID(srv.ID)+".json"), maxRecord)
	if errors.Is(err, os.ErrNotExist) {
		return Approval{}, nil
	}
	if err != nil {
		return Approval{}, err
	}
	var a Approval
	if err := json.Unmarshal(data, &a); err != nil {
		return Approval{}, err
	}
	if a.Version != approvalVersion {
		return Approval{}, fmt.Errorf("the approval of %s has an unsupported format; upgrade ctxtra", srv.ID)
	}
	if a.ID != srv.ID || a.Fingerprint != Fingerprint(srv) {
		return Approval{}, nil
	}
	return a, nil
}

// DecideApproval durably records consent or decline under its approval
// lock. It neither creates an encryption key nor contacts the server.
func (s *Store) DecideApproval(srv upstream.Server, allowed bool) error {
	if err := s.mkdir("approval"); err != nil {
		return err
	}
	unlock, err := fsutil.LockTimeout(s.approvalLockPath(srv.ID), s.lockTimeout())
	if err != nil {
		return err
	}
	defer unlock()
	// Unknown formats must not be overwritten by an older writer.
	if _, err := s.readApproval(srv); err != nil {
		return err
	}
	a := Approval{Version: approvalVersion, ID: srv.ID, Fingerprint: Fingerprint(srv), Allowed: allowed}
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	path := filepath.Join(s.Dir, "approval", fileID(srv.ID)+".json")
	err = writeDurable(path, append(data, '\n'))
	var nd *notDurable
	if allowed && errors.As(err, &nd) {
		// Never leave a newly granted approval readable after a failed save.
		_ = os.Remove(path)
		_ = syncDir(filepath.Dir(path))
	}
	return err
}

// ApprovalError is a pinned definition that cannot be contacted or run.
type ApprovalError struct{ ID string }

func (e *ApprovalError) Error() string {
	return e.ID + ": approval needed; run ctxtra mcp approve " + e.ID
}

// RequireApproval refuses unapproved pinned definitions, including no-auth
// endpoints. Config and trusted definitions require no extra approval.
func (s *Store) RequireApproval(srv upstream.Server) error {
	if !srv.Pinned {
		return nil
	}
	a, err := s.Approval(srv)
	if err != nil {
		return err
	}
	if !a.Allowed {
		return &ApprovalError{ID: srv.ID}
	}
	return nil
}
