package catalog

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/trust"
)

// Kind is what a path entry adds.
type Kind int

const (
	// Files: instruction files.
	Files Kind = iota
	// Skills: skill folders, each with a SKILL.md.
	Skills
)

// maxGlobEntries bounds the folder entries one pattern may visit.
const maxGlobEntries = 10000

var errGlobLimit = errors.New("the pattern visits more than 10,000 folder entries")

// maxManifestBytes limits a manifest, which is read apart from the
// session's files.
const maxManifestBytes = 1 << 20

var readBounded = fsutil.ReadBounded

// Item is a file or a skill folder that an entry adds.
type Item struct {
	// Path is where it is: on disk, or in ctxtra's store for a repo source.
	Path string
	// Source is the source it is in, and Rel its slash-separated path from
	// the source's top; both "" for a path on disk.
	Source, Rel string
	// stored: in ctxtra's store; read only through paths without symlinks.
	stored bool
	// fsys has it at Rel when the source is read from git; Path is then
	// only its name in messages.
	fsys fs.FS
}

// Child is the file name in folder it.
func (it Item) Child(name string) Item {
	c := it
	c.Path = filepath.Join(it.Path, name)
	if it.Rel != "" {
		c.Rel = path.Join(it.Rel, name)
	}
	return c
}

// Ref is how the config names it: SOURCE:PATH, or the path on disk.
func (it Item) Ref() string {
	if it.Source != "" {
		return it.Source + ":" + it.Rel
	}
	return it.Path
}

// Paths are the files or skill folders that positive entry e of an
// instructions or skills list adds, sorted, and the errors of what it
// couldn't add: folders the trust rules refuse leave out only what is in
// them. An entry that names a source whose content can't be used gives
// *Unavailable.
func (c *Catalog) Paths(e config.Entry, kind Kind) ([]Item, []error) {
	if !e.InSource {
		base, rest := c.diskBase(e.Value)
		root, pattern := path.Dir(rest), path.Base(rest)
		if hasMeta(rest) {
			root, pattern = doublestar.SplitPattern(rest)
		}
		dir := filepath.Join(base, filepath.FromSlash(root))
		found, errs := c.glob(nil, dir, pattern, e.Value, kind, true, nil)
		items := make([]Item, len(found))
		for i, rel := range found {
			items[i] = Item{Path: filepath.Join(dir, filepath.FromSlash(rel))}
		}
		return items, errs
	}
	s := c.sources[e.Source]
	pattern := path.Clean(e.Value)
	switch {
	case s == nil:
		return nil, []error{fmt.Errorf("%s: the config has no source %s", e.Text, e.Source)}
	case s.State != Active:
		return nil, []error{&Unavailable{s}}
	case pattern == ".." || strings.HasPrefix(pattern, "../"):
		return nil, []error{fmt.Errorf("%s: the path leaves its source", e.Text)}
	}
	if err := c.use(s); err != nil {
		return nil, []error{fmt.Errorf("%s: %w", e.Text, err)}
	}
	_, err := s.symlinkPaths()
	if err != nil {
		return nil, []error{fmt.Errorf("%s: %w", e.Text, err)}
	}
	found, errs := c.glob(s.fsys, s.Top, pattern, s.Name+":"+pattern, kind, s.Live(), s.linkTree)
	if !s.Live() {
		for _, err := range errs {
			var u *Unreadable
			if errors.As(err, &u) {
				if rel, ok := paths.Rel(s.Top, u.Path); ok {
					u.Ref = s.Name + ":" + rel
				}
			}
		}
	}
	items := make([]Item, len(found))
	for i, rel := range found {
		items[i] = Item{Path: filepath.Join(s.Top, filepath.FromSlash(rel)), Source: s.Name, Rel: rel, stored: !s.Live(), fsys: s.fsys}
	}
	return items, errs
}

// diskBase splits a path on disk as the config writes it into the folder it
// starts from, taken as it is, and the rest as written, slash-separated:
// absolute, starting with ~, or relative to the folder of the config's real
// file. Glob characters in the home or config folder's name stay literal.
func (c *Catalog) diskBase(p string) (base, rest string) {
	switch {
	case p == "~" || strings.HasPrefix(p, "~/"):
		base, rest = paths.Home(), strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/")
	case filepath.IsAbs(p):
		base, rest = "/", strings.TrimPrefix(filepath.ToSlash(p), "/")
	default:
		base, rest = c.base, filepath.ToSlash(p)
	}
	return base, path.Clean("./" + rest)
}

// DiskPattern is the absolute pattern of a path on disk as the config
// writes it, with glob characters of the folder it starts from escaped.
func (c *Catalog) DiskPattern(p string) string {
	base, rest := c.diskBase(p)
	return path.Join(escapeMeta(filepath.ToSlash(base)), rest)
}

func escapeMeta(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`*?[]{}\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Read returns the content and checked path of it, with rd's limits. A nil
// rd uses a manifest's limit. A file outside ctxtra's store is read only
// when the trust rules allow it; when they don't, the error is *Untrusted.
func (c *Catalog) Read(it Item, rd *fsutil.Reader) ([]byte, string, error) {
	limit := maxManifestBytes
	if rd != nil {
		limit = math.MaxInt // rd's own limit
	}
	return c.readMax(it, rd, limit)
}

// readMax is read with at most limit bytes for this file.
func (c *Catalog) readMax(it Item, rd *fsutil.Reader, limit int) ([]byte, string, error) {
	p := it.Path
	if it.Source != "" {
		if err := c.sources[it.Source].repoPathError(it.Rel); err != nil {
			return nil, "", err
		}
	}
	if it.fsys != nil {
		data, err := readFS(it, rd, limit)
		return data, p, err
	}
	var err error
	if from, real, ok := c.overlaid(p); ok && !it.stored {
		if from, err = c.trust.Resolve(from); err != nil {
			return nil, "", &Untrusted{err}
		}
		var data []byte
		if rd == nil {
			data, err = readBounded(from, limit)
		} else {
			data, err = rd.ReadMax(from, limit)
		}
		return data, real, err
	}
	if !it.stored {
		p, err = c.trust.Resolve(p)
		if err != nil {
			return nil, "", &Untrusted{err}
		}
	} else {
		p, err = trust.Stored(c.sources[it.Source].Top, p)
		if err != nil {
			return nil, "", err
		}
	}
	var data []byte
	if rd == nil {
		data, err = readBounded(p, limit)
	} else {
		data, err = rd.ReadMax(p, limit)
	}
	return data, p, err
}

// readFS reads it from its source's files in git, with at most limit bytes
// and rd's limits.
func readFS(it Item, rd *fsutil.Reader, limit int) ([]byte, error) {
	fi, err := fs.Stat(it.fsys, it.Rel)
	switch {
	case err != nil:
		return nil, err
	case !fi.Mode().IsRegular():
		return nil, fmt.Errorf("%s is not a regular file", it.Path)
	case fi.Size() > int64(limit):
		return nil, fmt.Errorf("%s is larger than %d bytes", it.Path, limit)
	}
	data, err := fs.ReadFile(it.fsys, it.Rel)
	if err == nil && rd != nil {
		err = rd.Count(it.Path, len(data))
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Untrusted is the error of a file that others can change, outside a
// trusted dir source, or that a symlink leads to.
type Untrusted struct{ Err error }

func (u *Untrusted) Error() string { return u.Err.Error() }
func (u *Untrusted) Unwrap() error { return u.Err }

// IsUntrusted reports whether err is a file the trust rules refuse.
func IsUntrusted(err error) bool {
	var u *Untrusted
	var t *trust.Error
	var d *trust.DeniedError
	return errors.As(err, &u) || errors.As(err, &t) || errors.As(err, &d)
}

func hasMeta(p string) bool { return strings.ContainsAny(p, `*?[{\`) }

// glob is what pattern, slash-separated and relative to root, matches, as
// sorted relative paths. live is a folder read from disk: symlinks are
// followed, to files for the trust check when the file is read and to
// folders after it. A folder the trust rules refuse isn't listed, and an
// entry that can't be read, such as a symlink to itself, is left out: their
// errors come with what the rest of the pattern found. Going over the
// traversal limits fails the whole pattern. In ctxtra's store
// there are no symlinks, and none is followed. With fsys, the files are
// there, not in root, which only names them in messages. ref is the pattern
// as the config names it.
func (c *Catalog) glob(fsys fs.FS, root, pattern, ref string, kind Kind, live bool, links *symlinkTree) ([]string, []error) {
	t := &tree{FS: fsys, root: root, trust: c.trust, live: live, virtual: fsys != nil, left: maxGlobEntries,
		ids: map[string]fileID{}, skippedPaths: map[string]bool{}, pattern: hasMeta(pattern), overlaid: c.overlaid}
	if links != nil {
		t.links, t.linkPaths = links.links, links.paths
	}
	if fsys == nil {
		t.FS = os.DirFS(root)
	}
	what := map[Kind]string{Files: "file", Skills: "skill folder with a SKILL.md"}[kind]
	if !t.pattern {
		if t.match(pattern, kind) {
			return []string{pattern}, nil
		}
		if len(t.skipped) > 0 {
			return nil, t.skipped
		}
		return nil, []error{fmt.Errorf("%s is not a %s", paths.Tilde(filepath.Join(root, filepath.FromSlash(pattern))), what)}
	}
	opts := []doublestar.GlobOption{doublestar.WithFailOnIOErrors()}
	if !live {
		opts = append(opts, doublestar.WithNoFollow())
	}
	var found []string
	err := doublestar.GlobWalk(t, pattern, func(p string, d fs.DirEntry) error {
		if t.left < 0 {
			return errGlobLimit
		}
		if t.match(p, kind) {
			found = append(found, p)
		}
		if t.left < 0 {
			return errGlobLimit
		}
		return nil
	}, opts...)
	if t.left < 0 && err == nil {
		err = errGlobLimit
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, append(t.skipped, err)
	}
	if len(found) == 0 && len(t.skipped) == 0 {
		return nil, []error{fmt.Errorf("%s matches no %s", ref, what)}
	}
	slices.Sort(found)
	return slices.Compact(found), t.skipped
}

// tree is a folder for glob, with a bound on the entries it visits.
type tree struct {
	fs.FS
	root  string
	trust *trust.Policy
	live  bool
	// virtual: the files are in FS only, not under root.
	virtual     bool
	pattern     bool
	links       map[string]bool
	linkPaths   []string
	extraDirs   map[string]map[string]fs.DirEntry
	virtualDirs map[string]bool
	left        int
	// skipped are the errors of entries left out, once each: folders the
	// trust rules refuse, and entries that can't be read, such as a symlink
	// to itself.
	skipped      []error
	skippedPaths map[string]bool
	// ids are the folders listed, by their path, for finding symlink
	// cycles.
	ids map[string]fileID
	// overlaid is Catalog.overlaid, for live folders.
	overlaid func(string) (string, string, bool)
}

// statLive is os.Stat of full, in the catalog's overlay if it is there.
func (t *tree) statLive(full string) (fs.FileInfo, error) {
	if t.overlaid != nil {
		if from, _, ok := t.overlaid(full); ok {
			full = from
		}
	}
	return os.Stat(full)
}

// fileID is what makes two paths the same folder.
type fileID struct{ fi os.FileInfo }

func (t *tree) id(name string) (os.FileInfo, bool) {
	if id, ok := t.ids[name]; ok {
		return id.fi, id.fi != nil
	}
	fi, err := os.Stat(filepath.Join(t.root, filepath.FromSlash(name)))
	if err != nil {
		fi = nil
	}
	t.ids[name] = fileID{fi}
	return fi, fi != nil
}

// cycle reports whether folder name is one of the folders it is in, which
// a symlink can make: listing it would go round without end.
func (t *tree) cycle(name string) bool {
	fi, ok := t.id(name)
	if !ok || name == "." {
		return false
	}
	for dir := path.Dir(name); ; dir = path.Dir(dir) {
		if up, ok := t.id(dir); ok && os.SameFile(fi, up) {
			return true
		}
		if dir == "." {
			return false
		}
	}
}

// match reports whether p is an item of kind: a file, or a folder with a
// SKILL.md file. Outside ctxtra's store a symlink counts by what it leads
// to.
func (t *tree) match(p string, kind Kind) bool {
	full := filepath.Join(t.root, filepath.FromSlash(p))
	stat := t.statStored
	switch {
	case t.virtual:
		stat = t.statFS
	case t.live:
		stat = t.statLive
	}
	fi, err := stat(full)
	if err != nil {
		if kind == Skills && !t.live && os.IsNotExist(err) {
			if linkErr := t.repoPathError(path.Join(p, "SKILL.md")); linkErr != nil {
				t.skip(filepath.Join(full, "SKILL.md"), linkErr)
				return false
			}
		}
		t.skip(full, err)
		return false
	}
	if kind == Files {
		return fi.Mode().IsRegular()
	}
	if !fi.IsDir() {
		return false
	}
	md := filepath.Join(full, "SKILL.md")
	skill, err := stat(md)
	if err != nil {
		t.skip(md, err)
		return false
	}
	return skill.Mode().IsRegular()
}

// statFS stats full, a path under root, in the tree's FS.
func (t *tree) statFS(full string) (fs.FileInfo, error) {
	rel, err := filepath.Rel(t.root, full)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	if err := t.repoPathError(rel); err != nil {
		return nil, err
	}
	fi, err := fs.Stat(t.FS, rel)
	if os.IsNotExist(err) {
		if exists, lookupErr := t.virtualDirectory(rel); lookupErr != nil {
			return nil, lookupErr
		} else if exists {
			return virtualDir(path.Base(rel)), nil
		}
	}
	return fi, err
}

func (t *tree) statStored(full string) (fs.FileInfo, error) {
	rel, _ := filepath.Rel(t.root, full)
	rel = filepath.ToSlash(rel)
	if err := t.repoPathError(rel); err != nil {
		return nil, err
	}
	parent := t.root
	if full != t.root {
		parent = filepath.Dir(full)
	}
	if _, err := trust.Stored(t.root, parent); err != nil {
		if os.IsNotExist(err) {
			if exists, lookupErr := t.virtualDirectory(rel); lookupErr != nil {
				return nil, lookupErr
			} else if exists {
				return virtualDir(path.Base(rel)), nil
			}
		}
		return nil, err
	}
	fi, err := os.Lstat(full)
	if os.IsNotExist(err) {
		if exists, lookupErr := t.virtualDirectory(rel); lookupErr != nil {
			return nil, lookupErr
		} else if exists {
			return virtualDir(path.Base(rel)), nil
		}
	}
	if err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return nil, errSymlink
	}
	return fi, err
}

func (t *tree) spend(n int) error {
	t.left -= n
	if t.left < 0 {
		return errGlobLimit
	}
	return nil
}

func (t *tree) Open(name string) (fs.File, error) {
	if err := t.spend(1); err != nil {
		return nil, err
	}
	return t.FS.Open(name)
}

func (t *tree) Stat(name string) (fs.FileInfo, error) {
	if err := t.spend(1); err != nil {
		return nil, err
	}
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	full := filepath.Join(t.root, filepath.FromSlash(name))
	stat := t.statStored
	switch {
	case t.virtual:
		stat = t.statFS
	case t.live:
		stat = t.statLive
	}
	fi, err := stat(full)
	if err != nil {
		t.skip(full, err)
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return fi, nil
}

// skip records why entry full of the tree is left out; one that doesn't
// exist needs no error.
func (t *tree) skip(full string, err error) {
	if errors.Is(err, fs.ErrNotExist) || t.skippedPaths[full] {
		return
	}
	t.skippedPaths[full] = true
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	t.skipped = append(t.skipped, &Unreadable{Path: full, Err: err})
}

// Unreadable is the error of a file or folder that a pattern found and
// couldn't read, such as a symlink to itself: only it is left out.
type Unreadable struct {
	Path string
	// Ref names the path within its source, when there is one.
	Ref string
	Err error
}

func (u *Unreadable) Error() string {
	name := u.Ref
	if name == "" {
		name = paths.Tilde(u.Path)
	}
	return name + " can't be read: " + u.Err.Error()
}
func (u *Unreadable) Unwrap() error { return u.Err }

func (t *tree) ReadDir(name string) ([]fs.DirEntry, error) {
	if strings.Count(name, "/") > 64 {
		return nil, errors.New("the pattern goes more than 64 folders deep")
	}
	if t.live {
		// A folder the trust rules refuse isn't listed: its file names
		// could be someone else's too.
		dir := filepath.Join(t.root, filepath.FromSlash(name))
		probe := filepath.Join(dir, ".ctxtra-glob")
		if err := t.trust.Check(probe); err != nil {
			var te *trust.Error
			if errors.As(err, &te) {
				te.Path = dir
				if te.Via == probe || filepath.Base(te.Via) == ".ctxtra-glob" {
					te.Via = filepath.Dir(te.Via)
				}
			}
			if !t.skippedPaths[dir] {
				t.skippedPaths[dir] = true
				t.skipped = append(t.skipped, &Untrusted{err})
			}
			return nil, nil
		}
		if t.cycle(name) {
			return nil, nil
		}
	}
	full := filepath.Join(t.root, filepath.FromSlash(name))
	if err := t.repoPathError(name); err != nil {
		t.skip(full, err)
		return nil, nil
	}
	children, err := t.linkChildren(name)
	if err != nil {
		return nil, err
	}
	if !t.live && !t.virtual {
		if _, err := trust.Stored(t.root, full); err != nil {
			if !os.IsNotExist(err) || len(children) == 0 {
				t.skip(full, err)
				return nil, nil
			}
		}
	}
	f, err := t.FS.Open(name)
	var entries []fs.DirEntry
	if err != nil {
		if !os.IsNotExist(err) || len(children) == 0 {
			t.skip(full, err)
			return nil, nil
		}
	} else {
		defer f.Close()
		d, ok := f.(fs.ReadDirFile)
		if !ok {
			return nil, nil
		}
		entries, err = d.ReadDir(max(t.left, 0) + 1)
		if err != nil && err != io.EOF {
			t.skip(full, err)
			return nil, nil
		}
	}
	if err := t.spend(len(entries) + 1); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i, entry := range entries {
		seen[entry.Name()] = true
		if !t.live && entry.Type()&fs.ModeSymlink != 0 {
			entries[i] = virtualDir(entry.Name())
		}
	}
	for child, entry := range children {
		if !seen[child] {
			entries = append(entries, entry)
		}
	}
	if !t.live {
		entries = slices.DeleteFunc(entries, func(d fs.DirEntry) bool { return strings.EqualFold(d.Name(), ".git") })
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}
