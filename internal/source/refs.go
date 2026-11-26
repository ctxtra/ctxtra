package source

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
)

var (
	reTag    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/+-]*$`)
	reHexRev = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)
	// reRevSuffix is what may follow a revision's name: steps back in
	// history, and peeling to an object type.
	reRevSuffix = regexp.MustCompile(`^(?:[~^][0-9]*|\^\{[a-z]*\})*$`)
)

// CheckTag accepts ordinary tag names.
func CheckTag(t string) error {
	if !reTag.MatchString(t) || strings.Contains(t, "..") || strings.Contains(t, "//") ||
		strings.HasSuffix(t, "/") || strings.HasSuffix(t, ".") || strings.HasSuffix(t, ".lock") || strings.Contains(t, "/.") {
		return fmt.Errorf("%q is not a tag name ctxtra accepts", t)
	}
	return nil
}

// CheckSubdir accepts a folder of a repository, relative to its top, in the
// form that the config and the lock compare.
func CheckSubdir(subdir string) error {
	if subdir == "" {
		return nil
	}
	if err := CheckPath(subdir); err != nil {
		return fmt.Errorf("subdir %s is not a folder ctxtra can read: write a relative path inside the repository", Sanitize(subdir))
	}
	return nil
}

// Treeish names the folder subdir of commit for git: the commit itself for
// its whole tree.
func Treeish(commit, subdir string) string {
	if subdir == "" {
		return commit
	}
	return commit + ":" + subdir
}

// FetchTag fetches tag into a private ref and returns the commit it points
// to, through an annotated tag too.
func (r *Repo) FetchTag(ctx context.Context, tag string) (string, error) {
	if err := CheckTag(tag); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := r.ensure(ctx, r.URL); err != nil {
		return "", err
	}
	commit, err := r.fetchRef(ctx, r.URL, "refs/tags/"+tag)
	if err != nil {
		return "", fmt.Errorf("can't fetch the tag %s of %s: %w", tag, r.URL, err)
	}
	return commit, nil
}

// ResolveRev fetches rev, any revision the remote has, and returns its full
// commit ID: a full or short commit ID, a tag or a branch, each with the
// suffixes that walk history (main~1, v1.4^2, v1^{commit}). A full ID is
// fetched alone; anything else needs the remote's branches and tags, which
// are fetched into a private namespace that rev's name is looked up in.
func (r *Repo) ResolveRev(ctx context.Context, rev string) (string, error) {
	if ValidCommit(rev) {
		if err := r.FetchCommit(ctx, rev); err != nil {
			return "", err
		}
		return rev, nil
	}
	name, suffix := rev, ""
	if i := strings.IndexAny(rev, "~^"); i >= 0 {
		name, suffix = rev[:i], rev[i:]
	}
	if name == "" || strings.HasPrefix(name, "-") || !reRevSuffix.MatchString(suffix) ||
		(CheckBranch(name) != nil && CheckTag(name) != nil && !reHexRev.MatchString(name)) {
		return "", fmt.Errorf("%q is not a revision ctxtra accepts: give a commit ID, a tag or a branch, with ~N or ^N after it if you like", rev)
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := r.ensure(ctx, r.URL); err != nil {
		return "", err
	}
	ns := "refs/ctxtra/revs/" + uniq()
	defer func() {
		out, err := r.git(ctx, true, nil, "for-each-ref", "--format=delete %(refname)", ns+"/")
		if err == nil && len(out) > 0 {
			_, _ = r.git(ctx, true, bytes.NewReader(out), "update-ref", "--stdin")
		}
	}()
	if err := r.fetch(ctx, r.URL, "+refs/heads/*:"+ns+"/heads/*", "+refs/tags/*:"+ns+"/tags/*"); err != nil {
		return "", err
	}
	// A tag or a branch first: a name of hex digits could also be a short ID.
	bases := []string{ns + "/tags/" + name, ns + "/heads/" + name}
	if reHexRev.MatchString(name) {
		bases = append(bases, strings.ToLower(name))
	}
	for _, base := range bases {
		if c, err := r.commit(ctx, base+suffix); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("%s has no commit %s (or a short ID is ambiguous: give more digits)", r.URL, rev)
}

// TagHead is the commit that tag points to in the remote, through an
// annotated tag too. It reads only refs: nothing is fetched or stored.
func (r *Repo) TagHead(ctx context.Context, tag string) (string, error) {
	if err := CheckTag(tag); err != nil {
		return "", err
	}
	ref := "refs/tags/" + tag
	out, err := r.git(ctx, false, nil, "ls-remote", "--", r.URL, ref, ref+"^{}")
	if err != nil {
		return "", err
	}
	direct, peeled := "", ""
	for line := range strings.SplitSeq(string(out), "\n") {
		id, name, _ := strings.Cut(line, "\t")
		switch {
		case !ValidCommit(id):
		case name == ref:
			direct = id
		case name == ref+"^{}":
			peeled = id
		}
	}
	switch {
	case peeled != "":
		return peeled, nil
	case direct != "":
		return direct, nil
	}
	return "", fmt.Errorf("the tag %s is not in %s", tag, r.URL)
}

// TreeID is the ID of the tree of commit's folder subdir. Two commits whose
// folder has one tree ID have the same files there. subdir must be a
// folder: a symlink is never followed.
func (r *Repo) TreeID(ctx context.Context, commit, subdir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	name := commit + "^{tree}"
	if subdir != "" {
		name = commit + ":" + subdir
	}
	// One name per line: a path may have any other character.
	out, err := r.git(ctx, true, strings.NewReader(name+"\n"), "cat-file", "--batch-check")
	f := strings.Fields(string(out))
	switch {
	case err != nil:
		return "", err
	case len(f) == 3 && f[1] == "tree" && ValidCommit(f[0]):
		return f[0], nil
	case subdir == "":
		return "", fmt.Errorf("commit %s is not in ctxtra's copy of %s", Short(commit), r.URL)
	case len(f) == 3:
		return "", fmt.Errorf("%s at %s: subdir %s is a %s, not a folder", r.URL, Short(commit), Sanitize(subdir), Sanitize(f[1]))
	}
	return "", fmt.Errorf("%s at %s has no folder %s", r.URL, Short(commit), Sanitize(subdir))
}

// Validate checks commit, in the cache, against ctxtra's limits: the
// repository with its history, and the whole commit. Every command checks a
// commit it takes from the cache before it uses or records it, also one that
// was there before the fetch.
func (r *Repo) Validate(ctx context.Context, commit string) error {
	_, _, err := r.Files(ctx, commit, "")
	return err
}

// Files lists the regular files of commit's folder subdir, by their paths
// from it, and the symlinks and submodules there that the store leaves out.
// It fails as Validate does: the limits apply to the whole repository, also
// outside subdir.
func (r *Repo) Files(ctx context.Context, commit, subdir string) ([]Entry, []Skipped, error) {
	if dirSize(r.dir) > maxRepoBytes {
		return nil, nil, fmt.Errorf("%s: %w", r.URL, errRepoSize)
	}
	entries, skipped, err := r.Tree(ctx, commit)
	if err != nil {
		return nil, nil, fmt.Errorf("%s at %s: %w", r.URL, Short(commit), err)
	}
	if subdir == "" {
		return entries, skipped, nil
	}
	if _, err := r.TreeID(ctx, commit, subdir); err != nil {
		return nil, nil, err
	}
	prefix := subdir + "/"
	var in []Entry
	for _, e := range entries {
		if p, ok := strings.CutPrefix(e.Path, prefix); ok {
			e.Path = p
			in = append(in, e)
		}
	}
	var left []Skipped
	for _, s := range skipped {
		if p, ok := strings.CutPrefix(s.Path, prefix); ok {
			s.Path = p
			left = append(left, s)
		}
	}
	return in, left, nil
}
