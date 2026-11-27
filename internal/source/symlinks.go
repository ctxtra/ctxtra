package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/trust"
)

// Source paths cannot contain .git, so the generation's metadata cannot
// replace a file from the commit.
const symlinksFile = ".git/ctxtra-symlinks"

func writeSymlinks(dir string, skipped []Skipped) error {
	links := []string{}
	for _, entry := range skipped {
		if entry.Kind == "symlink" {
			links = append(links, entry.Path)
		}
	}
	data := strings.Join(links, "\n")
	if len(links) != 0 {
		data += "\n"
	}
	return fsutil.WriteFile(filepath.Join(dir, filepath.FromSlash(symlinksFile)), []byte(data), 0o400)
}

// StoredSymlinks reads the paths omitted from a generation, without
// consulting the cache or opening any link or link target.
func StoredSymlinks(dir string) ([]string, error) {
	file := filepath.Join(dir, filepath.FromSlash(symlinksFile))
	if _, err := trust.Stored(dir, file); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	b, err := fsutil.ReadBounded(file, MaxEntries*(maxPathBytes+1))
	if err != nil {
		return nil, err
	}
	data := strings.TrimSuffix(string(b), "\n")
	if data == "" {
		return nil, nil
	}
	links := strings.Split(data, "\n")
	if len(links) > MaxEntries {
		return nil, fmt.Errorf("more than %d stored symlink paths", MaxEntries)
	}
	for _, link := range links {
		if err := CheckPath(link); err != nil {
			return nil, err
		}
	}
	return links, nil
}

// SymlinkPaths are the paths a snapshot leaves out, relative to its top.
func (s *Snapshot) SymlinkPaths() ([]string, error) {
	var links []string
	for _, entry := range s.Skipped {
		if entry.Kind == "symlink" {
			links = append(links, entry.Path)
		}
	}
	return links, nil
}
