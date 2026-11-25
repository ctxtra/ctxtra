package cfg

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/gitrepo"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Source is an entry of the config's sources: a git repository read at
// accepted commits (Repo), or a folder read live (Dir).
type Source struct {
	Name string
	Pos  yamlx.Pos
	// Repo is the repository's address as written; "" for a dir source.
	Repo string
	// Dir is the folder as written; "" for a repo source.
	Dir string
	// Ref is what a repo source follows.
	Ref Ref
	// Subdir is the folder of the repository that is the source's top; ""
	// for its root.
	Subdir string
	// CheckUpdates is false when the user turned update checks off; nil
	// means the default, on.
	CheckUpdates *bool
	// Errs are the source's errors. A broken source is left out, with every
	// entry that uses it.
	Errs []*yamlx.Error
}

// Broken reports whether an error leaves out the source.
func (s *Source) Broken() bool { return broken(s.Errs) }

// RefKind is the key that says what a repo source follows.
type RefKind string

const (
	Branch RefKind = "branch"
	Tag    RefKind = "tag"
	Rev    RefKind = "rev"
)

var refKinds = []RefKind{Branch, Tag, Rev}

// Ref is a branch, a tag or a full commit ID.
type Ref struct {
	Kind RefKind
	Name string
}

func (r Ref) String() string { return string(r.Kind) + " " + r.Name }

var reRev = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// IsFullCommitID reports whether s is a full commit ID: 40 hex characters,
// or 64 in a SHA-256 repository.
func IsFullCommitID(s string) bool { return reRev.MatchString(s) }

var reSCP = regexp.MustCompile(`^[^@/\s]+@[^:/\s]+:\S+$`)

// checkRepo says what is wrong with a repo address: an https:// or ssh://
// URL, user@host:path, or a local path, absolute or starting with ~.
func checkRepo(repo string) string {
	switch {
	case IsLocalRepo(repo):
		if strings.HasPrefix(repo, "~") && repo != "~" && !strings.HasPrefix(repo, "~/") {
			return fmt.Sprintf("repo %s: only ~ for your own home folder is read", repo)
		}
		return ""
	case reSCP.MatchString(repo) && !strings.Contains(repo, "://"):
		return ""
	}
	u, err := url.Parse(repo)
	if err == nil && (u.Scheme == "https" || u.Scheme == "ssh") && u.Host != "" && strings.Trim(u.Path, "/") != "" {
		return ""
	}
	return fmt.Sprintf("repo %s is not a git address: write an https:// or ssh:// URL, user@host:path, or a local path that is absolute or starts with ~", repo)
}

// IsLocalRepo reports whether a repo address is a path on this machine.
func IsLocalRepo(repo string) bool {
	return strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "~")
}

// RepoID is the form in which two addresses of one repository are equal:
// host/path without the scheme, user, port, a trailing .git or /, in lower
// case; for a local path, its full real path.
func RepoID(repo string) string {
	if !IsLocalRepo(repo) {
		return gitrepo.NormalizeRemote(repo)
	}
	p := filepath.Clean(paths.Expand(repo))
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

// CleanSubdir is subdir in the form the config and the lock compare.
func CleanSubdir(subdir string) string {
	if subdir == "" {
		return ""
	}
	c := path.Clean(subdir)
	if c == "." {
		return ""
	}
	return c
}

func decodeSource(d *yamlx.Doc, p yamlx.Pair) *Source {
	s := &Source{Name: p.Key, Pos: yamlx.At(p.K)}
	r := yamlx.NewReader(d, yamlx.Part{Kind: "source", Name: p.Key}, yamlx.BrokenSource, p.K, p.V)
	defer func() { s.Errs = r.Errs }()
	if err := CheckName(p.Key); err != nil {
		r.Fail(p.K, "%v", err)
	}
	if r.Broken() {
		return s
	}
	f, _ := r.Map(p.V, "a source", "repo", "dir", "branch", "tag", "rev", "subdir", "check-updates")
	text := func(key string) string {
		if n := f[key]; n != nil {
			v, _ := r.Text(n, key)
			return v
		}
		return ""
	}
	s.Repo, s.Dir, s.Subdir = text("repo"), text("dir"), text("subdir")
	var refs []string
	for _, k := range refKinds {
		if f[string(k)] != nil {
			refs = append(refs, string(k))
			s.Ref = Ref{Kind: k, Name: text(string(k))}
		}
	}
	if n := f["check-updates"]; n != nil {
		if b, ok := r.Bool(n, "check-updates"); ok {
			s.CheckUpdates = &b
		}
	}
	switch {
	case f["repo"] != nil && f["dir"] != nil:
		r.Fail(p.V, "a source has repo or dir, not both")
	case f["repo"] == nil && f["dir"] == nil:
		r.Fail(p.V, "a source needs repo, a git repository read at accepted commits, or dir, a folder read live")
	case f["dir"] != nil:
		for _, k := range slices.Concat(refs, keysIn(f, "subdir", "check-updates")) {
			why := map[string]string{
				"subdir":        "dir already names a folder",
				"check-updates": "there are no new versions to check",
			}[k]
			if why == "" {
				why = "it is read as it is on disk"
			}
			r.Fail(f[k], "a dir source has no %s: %s", k, why)
		}
		if s.Dir != "" && !IsLocalRepo(s.Dir) {
			r.Fail(f["dir"], "dir %s must be absolute or start with ~", s.Dir)
		}
	default:
		switch len(refs) {
		case 0:
			r.Fail(p.V, "a repo source needs one of branch, tag and rev")
		case 1:
		default:
			r.Fail(p.V, "a repo source has one of branch, tag and rev, not %s", strings.Join(refs, " and "))
		}
		if s.Repo != "" {
			if msg := checkRepo(s.Repo); msg != "" {
				r.Fail(f["repo"], "%s", msg)
			}
		}
		if s.Ref.Kind == Rev && s.Ref.Name != "" && !IsFullCommitID(s.Ref.Name) {
			r.Fail(f["rev"], "rev %s is not a full commit ID: rev is 40 hex characters, or 64 in a SHA-256 repository; ctxtra source add --rev writes it from any revision", s.Ref.Name)
		}
		if s.Subdir != "" {
			if strings.HasPrefix(s.Subdir, "/") || strings.HasPrefix(s.Subdir, "~") || leaves(s.Subdir) {
				r.Fail(f["subdir"], "subdir %s must be a folder inside the repository, relative to its root", s.Subdir)
			}
		}
	}
	return s
}

// keysIn are the keys of f among keys, in that order.
func keysIn(f map[string]*yaml.Node, keys ...string) []string {
	var out []string
	for _, k := range keys {
		if f[k] != nil {
			out = append(out, k)
		}
	}
	return out
}
