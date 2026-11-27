// Package state keeps what ctxtra learns on this machine.
//
// state.json holds what ctxtra learned about new commits of each pinned
// source and about new ctxtra releases, and the commit the user didn't
// accept. None of it changes what a session gets: losing it loses the
// notices.
//
// mappings.json holds the folders of imported clones (see Mapping). It
// changes what a session gets, so a broken file is reported, never rewritten.
package state

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
)

const maxBytes = 1 << 20

// Source is the state of one pinned source, by its store ID and what it
// follows: its branch or tag, and its subdir. URL and Follows tell whether
// the entry is still about the source in the config.
type Source struct {
	URL       string    `json:"url"`
	Follows   string    `json:"follows"`
	LastCheck time.Time `json:"lastCheck,omitzero"`  // the last fetch of the branch or tag that worked
	Dismissed string    `json:"dismissed,omitempty"` // the last commit the user didn't accept
	Check
	Head string `json:"head,omitempty"` // the branch's or tag's commit at Checked
	// Unchanged is a commit whose files under the source's subdir are
	// those of Head: commits that change only other folders are not new.
	Unchanged string `json:"unchanged,omitempty"`
}

// New reports whether Head is a commit the user hasn't seen: not accepted,
// not dismissed, and with other files than the accepted commit.
func (e Source) New(accepted string) bool {
	return e.Head != "" && e.Head != accepted && e.Head != e.Dismissed && (accepted == "" || e.Unchanged != accepted)
}

// Check is the result of the update checks of one thing: a source or ctxtra
// itself.
type Check struct {
	Attempt time.Time `json:"checkAttempt,omitzero"` // the last time a check was started
	Checked time.Time `json:"checked,omitzero"`      // the last check that worked
	Error   string    `json:"checkError,omitempty"`  // why the last check failed; "" after one that worked
}

// Release is what the checks learned about ctxtra's releases.
type Release struct {
	Check
	Latest string `json:"latest,omitempty"` // the latest release's version at Checked
}

// State is the whole file.
type State struct {
	Sources map[string]Source `json:"sources"`
	Ctxtra  Release           `json:"ctxtra,omitzero"`
}

// Path is ~/.local/state/ctxtra/state.json.
func Path() string { return filepath.Join(paths.StateDir(), "state.json") }

// Load reads the state. A missing or broken file is an empty state.
func Load() State {
	var s State
	if b, err := fsutil.ReadBounded(Path(), maxBytes); err == nil {
		if json.Unmarshal(b, &s) != nil {
			s = State{} // not partly decoded
		}
	}
	if s.Sources == nil {
		s.Sources = map[string]Source{}
	}
	return s
}

// key is per store ID and what the source follows: two sources can follow
// two branches of one repository, or two folders of one branch.
func key(id, follows string) string { return id + "@" + follows }

// Get is the state of the source id that follows follows, if it is about
// url.
func (s State) Get(id, url, follows string) Source {
	e := s.Sources[key(id, follows)]
	if e.URL != url || e.Follows != follows {
		return Source{URL: url, Follows: follows}
	}
	return e
}

// Update changes the entry of the source id under a lock and writes the
// file atomically.
func Update(id, url, follows string, change func(*Source)) error {
	return UpdateWithin(0, id, url, follows, change)
}

// UpdateRelease changes what the state knows about ctxtra's releases.
func UpdateRelease(change func(*Release)) error {
	return UpdateReleaseWithin(0, change)
}

// UpdateReleaseWithin is UpdateRelease that waits at most wait for the lock,
// or without a limit when wait is 0.
func UpdateReleaseWithin(wait time.Duration, change func(*Release)) error {
	return updateWithin(wait, func(s *State) error {
		change(&s.Ctxtra)
		return nil
	})
}

// ChangeWithin changes the whole state in one write, and waits at most wait
// for the lock.
func ChangeWithin(wait time.Duration, change func(*State)) error {
	return updateWithin(wait, func(s *State) error {
		change(s)
		return nil
	})
}

// Put sets the entry of the source id that follows e.Follows.
func (s State) Put(id string, e Source) { s.Sources[key(id, e.Follows)] = e }

// UpdateWithin is Update that waits at most wait for the lock, or without a
// limit when wait is 0.
func UpdateWithin(wait time.Duration, id, url, follows string, change func(*Source)) error {
	return updateWithin(wait, func(s *State) error {
		e := s.Get(id, url, follows)
		change(&e)
		s.Sources[key(id, follows)] = e
		return nil
	})
}

// Claim is a source to claim a check of, by the arguments of Get. Name is
// the caller's, returned with the claims.
type Claim struct{ Name, ID, URL, Follows string }

// ClaimChecks records a check attempt for ctxtra (when release) and for each
// source whose last attempt is at least interval old, and returns what it
// claimed, and the time it recorded. Two sessions that start at once don't both claim a check: the lock
// orders them, and the time is read under it, so the second one finds the
// first one's attempt. An attempt more than interval in the future, after the
// clock went back, is due too. It waits at most wait for the lock.
func ClaimChecks(release bool, srcs []Claim, now func() time.Time, interval, wait time.Duration) (bool, []Claim, time.Time, error) {
	unlock, err := fsutil.LockTimeout(lockPath(), wait)
	if err != nil {
		return false, nil, time.Time{}, err
	}
	defer unlock()
	s := Load()
	at := now()
	due := func(c Check) bool {
		d := at.Sub(c.Attempt)
		return d >= interval || d < -interval
	}
	claimedRelease := release && due(s.Ctxtra.Check)
	if claimedRelease {
		s.Ctxtra.Attempt = at
	}
	var claimed []Claim
	for _, c := range srcs {
		e := s.Get(c.ID, c.URL, c.Follows)
		if due(e.Check) {
			e.Attempt = at
			s.Sources[key(c.ID, c.Follows)] = e
			claimed = append(claimed, c)
		}
	}
	if !claimedRelease && len(claimed) == 0 {
		return false, nil, at, nil
	}
	return claimedRelease, claimed, at, writeJSON(Path(), s)
}

func lockPath() string { return filepath.Join(paths.StateDir(), "state.lock") }

func updateWithin(wait time.Duration, change func(*State) error) error {
	lock := fsutil.Lock
	if wait > 0 {
		lock = func(path string) (func(), error) { return fsutil.LockTimeout(path, wait) }
	}
	unlock, err := lock(lockPath())
	if err != nil {
		return err
	}
	defer unlock()
	s := Load()
	if err := change(&s); err != nil {
		return err
	}
	return writeJSON(Path(), s)
}

// writeJSON writes v to path atomically, if it fits the size the readers
// accept.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if len(b) >= maxBytes {
		return fmt.Errorf("%s would be larger than %d bytes", paths.Tilde(path), maxBytes)
	}
	return fsutil.WriteFile(path, append(b, '\n'), 0o600)
}
