//go:build unix

package hook

import (
	"os"
	"path/filepath"
	"sync"
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

	// An expired deadline ends the wait at once: while the slot before has
	// not answered, and while it has answered but not ended.
	expired := time.Now().Add(-time.Second)
	held := filepath.Join(d, "run.1.0")
	unlockHeld, err := fsutil.LockTimeout(held, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockHeld()
	if err := os.WriteFile(filepath.Join(d, "done.1.0"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, done, run string }{
		{"not answered", filepath.Join(d, "done.2.0"), filepath.Join(d, "run.2.0")},
		{"answered, not ended", filepath.Join(d, "done.1.0"), held},
	} {
		returned := make(chan struct{})
		go func() {
			waitFor(c.done, c.run, expired)
			close(returned)
		}()
		select {
		case <-returned:
		case <-time.After(30 * time.Second):
			t.Fatalf("%s: still waiting after the deadline", c.name)
		}
	}
}

// A slot that answers late, well within orderWait, still answers before
// the slot after it: the time is taken in the hook, right before each
// slot writes its answer.
func TestSlotsAnswerAfterTheSlotBefore(t *testing.T) {
	_, r := bigSession(t, 100_000)
	var mu sync.Mutex
	at := map[int]time.Time{}
	orig := beforeAnswer
	beforeAnswer = func(slot int) {
		if slot == 3 {
			time.Sleep(300 * time.Millisecond)
		}
		mu.Lock()
		at[slot] = time.Now()
		mu.Unlock()
	}
	t.Cleanup(func() { beforeAnswer = orig })
	got := runSlots(t, "SessionStart", `{"cwd": "`+r+`", "session_id": "s1", "source": "startup"}`)
	if got[4].text == "" {
		t.Fatal("no fifth part")
	}
	if !at[4].After(at[3]) {
		t.Errorf("slot 4 answered %v after slot 3", at[4].Sub(at[3]))
	}
}
