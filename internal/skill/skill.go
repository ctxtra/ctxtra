// Package skill writes ctxtra's skills of this binary to
// ~/.local/share/ctxtra/skills/<name>, where a binding in the user's config adds them to
// sessions like any other skill.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ctxtra/ctxtra"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
)

// Base is the ctxtra skill, which lets agents set up and change ctxtra.
const Base = "ctxtra"

// Binding is the binding of the config that skill install adds ctxtra's
// skills to.
const Binding = "ctxtra-system"

// Comment is the comment above the binding that skill install adds.
const Comment = "# ctxtra's own skills (ctxtra skill install). Agents use them to work with ctxtra; keep this binding."

// Write writes every file of the skill name that differs from this
// binary's, and reports whether it changed any. The folder is ctxtra's:
// changed files get no backup. A link in place of the folder is refused, so
// the hook never writes into a folder somewhere else.
func Write(name string) (bool, error) { return writeTo(name, paths.SkillDir(name)) }

// newTemp is os.MkdirTemp; tests make the new folder fail.
var newTemp = os.MkdirTemp

// Install puts this binary's skill name in its folder as a whole, then calls
// save. When writing the files or save fails, the folder is as it was: the
// old one is put back, or the new one removed. changed reports whether the
// files differ from what was there.
func Install(name string, save func() error) (changed bool, err error) {
	dir := paths.SkillDir(name)
	fi, err := os.Lstat(dir)
	switch {
	case err == nil && !fi.IsDir():
		return false, fmt.Errorf("%s isn't a folder; not changed", paths.Tilde(dir))
	case err == nil && upToDate(name, dir):
		return false, save()
	}
	old := err == nil
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return false, err
	}
	tmp, err := newTemp(parent, "."+name+".new-")
	if err != nil {
		return false, err
	}
	if _, err := writeTo(name, tmp); err != nil {
		os.RemoveAll(tmp)
		return false, err
	}
	aside := tmp + ".old"
	if old {
		if err := os.Rename(dir, aside); err != nil {
			os.RemoveAll(tmp)
			return false, err
		}
	}
	undo := func() error {
		var errs []error
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, err)
		}
		if old {
			if err := os.Rename(aside, dir); err != nil {
				errs = append(errs, fmt.Errorf("could not put back %s, which is in %s: %w", paths.Tilde(dir), paths.Tilde(aside), err))
			}
		}
		return errors.Join(errs...)
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		return false, errors.Join(err, undo())
	}
	if err := save(); err != nil {
		return false, errors.Join(err, undo())
	}
	os.RemoveAll(aside)
	return true, nil
}

// upToDate reports whether every file of the skill name in dir is this
// binary's.
func upToDate(name, dir string) bool {
	src := ctxtra.Skill(name)
	same := src != nil
	_ = fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || !same || d.IsDir() {
			return err
		}
		want, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		got, err := fsutil.ReadBounded(filepath.Join(dir, filepath.FromSlash(p)), len(want))
		same = err == nil && bytes.Equal(got, want)
		return nil
	})
	return same
}

// Stage writes the skill name to a new temporary folder, for a check before
// it is installed. remove deletes the folder.
func Stage(name string) (dir string, remove func(), err error) {
	tmp, err := os.MkdirTemp("", "ctxtra-skill-")
	if err != nil {
		return "", nil, err
	}
	remove = func() { os.RemoveAll(tmp) }
	dir = filepath.Join(paths.Real(tmp), name)
	if _, err := writeTo(name, dir); err != nil {
		remove()
		return "", nil, err
	}
	return dir, remove, nil
}

func writeTo(name, dir string) (bool, error) {
	src := ctxtra.Skill(name)
	if src == nil {
		return false, fmt.Errorf("ctxtra has no skill %q", name)
	}
	if fi, err := os.Lstat(dir); err == nil && !fi.IsDir() {
		return false, fmt.Errorf("%s isn't a folder; not changed", paths.Tilde(dir))
	}
	changed := false
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, filepath.FromSlash(p))
		// A bounded read that refuses a FIFO, so the hook can't block; a
		// file it can't read is replaced.
		if got, err := fsutil.ReadBounded(dest, len(want)); err == nil && bytes.Equal(got, want) {
			return nil
		}
		changed = true
		return fsutil.WriteFile(dest, want, 0o644)
	})
	return changed, err
}

// Refresh brings each installed skill to this binary's version. It skips
// the skills that aren't installed. The hook runs it, so the skills follow
// the binary that the agents run.
func Refresh() error {
	var errs []error
	for _, name := range ctxtra.SkillNames() {
		if _, err := os.Stat(filepath.Join(paths.SkillDir(name), "SKILL.md")); err != nil {
			continue
		}
		if _, err := Write(name); err != nil {
			errs = append(errs, fmt.Errorf("skill %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
