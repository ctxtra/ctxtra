package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
)

// maxStored bounds the files this package writes and reads, the same for
// both: a set that doesn't fit is not written.
const maxStored = 64 << 10

// stored is the format of profiles.json and of a session's record.
type stored struct {
	Profiles []string `json:"profiles"`
	// Origin is where a session's set came from; profiles.json has none.
	Origin Origin `json:"origin,omitempty"`
}

// StoredPath is the stored selection: on this machine, outside the config,
// which may be shared across machines.
func StoredPath() string { return filepath.Join(paths.StateDir(), "profiles.json") }

// Stored is the stored selection, and whether there is one. A file that
// can't be read, or holds an invalid name, is an error.
func Stored() (names []string, ok bool, err error) {
	s, ok, err := readStored(StoredPath())
	return s.Profiles, ok, err
}

// Store replaces the stored selection with names, which must be valid;
// none clears it. It refuses to overwrite a file it can't read, and names
// that file in the error.
func Store(names []string) error {
	path := StoredPath()
	if _, _, err := readStored(path); err != nil {
		return err
	}
	s := stored{Profiles: []string{}}
	for _, n := range names {
		if err := config.CheckName(n); err != nil {
			return err
		}
		if !slices.Contains(s.Profiles, n) {
			s.Profiles = append(s.Profiles, n)
		}
	}
	return writeStored(path, s)
}

// readStored reads the file at path, and whether it exists. The error
// names the file.
func readStored(path string) (stored, bool, error) {
	var s stored
	b, err := fsutil.ReadBounded(path, maxStored)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, false, nil
	case err != nil:
		return s, false, fmt.Errorf("%s: %w", paths.Tilde(path), err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	err = dec.Decode(&s)
	if err == nil {
		if dec.Decode(&struct{}{}) != io.EOF {
			err = errors.New("more after the JSON value")
		}
	}
	if err != nil {
		return stored{}, false, fmt.Errorf("%s is not a valid profile selection: %w", paths.Tilde(path), err)
	}
	for _, n := range s.Profiles {
		if err := config.CheckName(n); err != nil {
			return stored{}, false, fmt.Errorf("%s is not a valid profile selection: %w", paths.Tilde(path), err)
		}
	}
	return s, true, nil
}

// writeStored writes s to path, readable by the user only, when it is at
// most maxStored bytes.
func writeStored(path string, s stored) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if len(b) > maxStored {
		return fmt.Errorf("%d profiles take %d bytes, more than the %d that are kept", len(s.Profiles), len(b), maxStored)
	}
	return fsutil.WriteFile(path, b, 0o600)
}
