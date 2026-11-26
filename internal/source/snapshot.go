package source

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

// Snapshot is the folder subdir of one commit, read from ctxtra's copy of
// the repository in the cache: what the update review reads. Reading it
// writes nothing, and it has what the store would have: regular files only,
// at the same paths.
type Snapshot struct {
	r     *Repo
	ctx   context.Context
	files map[string]Entry
	dirs  map[string][]fs.DirEntry
	// Skipped are the symlinks and submodules the store leaves out.
	Skipped []Skipped

	mu    sync.Mutex
	blobs map[string][]byte
}

// Snapshot reads the files of commit's folder subdir, after checking the
// whole commit against the limits, as Materialize does. ctx bounds every
// later read too.
func (r *Repo) Snapshot(ctx context.Context, commit, subdir string) (*Snapshot, error) {
	entries, skipped, err := r.Files(ctx, commit, subdir)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{r: r, ctx: ctx, files: map[string]Entry{}, dirs: map[string][]fs.DirEntry{".": nil},
		Skipped: skipped, blobs: map[string][]byte{}}
	for _, e := range entries {
		s.files[e.Path] = e
		child := fs.DirEntry(entryInfo{name: path.Base(e.Path), size: e.Size, mode: fileMode(e)})
		for p := e.Path; p != "."; {
			dir := path.Dir(p)
			_, known := s.dirs[dir]
			s.dirs[dir] = append(s.dirs[dir], child)
			if known {
				break
			}
			child, p = entryInfo{name: path.Base(dir), dir: true}, dir
		}
	}
	for _, list := range s.dirs {
		slices.SortFunc(list, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	}
	return s, nil
}

// Entries are the regular files of the snapshot, sorted by path.
func (s *Snapshot) Entries() []Entry {
	out := make([]Entry, 0, len(s.files))
	for _, e := range s.files {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return out
}

func fileMode(e Entry) fs.FileMode {
	if e.Exec {
		return 0o500
	}
	return 0o400
}

// Open opens a file or folder of the snapshot.
func (s *Snapshot) Open(name string) (fs.File, error) {
	fi, err := s.Stat(name)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return &dirFile{info: fi.(entryInfo), entries: s.dirs[name]}, nil
	}
	data, err := s.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return &file{info: fi.(entryInfo), data: data}, nil
}

// Stat describes a file or folder of the snapshot.
func (s *Snapshot) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if e, ok := s.files[name]; ok {
		return entryInfo{name: path.Base(name), size: e.Size, mode: fileMode(e)}, nil
	}
	if _, ok := s.dirs[name]; ok {
		return entryInfo{name: path.Base(name), dir: true}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// ReadDir lists a folder of the snapshot, sorted by name.
func (s *Snapshot) ReadDir(name string) ([]fs.DirEntry, error) {
	fi, err := s.Stat(name)
	switch {
	case err != nil:
		return nil, err
	case !fi.IsDir():
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	return slices.Clone(s.dirs[name]), nil
}

// ReadFile is the content of a file of the snapshot, read from git once.
func (s *Snapshot) ReadFile(name string) ([]byte, error) {
	e, ok := s.files[name]
	if !ok {
		if _, dir := s.dirs[name]; dir {
			return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
		}
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.blobs[e.OID]; ok {
		return bytes.Clone(b), nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, readTimeout)
	defer cancel()
	b, err := s.r.gitMax(ctx, true, nil, int(e.Size), "cat-file", "blob", e.OID)
	if err != nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	s.blobs[e.OID] = b
	return bytes.Clone(b), nil
}

type entryInfo struct {
	name string
	size int64
	mode fs.FileMode
	dir  bool
}

func (i entryInfo) Name() string { return i.name }
func (i entryInfo) Size() int64  { return i.size }
func (i entryInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o500
	}
	return i.mode
}
func (i entryInfo) ModTime() time.Time         { return time.Time{} }
func (i entryInfo) IsDir() bool                { return i.dir }
func (i entryInfo) Sys() any                   { return nil }
func (i entryInfo) Type() fs.FileMode          { return i.Mode().Type() }
func (i entryInfo) Info() (fs.FileInfo, error) { return i, nil }

type file struct {
	info entryInfo
	data []byte
	off  int
}

func (f *file) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *file) Close() error               { return nil }
func (f *file) Read(p []byte) (int, error) {
	if f.off >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.off:])
	f.off += n
	return n, nil
}

type dirFile struct {
	info    entryInfo
	entries []fs.DirEntry
	off     int
}

func (d *dirFile) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *dirFile) Close() error               { return nil }
func (d *dirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: fs.ErrInvalid}
}

func (d *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	left := d.entries[d.off:]
	if n > 0 && len(left) == 0 {
		return nil, io.EOF
	}
	if n > 0 && n < len(left) {
		left = left[:n]
	}
	d.off += len(left)
	return slices.Clone(left), nil
}
