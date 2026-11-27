package catalog

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/trust"
)

// Checkout is the catalog of a config with one source, name, that has the
// files of the folder dir as an accepted commit of a repo source would
// have them: regular files only. A symlink, the files of a nested
// repository or submodule, and .git are left out, and an entry that names
// one gets an error that says so. Nothing outside dir is read.
func Checkout(name, dir string) *Catalog {
	src := &config.Source{Name: name, Repo: dir}
	c := &Catalog{
		conf:    &config.Config{Sources: []*config.Source{src}},
		base:    dir,
		sources: map[string]*Source{},
		renamed: map[string]string{},
		trust:   trust.New(nil),
		store:   &storeUse{marked: map[string]error{}},
	}
	s := &Source{Source: src, State: Active, Top: dir, fsys: checkoutFS{dir}}
	c.sources[name] = s
	c.order = []*Source{s}
	return c
}

var (
	errSymlink = errors.New("a symlink, which a repo source leaves out")
	errNested  = errors.New("in a nested repository or submodule, which a repo source leaves out")
	errGit     = errors.New(".git is not a path of a source")
)

// checkoutFS is a folder as a commit would have it.
type checkoutFS struct{ root string }

// check says why name is not in the commit, nil when it may be.
func (f checkoutFS) check(name string) error {
	if name == "." {
		return nil
	}
	var parts []string
	for p := name; p != "."; p = path.Dir(p) {
		parts = append(parts, p)
	}
	slices.Reverse(parts)
	for _, p := range parts {
		if path.Base(p) == ".git" {
			return errGit
		}
		full := filepath.Join(f.root, filepath.FromSlash(p))
		fi, err := os.Lstat(full)
		switch {
		case err != nil:
			return err
		case fi.Mode()&fs.ModeSymlink != 0:
			return errSymlink
		case fi.IsDir() && exists(filepath.Join(full, ".git")):
			return errNested
		}
	}
	return nil
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func (f checkoutFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if err := f.check(name); err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	file, err := os.Open(filepath.Join(f.root, filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	return &checkoutFile{File: file, fs: f, name: name}, nil
}

func (f checkoutFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if err := f.check(name); err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	return os.Lstat(filepath.Join(f.root, filepath.FromSlash(name)))
}

// checkoutFile is a file or folder of a checkoutFS: a folder lists only
// what the commit would have.
type checkoutFile struct {
	*os.File
	fs   checkoutFS
	name string
}

func (f *checkoutFile) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.File.ReadDir(n)
	return slices.DeleteFunc(entries, func(e fs.DirEntry) bool {
		err := f.fs.check(path.Join(f.name, e.Name()))
		return err != nil && !errors.Is(err, errSymlink)
	}), err
}
