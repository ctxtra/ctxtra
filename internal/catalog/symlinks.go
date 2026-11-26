package catalog

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/ctxtra/ctxtra/internal/source"
)

func (s *Source) symlinkPaths() ([]string, error) {
	if s.Live() {
		return nil, nil
	}
	if !s.symlinksRead {
		s.symlinksRead = true
		switch f := s.fsys.(type) {
		case interface{ SymlinkPaths() ([]string, error) }:
			s.symlinks, s.symlinksErr = f.SymlinkPaths()
		case nil:
			s.symlinks, s.symlinksErr = source.StoredSymlinks(s.Top)
		}
		s.linkTree = newSymlinkTree(s.symlinks)
	}
	return s.symlinks, s.symlinksErr
}

func (s *Source) repoPathError(rel string) error {
	if s.Live() {
		return nil
	}
	if _, err := s.symlinkPaths(); err != nil {
		return err
	}
	for p := rel; p != "."; p = path.Dir(p) {
		switch {
		case strings.EqualFold(path.Base(p), ".git"):
			return errGit
		case s.linkTree != nil && s.linkTree.links[p]:
			return fmt.Errorf("%s:%s is %w", s.Name, p, errSymlink)
		}
	}
	return nil
}

type symlinkTree struct {
	links     map[string]bool
	extraDirs map[string]map[string]fs.DirEntry
}

func newSymlinkTree(links []string) *symlinkTree {
	if len(links) == 0 {
		return nil
	}
	t := &symlinkTree{links: map[string]bool{}, extraDirs: map[string]map[string]fs.DirEntry{}}
	for _, link := range links {
		t.links[link] = true
		for p := link; p != "."; p = path.Dir(p) {
			dir := path.Dir(p)
			if t.extraDirs[dir] == nil {
				t.extraDirs[dir] = map[string]fs.DirEntry{}
			}
			t.extraDirs[dir][path.Base(p)] = virtualDir(path.Base(p))
		}
	}
	return t
}

func (t *tree) repoPathError(rel string) error {
	if t.live {
		return nil
	}
	for p := rel; p != "."; p = path.Dir(p) {
		switch {
		case strings.EqualFold(path.Base(p), ".git"):
			return errGit
		case t.links[p]:
			return errSymlink
		}
	}
	return nil
}

// Recorded links look like directories to glob traversal so that a
// pattern below a link reports it, without opening the link or its target.
type virtualDir string

func (d virtualDir) Name() string               { return string(d) }
func (virtualDir) Size() int64                  { return 0 }
func (virtualDir) Mode() fs.FileMode            { return fs.ModeDir | 0o500 }
func (virtualDir) ModTime() time.Time           { return time.Time{} }
func (virtualDir) IsDir() bool                  { return true }
func (virtualDir) Sys() any                     { return nil }
func (virtualDir) Type() fs.FileMode            { return fs.ModeDir }
func (d virtualDir) Info() (fs.FileInfo, error) { return d, nil }
