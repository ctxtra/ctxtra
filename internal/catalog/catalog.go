// Package catalog is what a config's bindings can reference: files and skill
// folders on disk and in sources, and MCP servers of the config and of
// sources' manifests. It knows which sources can be read on this machine and
// where their content is, and it reads lazily: a source's manifest or files
// only when an entry asks for them.
package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/trust"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Catalog is what the bindings of one config can reference.
type Catalog struct {
	conf *config.Config
	// file is the config file; base the folder of its real file, which
	// relative paths in the config start from.
	file, base string
	sources    map[string]*Source
	order      []*Source
	trust      *trust.Policy
	// renamed is the config's name for a source's server, by SOURCE:SERVER.
	renamed map[string]string
}

// State is whether a source's content can be used on this machine.
type State int

const (
	// Active: its content is read, live or at the accepted commit.
	Active State = iota
	// Inactive: a repo source without a matching lock entry or a downloaded
	// commit, or the lock can't be read.
	Inactive
	// NotHere: a local repo path or a dir folder that isn't on this machine.
	// Not an error: entries that use it are left out.
	NotHere
	// Broken: the source's entry in the config has errors, or its path isn't
	// a folder or a repository.
	Broken
)

// Source is a source of the config, with what this machine has of it.
type Source struct {
	*config.Source
	State State
	// Top is the folder that the source's paths start from, without
	// symlinks: its accepted commit in ctxtra's store, under subdir, or the
	// dir folder. "" unless the source is active.
	Top string
	// Why says why the source isn't active, "" when it is.
	Why string
	// Err is the error that makes the source inactive or broken; nil when
	// there is none, or when the config's own errors say it.
	Err *Error

	manifest     *manifest.Manifest
	manifestErrs []*Error
	manifestRead bool
}

// Live reports whether the source is a folder read as it is on disk.
func (s *Source) Live() bool { return s.Dir != "" }

// Error is a mistake in one of the files that resolving reads: the config,
// the lock or a source's manifest.
type Error struct {
	// File is the file it is in, "" for none.
	File string
	Err  *yamlx.Error
}

func (e *Error) Error() string {
	if e.File == "" {
		return e.Err.Error()
	}
	return paths.Tilde(e.File) + ": " + e.Err.Error()
}

// Unavailable is the error of an entry that names a source whose content
// can't be used: inactive, broken or not on this machine. The entry adds
// nothing; the source's own error, if any, is reported once with the
// source.
type Unavailable struct{ Source *Source }

func (u *Unavailable) Error() string { return "source " + u.Source.Name + ": " + u.Source.Why }

// New is the catalog of config c with lock l, for the config file at
// configFile. It checks which sources this machine has, and reads nothing
// else.
func New(c *config.Config, l *lock.Lock, configFile string) *Catalog {
	cat := &Catalog{
		conf: c,
		file:    configFile,
		base:    filepath.Dir(paths.Real(configFile)),
		sources: map[string]*Source{},
		renamed: map[string]string{},
	}
	var roots []trust.Root
	for _, s := range c.Sources {
		src := cat.source(s, l)
		cat.sources[s.Name] = src
		cat.order = append(cat.order, src)
		if src.State == Active && src.Live() {
			roots = append(roots, trust.Root{Name: s.Name, Path: src.Top})
		}
	}
	// ctxtra's store is read only through SOURCE:PATH, which keeps a
	// source's files to the accepted commit.
	cat.trust = trust.New(roots, trust.Root{Name: "ctxtra's store (use SOURCE:PATH)", Path: source.StoreDir()})
	for _, s := range c.MCPServers {
		if u := s.Use(); u != "" && !s.Broken() {
			cat.renamed[u] = s.Name
		}
	}
	return cat
}

// Sources are the config's sources, in config order.
func (c *Catalog) Sources() []*Source { return c.order }

// Source is the source called name, nil when the config has none.
func (c *Catalog) Source(name string) *Source { return c.sources[name] }

// Errors are the errors of sources that this machine can't read, in config
// order.
func (c *Catalog) Errors() []*Error {
	var out []*Error
	for _, s := range c.order {
		if s.Err != nil {
			out = append(out, s.Err)
		}
	}
	return out
}

// source is s with what this machine has of it.
func (c *Catalog) source(s *config.Source, l *lock.Lock) *Source {
	src := &Source{Source: s}
	fail := func(state State, m yamlx.Mistake, format string, args ...any) *Source {
		src.State, src.Why = state, fmt.Sprintf(format, args...)
		if m != 0 {
			src.Err = &Error{File: c.file, Err: &yamlx.Error{Pos: s.Pos, Mistake: m, Part: yamlx.Part{Kind: "source", Name: s.Name}, Msg: src.Why}}
		}
		return src
	}
	if s.Broken() {
		return fail(Broken, 0, "the source has errors in the config")
	}
	if s.Dir != "" {
		dir := filepath.Clean(paths.Expand(s.Dir))
		if why := folder(dir); why != "" {
			if why == notHere {
				return fail(NotHere, 0, "%s is not on this machine", s.Dir)
			}
			return fail(Broken, yamlx.BrokenSource, "dir %s %s", s.Dir, why)
		}
		src.Top = paths.Real(dir)
		return src
	}
	if config.IsLocalRepo(s.Repo) {
		dir := filepath.Clean(paths.Expand(s.Repo))
		why := folder(dir)
		if why == "" && !isRepo(dir) {
			why = "is not a git repository"
		}
		switch why {
		case "":
		case notHere:
			return fail(NotHere, 0, "%s is not on this machine", s.Repo)
		default:
			return fail(Broken, yamlx.BrokenSource, "repo %s %s", s.Repo, why)
		}
	}
	if !l.Readable() {
		return fail(Inactive, 0, "ctxtra.lock can't be read")
	}
	e := l.Entry(s.Name)
	if e == nil {
		return fail(Inactive, yamlx.Inactive, "ctxtra.lock has no accepted commit of it; run `ctxtra source update %s`", s.Name)
	}
	if ch := lock.Compare(e, s); !ch.Same() {
		return fail(Inactive, yamlx.Inactive, "ctxtra.lock has a commit accepted for another %s; run `ctxtra source update %s` to review it",
			changed(ch), s.Name)
	}
	dir := commitDir(e)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fail(Inactive, yamlx.Inactive, "the accepted commit %s is not downloaded on this machine; run `ctxtra source download`",
			source.Short(e.Commit))
	}
	src.Top = filepath.Join(paths.Real(dir), filepath.FromSlash(config.CleanSubdir(s.Subdir)))
	return src
}

// changed names what a config source changed since its lock entry.
func changed(ch lock.Changes) string {
	var parts []string
	if ch.Repo {
		parts = append(parts, "repo")
	}
	if ch.Ref {
		parts = append(parts, "branch, tag or rev")
	}
	if ch.Subdir {
		parts = append(parts, "subdir")
	}
	return strings.Join(parts, " and ")
}

// commitDir is where the accepted commit of a lock entry is on this
// machine: ctxtra's store keeps each commit under the address it was
// downloaded from.
func commitDir(e *lock.Entry) string {
	remote := e.Repo
	if config.IsLocalRepo(remote) {
		remote = filepath.Clean(paths.Expand(remote))
	}
	return source.GenDir(remote, e.Commit)
}

const notHere = "is not on this machine"

// folder says what keeps dir from being a folder, notHere when it doesn't
// exist, "" when it is one.
func folder(dir string) string {
	fi, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		return notHere
	case err != nil:
		return fmt.Sprintf("can't be read: %v", err)
	case !fi.IsDir():
		return "is not a folder"
	}
	return ""
}

// isRepo reports whether dir is a git repository: a clone, or a bare
// repository.
func isRepo(dir string) bool {
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	fi, err := os.Stat(filepath.Join(dir, "objects"))
	_, herr := os.Stat(filepath.Join(dir, "HEAD"))
	return err == nil && fi.IsDir() && herr == nil
}

// ManifestErrors are the errors of the source's manifest, if an entry has
// read it.
func (s *Source) ManifestErrors() []*Error { return s.manifestErrs }

// Manifest is the manifest of active source s, read once; nil when the
// source has none. The errors are the manifest's.
func (c *Catalog) Manifest(s *Source) (*manifest.Manifest, []*Error) {
	if s.manifestRead || s.State != Active {
		return s.manifest, s.manifestErrs
	}
	s.manifestRead = true
	it := Item{Path: filepath.Join(s.Top, manifest.FileName), Source: s.Name, Rel: manifest.FileName, stored: !s.Live()}
	file := s.Name + ":" + manifest.FileName
	data, err := c.read(it, nil)
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		s.manifestErrs = []*Error{{File: file, Err: &yamlx.Error{Mistake: yamlx.Unreadable, Msg: err.Error()}}}
		return nil, s.manifestErrs
	}
	s.manifest = manifest.Parse(data)
	for _, e := range s.manifest.Errors {
		s.manifestErrs = append(s.manifestErrs, &Error{File: file, Err: e})
	}
	return s.manifest, s.manifestErrs
}

// errNotImplemented is the error of what this version parses but doesn't
// read yet.
type errNotImplemented string

func (e errNotImplemented) Error() string { return string(e) + " are not implemented yet" }

// Collections are the collections that entry e of a collections list names.
// Collections are not implemented yet: it is always an error.
func (c *Catalog) Collections(e config.Entry) ([]*config.Collection, error) {
	return nil, errNotImplemented("collections")
}

// RepoSet is the repo set that entry e of location.repo-sets names. Repo
// sets are not implemented yet: it is always an error.
func (c *Catalog) RepoSet(e config.Entry) (*config.RepoSet, error) {
	return nil, errNotImplemented("repo sets")
}
