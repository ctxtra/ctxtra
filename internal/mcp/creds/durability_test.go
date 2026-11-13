package creds

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

func TestApprovalCannotBeUsedBeforeDurableSave(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	srv := upstream.Server{ID: "work/local", Name: "local", Pinned: true, Command: []string{"local-mcp"}}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	oldSync := syncDir
	syncDir = func(dir string) error {
		fail := false
		if dir == filepath.Join(s.Dir, "approval") {
			once.Do(func() { close(entered); <-release; fail = true })
		}
		if fail {
			return errors.New("approval sync failed")
		}
		return oldSync(dir)
	}
	writeErr := make(chan error, 1)
	go func() { defer close(done); writeErr <- s.DecideApproval(srv, true) }()
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); <-done; syncDir = oldSync })
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("approval writer did not reach directory sync")
	}
	read := make(chan error, 1)
	go func() { read <- (&Store{Dir: s.Dir}).RequireApproval(srv) }()
	select {
	case err := <-read:
		if err == nil {
			t.Fatal("approval was usable before its save finished")
		}
	case <-time.After(time.Second):
		t.Fatal("approval inspection waited for a pending write")
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-writeErr; err == nil {
		t.Fatal("approval save unexpectedly succeeded")
	}
	if err := s.RequireApproval(srv); err == nil {
		t.Fatal("failed approval save left usable consent")
	}
}

func TestApprovalDoesNotWaitForCredentialLock(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	srv := upstream.Server{ID: "work/local", Name: "local", Pinned: true, Command: []string{"local-mcp"}}
	if err := s.DecideApproval(srv, true); err != nil {
		t.Fatal(err)
	}
	unlock, err := s.lock(srv)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	read := make(chan error, 1)
	go func() { read <- s.RequireApproval(srv) }()
	select {
	case err := <-read:
		if err != nil {
			t.Fatalf("durable approval unavailable during credential work: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("approval inspection waited for credential work")
	}
	write := make(chan error, 1)
	go func() { write <- s.DecideApproval(srv, false) }()
	select {
	case err := <-write:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("revocation waited for credential work")
	}
}

func TestLoadCannotUseCredentialsFromFailedSave(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir()}
	if _, err := s.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
	if err := s.SetClientSecret(srv, "old-client-secret"); err != nil {
		t.Fatal(err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	oldSync := syncDir
	syncDir = func(dir string) error {
		fail := false
		if dir == filepath.Join(s.Dir, "credentials") {
			once.Do(func() { close(entered); <-release; fail = true })
		}
		if fail {
			return errors.New("credential sync failed")
		}
		return oldSync(dir)
	}
	writeErr := make(chan error, 1)
	go func() { defer close(done); writeErr <- s.SetClientSecret(srv, "replacement-client-secret") }()
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); <-done; syncDir = oldSync })
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("credential writer did not reach directory sync")
	}
	type result struct {
		record *Record
		err    error
	}
	read := make(chan result, 1)
	go func() { r, err := (&Store{Dir: s.Dir}).Load(srv); read <- result{r, err} }()
	select {
	case <-read:
		t.Fatal("credential reader passed a writer before its save finished")
	case <-time.After(200 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-writeErr; err == nil {
		t.Fatal("credential save unexpectedly succeeded")
	}
	select {
	case got := <-read:
		if got.err != nil || got.record == nil || got.record.Client.Secret != "old-client-secret" {
			t.Fatalf("reader used rolled-back credentials: %+v, %v", got.record, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("credential reader did not finish after rollback")
	}
}
