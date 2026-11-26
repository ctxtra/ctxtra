package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
)

// Trusted is a dir source's folder that the user trusted with ctxtra
// source trust: its files are read live while its repository has the
// remotes recorded here.
type Trusted struct {
	// Path is the folder, without symlinks.
	Path string `json:"path"`
	// Remotes are the remotes of its repository when it was trusted,
	// normalized and sorted.
	Remotes []string `json:"remotes"`
}

// TrustedPath is the file of the trusted folders in the state folder.
func TrustedPath() string { return filepath.Join(paths.StateDir(), "trusted-dirs.json") }

// LoadTrusted reads the trusted folders. A missing file is none; a broken
// one is an error, and then no folder is trusted.
func LoadTrusted() ([]Trusted, error) {
	b, err := fsutil.ReadBounded(TrustedPath(), maxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var ts []Trusted
	if err == nil {
		err = json.Unmarshal(b, &ts)
	}
	if err != nil {
		return nil, fmt.Errorf("%s can't be read (%v); fix or delete it, then run ctxtra source trust again", paths.Tilde(TrustedPath()), err)
	}
	return ts, nil
}

// TrustedRemotes are the remotes recorded for the folder path; ok is false
// when it isn't trusted.
func TrustedRemotes(ts []Trusted, path string) (remotes []string, ok bool) {
	for _, t := range ts {
		if t.Path == path {
			return t.Remotes, true
		}
	}
	return nil, false
}

// Trust records remotes as the trusted remotes of the folder path.
func Trust(path string, remotes []string) error {
	unlock, err := fsutil.Lock(filepath.Join(paths.StateDir(), "trusted-dirs.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	ts, err := LoadTrusted()
	if err != nil {
		ts = nil
	}
	ts = slices.DeleteFunc(ts, func(t Trusted) bool { return t.Path == path })
	ts = append(ts, Trusted{Path: path, Remotes: remotes})
	slices.SortFunc(ts, func(a, b Trusted) int {
		switch {
		case a.Path < b.Path:
			return -1
		case a.Path > b.Path:
			return 1
		}
		return 0
	})
	return writeJSON(TrustedPath(), ts)
}
