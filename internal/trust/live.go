package trust

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/state"
)

// Live checks live folders against this machine's recorded trust. One value
// shares the trust-file read across all sources in a catalog.
type Live struct {
	trusted []state.Trusted
	err     error
	read    bool
}

// Check says why a source cannot be read live, or "" when its repository
// has no remotes or has the recorded ones. top is the real source folder;
// dir is its spelling in the config.
func (l *Live) Check(name, dir, top string) string {
	repo, remotes, err := Remotes(top)
	switch {
	case err != nil:
		return fmt.Sprintf("the remotes of %s can't be read (%v); it is not read until they can be", paths.Tilde(repo), err)
	case len(remotes) == 0:
		return ""
	}
	if !l.read {
		l.read = true
		l.trusted, l.err = state.LoadTrusted()
	}
	if l.err != nil {
		return l.err.Error()
	}
	was, ok := state.TrustedRemotes(l.trusted, top)
	switch {
	case !ok:
		return fmt.Sprintf("%s is in a repository with the remotes %s, which others can push to; to read it live, run `ctxtra source trust %s`; to read reviewed commits, use a repo source: `ctxtra source add --repo %s`",
			dir, strings.Join(remotes, ", "), name, paths.Tilde(repo))
	case !slices.Equal(was, remotes):
		return fmt.Sprintf("the remotes of %s changed since you trusted it (now %s); it is not read until you run `ctxtra source trust %s` again",
			dir, strings.Join(remotes, ", "), name)
	}
	return ""
}

// Route checks each symlink on the way to dir at its own location, using
// only sources already trusted without this route. top is its destination.
func Route(dir, top string, others *Policy) string {
	links, err := Links(filepath.Clean(paths.Expand(dir)))
	if err != nil {
		return fmt.Sprintf("dir %s: %v; it is not read", dir, err)
	}
	for _, link := range links {
		err := others.CheckAt(link)
		if err == nil {
			continue
		}
		var denied *Error
		if !errors.As(err, &denied) {
			return fmt.Sprintf("%s leads through a symlink at %s: %v; it is not read", dir, paths.Tilde(link), err)
		}
		repo, remotes, err := Remotes(denied.Repo)
		if err != nil {
			return fmt.Sprintf("%s leads through a symlink at %s, in the git repository %s, which ctxtra can't check (%v); it is not read",
				dir, paths.Tilde(link), paths.Tilde(repo), err)
		}
		return fmt.Sprintf("%s leads through a symlink at %s, in %s, which has the remotes %s and no trusted dir source there; it is not read. To read the folder the link leads to, use dir: %s",
			dir, paths.Tilde(link), paths.Tilde(repo), strings.Join(remotes, ", "), paths.Tilde(top))
	}
	return ""
}
