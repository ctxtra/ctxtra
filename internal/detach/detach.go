// Package detach starts ctxtra in the background, for work that must not
// make a session wait: update checks and the cleanup of session records.
package detach

import (
	"os"
	"path/filepath"
)

// Executable is the path this ctxtra was started by, as the hook command
// names it (a symlink stays a symlink), else the running binary.
func Executable() string {
	if filepath.IsAbs(os.Args[0]) {
		return os.Args[0]
	}
	exe, _ := os.Executable()
	return exe
}
