// Package fsutil reads and writes files safely: only regular files, with size
// limits, never blocking on a FIFO, and atomic writes with backups.
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrTotalLimit is returned by Reader.Read when the total limit is used up.
var ErrTotalLimit = errors.New("over the total read limit")

// OpenRegular opens name with flag and refuses anything but a regular file.
// Opening a FIFO doesn't block.
func OpenRegular(name string, flag int, perm os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(name, flag|openNonblock, perm)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a regular file", name)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// ReadBounded reads a regular file of at most limit bytes.
func ReadBounded(name string, limit int) ([]byte, error) {
	b, _, err := readBounded(name, limit)
	return b, err
}

// readBounded also returns how many bytes it read, also when the file is
// over the limit.
func readBounded(name string, limit int) ([]byte, int, error) {
	f, err := OpenRegular(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, len(b), err
	}
	if len(b) > limit {
		return nil, len(b), fmt.Errorf("%s is larger than %d bytes", name, limit)
	}
	return b, len(b), nil
}

// Reader reads files with a limit per file and a limit for all reads
// together, so one session can't exhaust memory or its time. Each file is
// read once, by its real path; later reads return the same result.
type Reader struct {
	perFile, total, used int
	cache                map[string]cached
	// counted are the files that Count counted: read elsewhere, and
	// charged once whether they are read again or not.
	counted map[string]counted
}

type cached struct {
	b   []byte
	err error
}

type counted struct {
	n   int
	err error
}

// NewReader returns a Reader with the given limits in bytes.
func NewReader(perFile, total int) *Reader {
	return &Reader{perFile: perFile, total: total, cache: map[string]cached{}, counted: map[string]counted{}}
}

// key is the identity of a file: its real path when it has one.
func key(name string) string {
	if real, err := filepath.EvalSymlinks(name); err == nil {
		return real
	}
	return filepath.Clean(name)
}

// Count counts n bytes of name, read elsewhere, against the total limit,
// once for each file. It fails when they don't fit in what is left. A
// later Read of name reads it without counting it again.
func (r *Reader) Count(name string, n int) error {
	k := key(name)
	if c, ok := r.cache[k]; ok {
		return c.err
	}
	if c, ok := r.counted[k]; ok {
		return c.err
	}
	var err error
	if r.used+n > r.total {
		err = fmt.Errorf("%s: %w of %d bytes", name, ErrTotalLimit, r.total)
	}
	r.used += n
	r.counted[k] = counted{n, err}
	return err
}

// Read returns the content of name. Bytes read count against the total limit
// also when the file is then refused for its size.
func (r *Reader) Read(name string) ([]byte, error) { return r.ReadMax(name, r.perFile) }

// ReadMax is Read with a smaller limit for this file.
func (r *Reader) ReadMax(name string, perFile int) ([]byte, error) {
	k := key(name)
	if c, ok := r.cache[k]; ok {
		return c.b, c.err
	}
	perFile = min(perFile, r.perFile)
	credit := r.counted[k].n
	left := r.total - r.used + credit
	if left <= 0 {
		return nil, fmt.Errorf("%s: %w of %d bytes", name, ErrTotalLimit, r.total)
	}
	limit := min(perFile, left)
	b, n, err := readBounded(name, limit)
	r.used += n - credit
	delete(r.counted, k)
	if err != nil && n > limit && limit < perFile {
		err = fmt.Errorf("%s: %w of %d bytes", name, ErrTotalLimit, r.total)
	}
	r.cache[k] = cached{b, err}
	return b, err
}
