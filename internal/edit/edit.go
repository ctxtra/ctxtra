// Package edit writes the config and the lock: under one operating-system
// lock, from a fresh read of both, changing only the caller's entries, each
// file through a new file renamed over the old one.
package edit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/paths"
)

// maxFileBytes is the largest config or lock file ctxtra reads.
const maxFileBytes = 4 << 20

// writeFile replaces a file; tests replace it to see the order of writes.
var writeFile = fsutil.WriteAtomic

// ErrLockChanged is a lock entry that changed while its review ran: the
// review must start again from the new entry.
var ErrLockChanged = errors.New("the lock changed during the review; run the command again")

// Files are the files a write reads and changes.
type Files struct {
	// Config is the config file; its lock file is next to its real file.
	Config string
	// Mutex is the file that the operating-system lock is taken on.
	Mutex string
	// Backups is the folder of old versions of both files.
	Backups string
	// Options are how the config is read to check a write.
	Options config.Options
}

// Default are the user's files: config.yaml in the config folder, and
// config.lock in the state folder.
func Default(opts config.Options) Files {
	return Files{
		Config:  paths.ConfigPath(),
		Mutex:   filepath.Join(paths.StateDir(), "config.lock"),
		Backups: paths.BackupDir(),
		Options: opts,
	}
}

// Tx is one write: the config and the lock as read under the lock, and the
// caller's changes to them.
type Tx struct {
	// Config is the config, with the changes the caller makes to it.
	Config *config.Editor
	before *config.Config
	lock   *lock.Lock
	// lockFile is false when the lock file doesn't exist.
	lockFile bool
	set      []*lock.Entry
	// The files as read, nil when missing, and which ones the write
	// replaced: a failed commit puts them back.
	oldConfig, oldLock     []byte
	wroteConfig, wroteLock bool
}

// LockEntry is the usable lock entry of source name as read, nil when
// there is none.
func (tx *Tx) LockEntry(name string) *lock.Entry { return tx.lock.Entry(name) }

// SetLockEntry writes e as the lock entry of its source, whatever the entry
// was.
func (tx *Tx) SetLockEntry(e *lock.Entry) {
	tx.set = slices.DeleteFunc(tx.set, func(x *lock.Entry) bool { return x.Name == e.Name })
	tx.set = append(tx.set, e)
}

// UpdateLockEntry writes e as the lock entry of its source if the entry is
// still was, as read when the review of e started (nil for none). Otherwise
// it writes nothing and returns ErrLockChanged.
func (tx *Tx) UpdateLockEntry(e, was *lock.Entry) error {
	if !sameEntry(tx.lock.Entry(e.Name), was) {
		return fmt.Errorf("source %s: %w", e.Name, ErrLockChanged)
	}
	tx.SetLockEntry(e)
	return nil
}

// Run takes the lock, reads the config and the lock file, and calls fn.
// When fn returns nil, Run writes what fn changed: the lock file first,
// then the config, each read back with the strict YAML layer before it
// replaces the old file. When fn or a check fails, nothing is written.
func (f Files) Run(fn func(tx *Tx) error) error {
	return f.RunCommit(fn, func(write func() error) error { return write() })
}

// RunCommit is Run that hands the write to commit, still under the lock:
// commit calls write when the other things it saves can go with the
// config, and nothing is written when it doesn't.
func (f Files) RunCommit(fn func(tx *Tx) error, commit func(write func() error) error) error {
	unlock, err := fsutil.Lock(f.Mutex)
	if err != nil {
		return err
	}
	defer unlock()
	tx, err := f.read()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	wrote := false
	err = commit(func() error {
		err := f.write(tx)
		wrote = err == nil
		return err
	})
	if err == nil || !wrote {
		return err
	}
	if rerr := f.restore(tx); rerr != nil {
		return errors.Join(err, rerr)
	}
	return err
}

// restore puts back the files that a write replaced.
func (f Files) restore(tx *Tx) error {
	var errs []error
	put := func(path string, old []byte) {
		if old == nil {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("could not restore %s: %w", paths.Tilde(path), err))
			}
			return
		}
		if _, _, err := writeFile(path, old, f.Backups, false); err != nil {
			errs = append(errs, fmt.Errorf("could not restore %s: %w", paths.Tilde(path), err))
		}
	}
	if tx.wroteConfig {
		put(f.Config, tx.oldConfig)
	}
	if tx.wroteLock {
		put(lock.Path(f.Config), tx.oldLock)
	}
	return errors.Join(errs...)
}

// Locked runs fn under the lock that every write takes: what fn reads from
// the config doesn't change while it runs.
func (f Files) Locked(fn func() error) error {
	unlock, err := fsutil.Lock(f.Mutex)
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}

// Created reports whether the config file is missing: a write creates it.
func (f Files) Created() bool {
	_, err := os.Stat(f.Config)
	return errors.Is(err, os.ErrNotExist)
}

func (f Files) read() (*Tx, error) {
	data, err := readFile(f.Config)
	if err != nil {
		return nil, err
	}
	ed, err := config.NewEditor(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", paths.Tilde(f.Config), err)
	}
	tx := &Tx{Config: ed, before: config.Parse(ed.Bytes(), f.Options), lock: &lock.Lock{}, oldConfig: data}
	data, err = readFile(lock.Path(f.Config))
	tx.oldLock = data
	switch {
	case err != nil:
		return nil, err
	case data != nil:
		tx.lock, tx.lockFile = lock.Parse(data), true
	}
	return tx, nil
}

// readFile is the content of path, nil when there is no such file.
func readFile(path string) ([]byte, error) {
	data, err := fsutil.ReadBounded(path, maxFileBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func (f Files) write(tx *Tx) error {
	c := config.Parse(tx.Config.Bytes(), f.Options)
	if tx.Config.Changed() {
		if err := tx.Config.Check(f.Options); err != nil {
			return fmt.Errorf("the change would add a mistake to the config; nothing changed:\n%w", err)
		}
	}
	lockData, err := f.lockData(tx, c)
	if err != nil {
		return fmt.Errorf("%s: %w; nothing changed", paths.Tilde(lock.Path(f.Config)), err)
	}
	if lockData != nil {
		if _, _, err := writeFile(lock.Path(f.Config), lockData, f.Backups, false); err != nil {
			return err
		}
		tx.wroteLock = true
	}
	if tx.Config.Changed() {
		if _, _, err := writeFile(f.Config, tx.Config.Bytes(), f.Backups, false); err != nil {
			return err
		}
		tx.wroteConfig = true
	}
	return nil
}

// lockData is the new lock file, nil when it stays as it is. It is written
// when the caller changed an entry, and when the caller removed a repo
// source that has an entry. Entries of sources that the config doesn't have
// as repo sources are left out.
func (f Files) lockData(tx *Tx, c *config.Config) ([]byte, error) {
	removed := slices.ContainsFunc(tx.lock.Entries, func(e *lock.Entry) bool {
		return isRepo(tx.before.Source(e.Name)) && !isRepo(c.Source(e.Name))
	})
	if len(tx.set) == 0 && !removed {
		return nil, nil
	}
	if !tx.lock.Readable() {
		if len(tx.set) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("the lock file can't be read (%v); fix it or remove it", tx.lock.Errors[0])
	}
	for _, e := range tx.set {
		if !isRepo(c.Source(e.Name)) {
			return nil, fmt.Errorf("the config has no repo source %s", e.Name)
		}
	}
	entries := slices.DeleteFunc(slices.Clone(tx.lock.Entries), func(e *lock.Entry) bool {
		return slices.ContainsFunc(tx.set, func(x *lock.Entry) bool { return x.Name == e.Name })
	})
	data, err := lock.Format(append(entries, tx.set...), c)
	if err != nil {
		if len(tx.set) == 0 {
			return nil, nil
		}
		return nil, err
	}
	back := lock.Parse(data)
	if len(back.Errors) > 0 {
		return nil, fmt.Errorf("the new lock file doesn't read back: %w", back.Errors[0])
	}
	for _, e := range tx.set {
		if !sameEntry(back.Entry(e.Name), e) {
			return nil, fmt.Errorf("the lock entry of %s doesn't read back as written", e.Name)
		}
	}
	return data, nil
}

func isRepo(s *config.Source) bool { return s != nil && s.Dir == "" }

// sameEntry reports whether a and b accept the same commit for the same
// source, address, ref and folder, as written.
func sameEntry(a, b *lock.Entry) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name && a.Repo == b.Repo && a.Ref == b.Ref && a.Subdir == b.Subdir && a.Commit == b.Commit
}

// Add adds entry name to section s of the config; see config.Editor.Add.
// added is false when the same entry is already there.
func (f Files) Add(s config.Section, name string, entry *yaml.Node, at config.Place) (added bool, err error) {
	err = f.Run(func(tx *Tx) error {
		added, err = tx.Config.Add(s, name, entry, at)
		return err
	})
	return added, err
}

// Replace changes the entry name of section s of the config in its place.
func (f Files) Replace(s config.Section, name string, entry *yaml.Node) error {
	return f.Run(func(tx *Tx) error { return tx.Config.Replace(s, name, entry) })
}

// Remove removes the entry name of section s of the config. A removed
// source's lock entry goes with it.
func (f Files) Remove(s config.Section, name string) error {
	return f.Run(func(tx *Tx) error { return tx.Config.Remove(s, name) })
}

// AddSource adds source name to the config, and accepted, its first
// accepted commit, to the lock; a dir source has none (nil). The lock is
// written first: a crash in between leaves an unused lock entry, which the
// next write removes or replaces. added is false when the same source is
// already in the config; then the lock stays as it is.
func (f Files) AddSource(name string, entry *yaml.Node, accepted *lock.Entry) (added bool, err error) {
	err = f.Run(func(tx *Tx) error {
		added, err = tx.Config.Add(config.Sources, name, entry, config.Place{})
		if added && accepted != nil {
			tx.SetLockEntry(accepted)
		}
		return err
	})
	return added, err
}

// Accept writes accepted, a commit that a review of a source accepted, if
// the source's lock entry is still was, as read when the review started.
// Otherwise it returns ErrLockChanged.
func (f Files) Accept(accepted, was *lock.Entry) error {
	return f.Run(func(tx *Tx) error { return tx.UpdateLockEntry(accepted, was) })
}
