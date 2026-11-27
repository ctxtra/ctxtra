package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ctxtra/ctxtra/internal/detach"
	"github.com/ctxtra/ctxtra/internal/paths"
)

// Kind is how a hook event relates to its session.
type Kind int

const (
	// Start is a new session or /clear: the current set, which replaces
	// the session's record.
	Start Kind = iota
	// Continue is compaction, resume, or an event that doesn't say: the
	// session's record, else its parent's when one is known.
	Continue
	// Subagent is a subagent's start: its parent session's record.
	Subagent
)

// Event is a hook event, as far as profiles go. IDs are the agent's own,
// opaque values: ctxtra never guesses them.
type Event struct {
	// Agent is the agent, by the name ctxtra setup uses.
	Agent string
	Kind  Kind
	// Session is the session the event belongs to: a subagent's own ID
	// where the agent gives it one. "" when the agent doesn't say.
	Session string
	// Parent is a subagent's parent session, "" when the agent doesn't
	// say. A Continue with no record can inherit it too: some context
	// callbacks don't distinguish a child's start from later events.
	Parent string
}

const (
	// keepRecords is how long a record that no event used is kept.
	keepRecords = 30 * 24 * time.Hour
	// touchAfter: a record read later than this after its last write or
	// read is marked as used again, so a long session keeps it.
	touchAfter = 24 * time.Hour
	// cleanEvery is how often sessions start the cleanup.
	cleanEvery = 24 * time.Hour
)

// Variables for tests.
var (
	now   = time.Now
	spawn = func(args []string) error { return detach.Start(detach.Executable(), args) }
	// beforeRemove runs between the cleanup's two checks of a record.
	beforeRemove = func(string) {}
)

// CleanArgs are the arguments of the ctxtra command that deletes records no
// event used for 30 days. Sessions start it in the background.
var CleanArgs = []string{"hook", "--clean-sessions"}

// recordsDir holds one record per session, readable by the user only.
func recordsDir() string { return filepath.Join(paths.StateDir(), "sessions") }

// recordPath is the record of agent's session id.
func recordPath(agent, id string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%s\n%s", len(agent), agent, id)))
	return filepath.Join(recordsDir(), hex.EncodeToString(h[:16])+".json")
}

// ForEvent is the set that hook event e gets, and keeps it for e's
// session:
//
//   - Start: the current set, which replaces the record.
//   - Subagent: the parent's record; also kept for the subagent when it
//     has an ID of its own.
//   - Continue: the session's record, else a known parent's record.
//   - No session ID, or no record: the current set, recorded when there is
//     a session ID.
//
// err is why a record couldn't be read or written, for the hook log: the
// event then gets the current set, and the session still starts.
func ForEvent(e Event) (Active, error) {
	var errs []error
	var lookups []string
	switch e.Kind {
	case Subagent:
		lookups = append(lookups, e.Parent)
	case Continue:
		lookups = append(lookups, e.Session)
		if e.Parent != e.Session {
			lookups = append(lookups, e.Parent)
		}
	}
	for _, lookup := range lookups {
		if lookup == "" {
			continue
		}
		a, ok, err := read(e.Agent, lookup)
		errs = append(errs, err)
		if ok {
			if e.Session == "" || e.Session == lookup {
				return a, err
			}
			werr := write(e.Agent, e.Session, a)
			if werr == nil {
				return a, err
			}
			// Not recorded: the event gets what a new session would.
			return Current(), errors.Join(err, werr)
		}
		if err != nil {
			break
		}
	}
	a := Current()
	if e.Session != "" {
		errs = append(errs, write(e.Agent, e.Session, a))
	}
	return a, errors.Join(errs...)
}

// read is the record of agent's session id, and whether there is one.
func read(agent, id string) (Active, bool, error) {
	path := recordPath(agent, id)
	s, ok, err := readStored(path)
	if !ok {
		if err != nil {
			err = fmt.Errorf("can't read the session's profiles: %w", err)
		}
		return Active{}, false, err
	}
	// Marked as used, so the cleanup keeps it. Not on every read: a
	// subagent start then costs no write.
	if st, err := os.Stat(path); err == nil && now().Sub(st.ModTime()) > touchAfter {
		_ = os.Chtimes(path, now(), now())
	}
	return Active{Names: s.Profiles, Origin: s.Origin, Kept: true}, true, nil
}

// write replaces the record of agent's session id with a, and starts the
// cleanup when it is due. A record that can't be written is removed, so
// that later events don't take an older set from it.
func write(agent, id string, a Active) error {
	names := a.Names
	if names == nil {
		names = []string{}
	}
	path := recordPath(agent, id)
	if err := writeStored(path, stored{Profiles: names, Origin: a.Origin}); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("can't keep the session's profiles: %w", err)
	}
	startClean()
	return nil
}

// startClean starts the cleanup in the background at most once per
// cleanEvery. The mark is set first: when the cleanup can't start, the
// next attempt waits as long.
func startClean() {
	mark := filepath.Join(recordsDir(), ".cleaned")
	if st, err := os.Stat(mark); err == nil && now().Sub(st.ModTime()) < cleanEvery {
		return
	}
	f, err := os.OpenFile(mark, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	f.Close()
	if os.Chtimes(mark, now(), now()) == nil {
		_ = spawn(CleanArgs)
	}
}

// Clean deletes the records that no event used for 30 days, and files
// that a write left behind.
func Clean() error {
	entries, err := os.ReadDir(recordsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, ".ctxtra-") {
			continue
		}
		path := filepath.Join(recordsDir(), name)
		if !unused(path) {
			continue
		}
		beforeRemove(path)
		// Again right before the removal: a hook may have used or replaced
		// the record since. Without a lock, a use in between these two
		// steps can still be lost; on a record unused for 30 days, that is
		// a rare cost.
		if !unused(path) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// unused reports whether the file at path exists and no event used it for
// keepRecords.
func unused(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && now().Sub(st.ModTime()) > keepRecords
}
