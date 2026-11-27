//go:build unix

package hook

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/internal/fsutil"
)

// A slot answers after the slot before it has answered and ended, which
// frees its lock, or at the deadline. Which test goroutine records its time
// first says nothing about that order, so it is checked here, on the
// markers themselves.
func TestWaitForTheSlotBefore(t *testing.T) {
	d := t.TempDir()
	done, run := filepath.Join(d, "done.0.0"), filepath.Join(d, "run.0.0")
	unlock, err := fsutil.LockTimeout(run, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	returned := make(chan struct{})
	go func() {
		waitFor(done, run, time.Now().Add(time.Minute))
		close(returned)
	}()
	waiting := func(what string) {
		t.Helper()
		select {
		case <-returned:
			t.Fatalf("answered %s", what)
		case <-time.After(100 * time.Millisecond):
		}
	}
	waiting("before the slot before answered")
	if err := os.WriteFile(done, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waiting("before the slot before ended")
	unlock()
	select {
	case <-returned:
	case <-time.After(30 * time.Second):
		t.Fatal("still waiting after the slot before ended")
	}

	// The deadline ends the wait for a slot that never answers.
	start := time.Now()
	waitFor(filepath.Join(d, "done.1.0"), filepath.Join(d, "run.1.0"), start.Add(50*time.Millisecond))
	if time.Since(start) > 30*time.Second {
		t.Error("waited past the deadline")
	}
}
