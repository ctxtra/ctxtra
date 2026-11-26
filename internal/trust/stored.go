package trust

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Stored returns the checked path under root without following a symlink.
// Files read from an accepted commit must not reach outside that commit.
func Stored(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s leaves its source", path)
	}
	cur := root
	parts := append([]string{""}, strings.Split(rel, string(filepath.Separator))...)
	for _, part := range parts {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return "", err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is a symlink in a repo source; skipped", cur)
		}
	}
	return cur, nil
}
