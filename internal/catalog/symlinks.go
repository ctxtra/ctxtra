package catalog

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
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
	links map[string]bool
	paths []string
}

func newSymlinkTree(links []string) *symlinkTree {
	if len(links) == 0 {
		return nil
	}
	slices.Sort(links)
	t := &symlinkTree{links: make(map[string]bool, len(links)), paths: links}
	for _, link := range links {
		t.links[link] = true
	}
	return t
}

func (t *tree) virtualDirectory(name string) (bool, error) {
	if !t.pattern || len(t.linkPaths) == 0 {
		return false, nil
	}
	if exists, ok := t.virtualDirs[name]; ok {
		return exists, nil
	}
	prefix := name + "/"
	if err := t.spend(1); err != nil {
		return false, err
	}
	start, _ := slices.BinarySearch(t.linkPaths, prefix)
	if start < len(t.linkPaths) && strings.HasPrefix(t.linkPaths[start], prefix) {
		if t.virtualDirs == nil {
			t.virtualDirs = map[string]bool{}
		}
		t.virtualDirs[name] = true
		return true, nil
	}
	return false, nil
}

// linkChildren charges each examined record before allocating its child.
// Only directories this pattern visits are retained, within its budget.
func (t *tree) linkChildren(name string) (map[string]fs.DirEntry, error) {
	if len(t.linkPaths) == 0 {
		return nil, nil
	}
	if children, ok := t.extraDirs[name]; ok {
		return children, nil
	}
	if err := t.spend(1); err != nil {
		return nil, err
	}
	children := map[string]fs.DirEntry{}
	prefix := ""
	if name != "." {
		prefix = name + "/"
	}
	start, _ := slices.BinarySearch(t.linkPaths, prefix)
	for _, link := range t.linkPaths[start:] {
		rest, ok := strings.CutPrefix(link, prefix)
		if !ok {
			break
		}
		if err := t.spend(1); err != nil {
			return nil, err
		}
		child, _, _ := strings.Cut(rest, "/")
		if _, ok := children[child]; !ok {
			children[child] = virtualDir(child)
			if t.virtualDirs == nil {
				t.virtualDirs = map[string]bool{}
			}
			t.virtualDirs[prefix+child] = true
		}
	}
	if t.extraDirs == nil {
		t.extraDirs = map[string]map[string]fs.DirEntry{}
	}
	t.extraDirs[name] = children
	return children, nil
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
