package profile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// forEvent is ForEvent, which must not fail.
func forEvent(t *testing.T, e Event) Active {
	t.Helper()
	a, err := ForEvent(e)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// claude is an event as Claude Code sends it: a subagent's session_id is
// its parent's.
func claude(kind Kind, id string) Event {
	e := Event{Agent: "claude-code", Kind: kind, Session: id}
	if kind == Subagent {
		e.Parent = id
	}
	return e
}

func records(t *testing.T, h string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(h, ".local/state/ctxtra/sessions/*.json"))
	return m
}

func TestASessionKeepsItsProfiles(t *testing.T) {
	t.Run("switching while a session runs", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam")
		if a := forEvent(t, claude(Start, "s1")); !slices.Equal(a.Names, []string{"iam"}) || a.Kept {
			t.Fatalf("start: %+v", a)
		}
		store(t, "observability")
		a := forEvent(t, claude(Subagent, "s1"))
		if !slices.Equal(a.Names, []string{"iam"}) || !a.Kept || a.Origin != FromStored {
			t.Errorf("subagent: %+v", a)
		}
		if s := a.String(); s != "iam (from stored selection, kept from the session's start)" {
			t.Errorf("%q", s)
		}
	})
	t.Run("compaction", func(t *testing.T) {
		testenv.Home(t)
		t.Setenv(Env, "iam")
		forEvent(t, claude(Start, "s1"))
		os.Unsetenv(Env)
		store(t, "observability")
		if a := forEvent(t, claude(Continue, "s1")); !slices.Equal(a.Names, []string{"iam"}) || !a.Kept || a.Origin != FromEnv {
			t.Errorf("%+v", a)
		}
	})
	t.Run("clear starts over", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam")
		forEvent(t, claude(Start, "s1"))
		store(t, "observability")
		if a := forEvent(t, claude(Start, "s1")); !slices.Equal(a.Names, []string{"observability"}) || a.Kept {
			t.Errorf("clear: %+v", a)
		}
		if a := forEvent(t, claude(Subagent, "s1")); !slices.Equal(a.Names, []string{"observability"}) || !a.Kept {
			t.Errorf("subagent: %+v", a)
		}
	})
	t.Run("sessions are kept apart", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam")
		forEvent(t, claude(Start, "s1"))
		store(t, "observability")
		forEvent(t, claude(Start, "s2"))
		forEvent(t, Event{Agent: "codex", Kind: Start, Session: "s1"})
		for _, c := range []struct {
			e    Event
			want string
		}{{claude(Subagent, "s1"), "iam"}, {claude(Subagent, "s2"), "observability"}, {Event{Agent: "codex", Kind: Subagent, Parent: "s1"}, "observability"}} {
			if a := forEvent(t, c.e); !slices.Equal(a.Names, []string{c.want}) {
				t.Errorf("%+v: %+v", c.e, a)
			}
		}
	})
	// An agent whose subagent has a session ID of its own: the subagent's
	// later events find the profiles under it.
	t.Run("a subagent with its own ID", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam")
		forEvent(t, Event{Agent: "muse", Kind: Start, Session: "p"})
		store(t, "observability")
		if a := forEvent(t, Event{Agent: "muse", Kind: Subagent, Session: "c", Parent: "p"}); !slices.Equal(a.Names, []string{"iam"}) || !a.Kept {
			t.Errorf("subagent: %+v", a)
		}
		if a := forEvent(t, Event{Agent: "muse", Kind: Continue, Session: "c"}); !slices.Equal(a.Names, []string{"iam"}) || !a.Kept {
			t.Errorf("subagent's compaction: %+v", a)
		}
	})
	t.Run("a resume without a record", func(t *testing.T) {
		h := testenv.Home(t)
		store(t, "iam")
		if a := forEvent(t, claude(Continue, "s1")); !slices.Equal(a.Names, []string{"iam"}) || a.Kept {
			t.Errorf("resume: %+v", a)
		}
		if len(records(t, h)) != 1 {
			t.Error("no record")
		}
		store(t, "observability")
		if a := forEvent(t, claude(Subagent, "s1")); !slices.Equal(a.Names, []string{"iam"}) {
			t.Errorf("subagent: %+v", a)
		}
	})
	t.Run("records are readable by the user only", func(t *testing.T) {
		h := testenv.Home(t)
		forEvent(t, claude(Start, "s1"))
		r := records(t, h)
		if len(r) != 1 {
			t.Fatalf("%v", r)
		}
		for _, p := range []string{r[0], filepath.Dir(r[0])} {
			if st, err := os.Stat(p); err != nil || st.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s: %v %v", p, st.Mode(), err)
			}
		}
	})
}

func TestKeepingProfilesNeverBreaksASession(t *testing.T) {
	t.Run("agent without a session ID", func(t *testing.T) {
		h := testenv.Home(t)
		store(t, "iam")
		forEvent(t, Event{Agent: "cursor", Kind: Start})
		store(t, "observability")
		for _, e := range []Event{{Agent: "cursor", Kind: Subagent}, {Agent: "cursor", Kind: Continue}, {Agent: "cursor"}} {
			if a := forEvent(t, e); !slices.Equal(a.Names, []string{"observability"}) || a.Kept {
				t.Errorf("%+v: %+v", e, a)
			}
		}
		if r := records(t, h); len(r) != 0 {
			t.Errorf("records: %v", r)
		}
	})
	t.Run("cannot record", func(t *testing.T) {
		h := testenv.Home(t)
		store(t, "iam")
		testenv.Write(t, filepath.Join(h, ".local/state/ctxtra/sessions"), "a file, not a folder")
		for _, e := range []Event{claude(Start, "s1"), claude(Subagent, "s1"), claude(Continue, "s1")} {
			a, err := ForEvent(e)
			if !slices.Equal(a.Names, []string{"iam"}) || a.Kept || err == nil {
				t.Errorf("%+v: %+v %v", e, a, err)
			}
		}
	})
	t.Run("a record that can't be read", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam")
		testenv.Write(t, recordPath("claude-code", "s1"), "{")
		store(t, "observability")
		a, err := ForEvent(claude(Subagent, "s1"))
		if !slices.Equal(a.Names, []string{"observability"}) || a.Kept || err == nil || !strings.HasPrefix(err.Error(), "can't read the session's profiles: ") {
			t.Errorf("%+v %v", a, err)
		}
		// Replaced by the current selection.
		if a := forEvent(t, claude(Subagent, "s1")); !slices.Equal(a.Names, []string{"observability"}) || !a.Kept {
			t.Errorf("next: %+v", a)
		}
	})
	t.Run("a broken stored selection", func(t *testing.T) {
		h := testenv.Home(t)
		testenv.Write(t, filepath.Join(h, ".local/state/ctxtra/profiles.json"), "{")
		a := forEvent(t, claude(Start, "s1"))
		if len(a.Names) != 0 || len(a.Errors) != 1 {
			t.Errorf("%+v", a)
		}
	})
}

// setNow makes it t from now on in this package.
func setNow(t *testing.T, at time.Time) {
	old := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = old })
}

func TestCleanup(t *testing.T) {
	h := testenv.Home(t)
	var started [][]string
	old := spawn
	spawn = func(args []string) error { started = append(started, args); return nil }
	t.Cleanup(func() { spawn = old })
	start := time.Now()

	// Sessions start the cleanup once a day.
	forEvent(t, claude(Start, "old"))
	forEvent(t, claude(Start, "used"))
	forEvent(t, claude(Start, "other"))
	if len(started) != 1 || !slices.Equal(started[0], []string{"hook", "--clean-sessions"}) {
		t.Fatalf("started %v", started)
	}
	setNow(t, start.Add(25*time.Hour))
	forEvent(t, claude(Start, "new"))
	if len(started) != 2 {
		t.Fatalf("started %v", started)
	}
	// Written then: the file system has the real time.
	if err := os.Chtimes(recordPath("claude-code", "new"), now(), now()); err != nil {
		t.Fatal(err)
	}

	// A record read after a day is marked as used.
	setNow(t, start.Add(20*24*time.Hour))
	if a := forEvent(t, claude(Subagent, "used")); !a.Kept {
		t.Fatalf("%+v", a)
	}
	leftover := filepath.Join(h, ".local/state/ctxtra/sessions/.ctxtra-123")
	testenv.Write(t, leftover, "")
	old31 := start.Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(leftover, old31, old31); err != nil {
		t.Fatal(err)
	}
	mark := filepath.Join(h, ".local/state/ctxtra/sessions/.cleaned")

	setNow(t, start.Add(31*24*time.Hour))
	if err := Clean(); err != nil {
		t.Fatal(err)
	}
	for id, kept := range map[string]bool{"old": false, "other": false, "used": true, "new": true} {
		if _, err := os.Stat(recordPath("claude-code", id)); (err == nil) != kept {
			t.Errorf("%s: kept %v, want %v", id, err == nil, kept)
		}
	}
	if _, err := os.Stat(leftover); err == nil {
		t.Error("leftover kept")
	}
	if _, err := os.Stat(mark); err != nil {
		t.Errorf("mark: %v", err)
	}
	// Nothing to clean.
	testenv.Home(t)
	if err := Clean(); err != nil {
		t.Error(err)
	}
}

// BenchmarkForEvent is what the hook adds for profiles: a session start
// that writes the record, and a subagent start that reads it.
func BenchmarkForEvent(b *testing.B) {
	testenv.Home(b)
	if err := Store([]string{"iam"}); err != nil {
		b.Fatal(err)
	}
	for _, e := range []Event{claude(Start, "s1"), claude(Subagent, "s1")} {
		b.Run([]string{"Start", "Continue", "Subagent"}[e.Kind], func(b *testing.B) {
			for b.Loop() {
				if _, err := ForEvent(e); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
