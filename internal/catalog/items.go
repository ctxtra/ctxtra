package catalog

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ctxtra/ctxtra/internal/cfg"
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

// maxManifestBytes limits a manifest, which is read apart from the
// session's files.
const maxManifestBytes = 1 << 20

// Item is a file or a skill folder that an entry adds.
type Item struct {
	// Path is where it is: on disk, or in ctxtra's store for a repo source.
	Path string
	// Source is the source it is in, and Rel its slash-separated path from
	// the source's top; both "" for a path on disk.
	Source, Rel string
	// stored: in ctxtra's store, which has no symlinks; read without the
	// trust check.
	stored bool
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

// Real is where it is, without symlinks. The store has none, and a
// source's top is real already.
func (it Item) Real() string {
	if it.stored {
		return it.Path
	}
	return paths.Real(it.Path)
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
func (c *Catalog) Paths(e cfg.Entry, kind Kind) ([]Item, []error) {
	if !e.InSource {
		base, rest := c.diskBase(e.Value)
		root, pattern := path.Dir(rest), path.Base(rest)
		if hasMeta(rest) {
			root, pattern = doublestar.SplitPattern(rest)
		}
		dir := filepath.Join(base, filepath.FromSlash(root))
		found, errs := c.glob(dir, pattern, kind, true)
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
	found, errs := c.glob(s.Top, pattern, kind, s.Live())
	items := make([]Item, len(found))
	for i, rel := range found {
		items[i] = Item{Path: filepath.Join(s.Top, filepath.FromSlash(rel)), Source: s.Name, Rel: rel, stored: !s.Live()}
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

// Read is the content of it, read with rd's limits; nil rd reads with a
// manifest's limit. A file outside ctxtra's store is read only when the
// trust rules allow it; when they don't, the error is *Untrusted.
func (c *Catalog) Read(it Item, rd *fsutil.Reader) ([]byte, error) { return c.read(it, rd) }

func (c *Catalog) read(it Item, rd *fsutil.Reader) ([]byte, error) {
	p := it.Path
	if !it.stored {
		if err := c.trust.Check(p); err != nil {
			return nil, &Untrusted{err}
		}
	}
	if rd == nil {
		return fsutil.ReadBounded(p, maxManifestBytes)
	}
	return rd.Read(p)
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
// folders after it; a folder the trust rules refuse isn't listed, and its
// error comes with what the rest of the pattern found. In ctxtra's store
// there are no symlinks, and none is followed.
func (c *Catalog) glob(root, pattern string, kind Kind, live bool) ([]string, []error) {
	t := &tree{FS: os.DirFS(root), root: root, trust: c.trust, live: live, left: maxGlobEntries,
		ids: map[string]fileID{}, refusedDirs: map[string]bool{}}
	what := map[Kind]string{Files: "file", Skills: "skill folder with a SKILL.md"}[kind]
	if !hasMeta(pattern) {
		if t.match(pattern, kind) {
			return []string{pattern}, nil
		}
		return nil, []error{fmt.Errorf("%s is not a %s", paths.Tilde(filepath.Join(root, filepath.FromSlash(pattern))), what)}
	}
	opts := []doublestar.GlobOption{doublestar.WithFailOnIOErrors()}
	if !live {
		opts = append(opts, doublestar.WithNoFollow())
	}
	var found []string
	err := doublestar.GlobWalk(t, pattern, func(p string, d fs.DirEntry) error {
		if t.match(p, kind) {
			found = append(found, p)
		}
		return nil
	}, opts...)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, append(t.refused, err)
	}
	if len(found) == 0 && len(t.refused) == 0 {
		return nil, []error{fmt.Errorf("%s matches no %s", pattern, what)}
	}
	slices.Sort(found)
	return slices.Compact(found), t.refused
}

// tree is a folder for glob, with a bound on the entries it visits.
type tree struct {
	fs.FS
	root  string
	trust *trust.Policy
	live  bool
	left  int
	// refused are the errors of folders the trust rules refuse, once each.
	refused     []error
	refusedDirs map[string]bool
	// ids are the folders listed, by their path, for finding symlink
	// cycles.
	ids map[string]fileID
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
	stat := os.Lstat
	if t.live {
		stat = os.Stat
	}
	fi, err := stat(full)
	if err != nil {
		return false
	}
	if kind == Files {
		return fi.Mode().IsRegular()
	}
	if !fi.IsDir() {
		return false
	}
	skill, err := stat(filepath.Join(full, "SKILL.md"))
	return err == nil && skill.Mode().IsRegular()
}

func (t *tree) spend(n int) error {
	t.left -= n
	if t.left < 0 {
		return errors.New("the pattern visits more than 10,000 folder entries")
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
	if name == "." || t.live {
		return os.Stat(full)
	}
	return os.Lstat(full)
}

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
			if !t.refusedDirs[dir] {
				t.refusedDirs[dir] = true
				t.refused = append(t.refused, &Untrusted{err})
			}
			return nil, nil
		}
		if t.cycle(name) {
			return nil, nil
		}
	}
	f, err := t.FS.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d, ok := f.(fs.ReadDirFile)
	if !ok {
		return nil, fmt.Errorf("%s is not a folder", name)
	}
	entries, err := d.ReadDir(max(t.left, 0) + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if err := t.spend(len(entries) + 1); err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}
