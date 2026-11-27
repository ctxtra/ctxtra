// Package catalog is what a config's bindings can reference: files and skill
// folders on disk and in sources, and MCP servers of the config and of
// sources' manifests. It knows which sources can be read on this machine and
// where their content is, and it reads lazily: a source's manifest or files
// only when an entry asks for them.
package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
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
	live    trust.Live
	// store is the use of commits in ctxtra's store, shared by copies of
	// the catalog.
	store *storeUse
	// colls are the collections read, by their definition.
	colls map[*config.Collection]*coll
	// expansions counts the expansions of collections in the current
	// evaluation, up to maxExpansions; tooMany is the error of the ones
	// past it.
	expansions int
	tooMany    *Error
}

// storeUse is what a catalog marked as used in ctxtra's store. All marks
// share one wait for a cleanup of the store; after one failed, every
// commit not marked yet is refused at once.
type storeUse struct {
	marked   map[string]error
	waitLeft time.Duration
	busy     error
}

// touch is source.Touch; a variable for tests.
var touch = source.Touch

// use marks the accepted commit of s as used, before anything is read from
// it: cleanup keeps what sessions use. It fails while cleanup owns the
// store past the wait; the commit must then not be read.
func (c *Catalog) use(s *Source) error {
	if s.Live() || s.Top == "" || s.fsys != nil {
		return nil
	}
	gen := s.Top
	if err, ok := c.store.marked[gen]; ok {
		return err
	}
	err := c.store.busy
	if err == nil {
		start := time.Now()
		err = touch(gen, max(c.store.waitLeft, 0))
		c.store.waitLeft -= time.Since(start)
		c.store.busy = err
	}
	if err != nil {
		err = fmt.Errorf("source %s: ctxtra's store is being cleaned (%v); try again", s.Name, err)
	}
	c.store.marked[gen] = err
	return err
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

	// fsys has the files of a source read from git, not from a folder.
	fsys fs.FS

	manifest     *manifest.Manifest
	manifestErrs []*Error
	manifestRead bool
	manifestSize int
	symlinksRead bool
	symlinks     []string
	symlinksErr  error
	linkTree     *symlinkTree
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
		conf:    c,
		file:    configFile,
		base:    filepath.Dir(paths.Real(configFile)),
		sources: map[string]*Source{},
		renamed: map[string]string{},
		store:   &storeUse{marked: map[string]error{}, waitLeft: source.TouchWait},
	}
	for _, s := range c.Sources {
		src := cat.source(s, l)
		cat.sources[s.Name] = src
		cat.order = append(cat.order, src)
	}
	roots := cat.checkRoutes()
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
		if why := c.live.Check(s.Name, s.Dir, src.Top); why != "" {
			src.Top = ""
			return fail(Inactive, yamlx.Inactive, "%s", why)
		}
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
	src.Top = paths.Real(dir)
	return src
}

// checkRoutes checks the symlinks on the way to each active dir source,
// and returns the dir sources that pass, as trusted roots. A link may be
// trusted because it is in another dir source that passed: those are
// found step by step, so that no source's trust comes from itself.
func (c *Catalog) checkRoutes() []trust.Root {
	passed := map[*Source]bool{}
	var roots []trust.Root
	for changed := true; changed; {
		changed = false
		others := trust.New(roots)
		for _, s := range c.order {
			if s.State == Active && s.Live() && !passed[s] && trust.Route(s.Dir, s.Top, others) == "" {
				passed[s], changed = true, true
				roots = append(roots, trust.Root{Name: s.Name, Path: s.Top})
			}
		}
	}
	others := trust.New(roots)
	for _, s := range c.order {
		if s.State == Active && s.Live() && !passed[s] {
			s.State, s.Why = Inactive, trust.Route(s.Dir, s.Top, others)
			s.Top = ""
			s.Err = &Error{File: c.file, Err: &yamlx.Error{Pos: s.Pos, Mistake: yamlx.Inactive, Part: yamlx.Part{Kind: "source", Name: s.Name}, Msg: s.Why}}
		}
	}
	return roots
}

// Route says why the symlinks on the way to dir source name keep it from
// being read, "" when they don't, with the other active dir sources as
// the only trusted folders.
func (c *Catalog) Route(name string) string {
	s := c.sources[name]
	if s == nil || s.Dir == "" {
		return ""
	}
	var roots []trust.Root
	for _, o := range c.order {
		if o != s && o.State == Active && o.Live() {
			roots = append(roots, trust.Root{Name: o.Name, Path: o.Top})
		}
	}
	dir := filepath.Clean(paths.Expand(s.Dir))
	return trust.Route(s.Dir, paths.Real(dir), trust.New(roots))
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

// CommitDir is where the accepted commit of a lock entry is on this
// machine: ctxtra's store keeps each commit under the address it was
// downloaded from.
func CommitDir(e *lock.Entry) string { return commitDir(e) }

// WithFiles is the catalog with source name read from fsys: what the config
// would use from another commit, for the review of an update. top is the
// folder that the source's paths in messages start from; nothing is read
// there.
func (c *Catalog) WithFiles(name string, fsys fs.FS, top string) *Catalog {
	cp := *c
	cp.sources = map[string]*Source{}
	cp.order = nil
	cp.colls, cp.expansions, cp.tooMany = nil, 0, nil
	for _, s := range c.order {
		if s.Name == name {
			s = &Source{Source: s.Source, State: Active, Top: top, fsys: fsys}
		}
		cp.sources[s.Name] = s
		cp.order = append(cp.order, s)
	}
	return &cp
}

func commitDir(e *lock.Entry) string {
	remote := e.Repo
	if config.IsLocalRepo(remote) {
		remote = filepath.Clean(paths.Expand(remote))
	}
	return source.GenDir(remote, config.CleanSubdir(e.Subdir), e.Commit)
}

const notHere = "is not on this machine"

// NotOnThisMachine reports whether source s is not on this machine: a dir folder or
// a local repo path that doesn't exist. That is not an error: entries that
// use the source are left out, and nothing checks or downloads it.
func NotOnThisMachine(s *config.Source) bool {
	p := s.Dir
	if p == "" {
		if !config.IsLocalRepo(s.Repo) {
			return false
		}
		p = s.Repo
	}
	return folder(filepath.Clean(paths.Expand(p))) == notHere
}

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
// source has none or when it can't be read now. The errors are the
// manifest's, and why it can't be read: nil with no errors means it has
// none.
func (c *Catalog) Manifest(s *Source) (*manifest.Manifest, []*Error) {
	m, errs, err := c.ManifestWithin(s, nil)
	if err != nil {
		errs = append(errs, &Error{File: s.Name + ":" + manifest.FileName, Err: &yamlx.Error{Mistake: yamlx.Unreadable, Msg: err.Error()}})
	}
	return m, errs
}

// ManifestWithin is Manifest read with rd's limits, when it is not read
// yet; a manifest read before counts its size in rd. err is set when the
// manifest doesn't fit in what rd has left: it is then not read, and not
// kept as read.
func (c *Catalog) ManifestWithin(s *Source, rd *fsutil.Reader) (*manifest.Manifest, []*Error, error) {
	if s.State != Active {
		return nil, nil, nil
	}
	if err := c.use(s); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(s.Top, manifest.FileName)
	if s.manifestRead {
		if rd != nil && s.manifestSize > 0 {
			if err := rd.Count(path, s.manifestSize); err != nil {
				return nil, nil, err
			}
		}
		return s.manifest, s.manifestErrs, nil
	}
	it := Item{Path: path, Source: s.Name, Rel: manifest.FileName, stored: !s.Live(), fsys: s.fsys}
	file := s.Name + ":" + manifest.FileName
	data, _, err := c.readMax(it, rd, maxManifestBytes)
	if errors.Is(err, fsutil.ErrTotalLimit) {
		return nil, nil, err
	}
	m, errs := c.keepManifest(s, file, data, err)
	return m, errs, nil
}

// keepManifest keeps what reading the manifest of s gave.
func (c *Catalog) keepManifest(s *Source, file string, data []byte, err error) (*manifest.Manifest, []*Error) {
	s.manifestRead = true
	s.manifestSize = len(data)
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

// RepoSet is the repo set that entry e of location.repo-sets names: the
// config's own for a bare name, a source's for SOURCE:SET. A set with
// errors that leave it out can't be read; one whose entries have errors
// is the rest of its entries. The error of a set of a source whose content
// can't be used is an *Unavailable.
func (c *Catalog) RepoSet(e config.Entry) (*config.RepoSet, error) {
	var s *config.RepoSet
	where := "the config"
	if e.InSource {
		src := c.sources[e.Source]
		switch {
		case src == nil:
			return nil, fmt.Errorf("the config has no source %s", e.Source)
		case src.State != Active:
			return nil, &Unavailable{src}
		}
		m, _, err := c.ManifestWithin(src, nil)
		switch {
		case err != nil:
			return nil, err
		case m == nil:
			return nil, fmt.Errorf("source %s has no %s, which declares its repo sets", src.Name, manifest.FileName)
		}
		s, where = m.RepoSet(e.Value), "source "+src.Name
	} else {
		s = c.conf.RepoSet(e.Value)
	}
	switch {
	case s == nil:
		return nil, fmt.Errorf("%s has no repo set %s", where, e.Value)
	case s.Broken():
		return nil, fmt.Errorf("the repo set %s of %s has errors", e.Value, where)
	}
	return s, nil
}
