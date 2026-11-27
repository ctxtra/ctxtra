// Package caserule tells whether a folder's volume compares names with or
// without regard to case. It only reads: it never writes into the folder to
// find out.
package caserule

import (
	"errors"
	"io/fs"
	"path/filepath"
	"syscall"
)

// Rule is how a volume compares names.
type Rule int

const (
	// Unknown: the volume can't be asked, or its answer isn't established.
	Unknown Rule = iota
	// Sensitive: Clients and clients are different names.
	Sensitive
	// Insensitive: Clients and clients are the same name.
	Insensitive
)

func (r Rule) String() string {
	switch r {
	case Sensitive:
		return "case-sensitive"
	case Insensitive:
		return "case-insensitive"
	}
	return "unknown"
}

// Of is the rule of the volume that holds dir, an absolute path. A folder
// that doesn't exist is asked through its nearest existing parent.
func Of(dir string) Rule {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		r, err := of(d)
		if err == nil || !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return r
		}
		if filepath.Dir(d) == d {
			return Unknown
		}
	}
}
