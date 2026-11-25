// Package lock is the model of the lock file, ctxtra.lock: for each repo
// source, the accepted commit and what it was accepted for. Only ctxtra
// writes it.
package lock

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/cfg"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// FileName is the lock's name, next to the config's real file.
const FileName = "ctxtra.lock"

// Path is the lock file of the config at configPath: next to its real file,
// after symlinks, so a config kept in dotfiles carries its lock along.
func Path(configPath string) string {
	if real, err := filepath.EvalSymlinks(configPath); err == nil {
		configPath = real
	}
	return filepath.Join(filepath.Dir(configPath), FileName)
}

// Lock is the lock file as read.
type Lock struct {
	// Entries are the file's entries, in file order.
	Entries []*Entry
	// Errors are every error in the file, in file order. A lock that can't
	// be read has one error and no entries: every repo source is inactive.
	Errors []*yamlx.Error
}

// Entry is the accepted commit of one repo source, and what it was
// accepted for: the address, branch, tag or rev, and subdir.
type Entry struct {
	Name   string
	Repo   string
	Ref    cfg.Ref
	Subdir string
	Commit string
	Pos    yamlx.Pos
	// Errs are the entry's errors; a broken entry isn't used.
	Errs []*yamlx.Error
}

// Broken reports whether an error leaves out the entry.
func (e *Entry) Broken() bool {
	return slices.ContainsFunc(e.Errs, func(err *yamlx.Error) bool { return err.Out() >= yamlx.OutPart })
}

// Readable reports whether the file could be read as a whole.
func (l *Lock) Readable() bool {
	return !slices.ContainsFunc(l.Errors, func(e *yamlx.Error) bool { return e.Out() == yamlx.OutFile })
}

// Entry is the usable entry of source name, nil when there is none.
func (l *Lock) Entry(name string) *Entry {
	for _, e := range l.Entries {
		if e.Name == name && !e.Broken() {
			return e
		}
	}
	return nil
}

// Parse reads a lock file.
func Parse(data []byte) *Lock {
	l := &Lock{}
	d, err := yamlx.Parse(data, yamlx.LockKind)
	if err != nil {
		l.Errors = []*yamlx.Error{err}
		return l
	}
	pairs, errs := d.Pairs(d.Root, func(key string) yamlx.Part { return yamlx.Part{Kind: "key", Name: key} })
	l.Errors = errs
	for _, p := range pairs {
		switch p.Key {
		case "schema":
		case "sources":
			entries, errs := cfg.NamedMap(d, p, "lock entry")
			l.Errors = append(l.Errors, errs...)
			for _, e := range entries {
				le := decodeEntry(d, e)
				l.Entries = append(l.Entries, le)
				l.Errors = append(l.Errors, le.Errs...)
			}
		default:
			l.Errors = append(l.Errors, cfg.UnknownKey(p, []string{"sources"}))
		}
	}
	cfg.SortErrors(l.Errors)
	return l
}

var entryKeys = []string{"repo", "branch", "tag", "rev", "subdir", "commit"}

func decodeEntry(d *yamlx.Doc, p yamlx.Pair) *Entry {
	e := &Entry{Name: p.Key, Pos: yamlx.At(p.K)}
	r := yamlx.NewReader(d, yamlx.Part{Kind: "lock entry", Name: p.Key}, yamlx.BrokenLockEntry, p.K, p.V)
	defer func() { e.Errs = r.Errs }()
	if err := cfg.CheckName(p.Key); err != nil {
		r.Fail(p.K, "%v", err)
	}
	if r.Broken() {
		return e
	}
	f, _ := r.Map(p.V, "a lock entry", entryKeys...)
	text := func(key string) string {
		if n := f[key]; n != nil {
			v, _ := r.Text(n, key)
			return v
		}
		return ""
	}
	e.Repo, e.Subdir, e.Commit = text("repo"), text("subdir"), text("commit")
	var refs []string
	for _, k := range []cfg.RefKind{cfg.Branch, cfg.Tag, cfg.Rev} {
		if f[string(k)] != nil {
			refs = append(refs, string(k))
			e.Ref = cfg.Ref{Kind: k, Name: text(string(k))}
		}
	}
	if f == nil {
		return e
	}
	for _, k := range []string{"repo", "commit"} {
		if f[k] == nil {
			r.Fail(p.V, "%s is missing", k)
		}
	}
	if len(refs) != 1 {
		r.Fail(p.V, "an entry has one of branch, tag and rev")
	}
	if e.Commit != "" {
		if msg := cfg.CommitIDError("commit", e.Commit); msg != "" {
			r.Fail(f["commit"], "%s", msg)
		}
	}
	if e.Ref.Kind == cfg.Rev && e.Ref.Name != "" {
		if msg := cfg.CommitIDError("rev", e.Ref.Name); msg != "" {
			r.Fail(f["rev"], "%s", msg)
		}
	}
	return e
}

// Changes are what a source's config entry changed since its lock entry
// was accepted.
type Changes struct {
	// Repo: the address names another repository.
	Repo bool
	// Ref: another branch, tag or rev.
	Ref bool
	// Subdir: another folder of the repository.
	Subdir bool
}

// Same reports whether the lock entry was accepted for what the config says.
func (c Changes) Same() bool { return !c.Repo && !c.Ref && !c.Subdir }

// NewSource reports whether the config names other files than the lock
// entry was accepted for: another repository or folder, which is reviewed
// as a new source. Another branch, tag or rev is the same source.
func (c Changes) NewSource() bool { return c.Repo || c.Subdir }

// Compare is what source s changed since entry e was accepted. Addresses
// are compared as the same repository (cfg.RepoID), so another form of an
// address is no change.
func Compare(e *Entry, s *cfg.Source) Changes {
	return Changes{
		Repo:   cfg.RepoID(e.Repo) != cfg.RepoID(s.Repo),
		Ref:    e.Ref != s.Ref,
		Subdir: cfg.CleanSubdir(e.Subdir) != cfg.CleanSubdir(s.Subdir),
	}
}

// Format is the lock file of entries: block YAML, sources sorted by name,
// one key per line in the order repo, branch, tag or rev, subdir, commit.
// It leaves out the entries of sources that c doesn't have as repo
// sources. An entry that isn't complete, or one read with errors, is an
// error, and nothing is written: writing a broken entry back in a clean
// form would make it used again. The caller replaces or removes it.
func Format(entries []*Entry, c *cfg.Config) ([]byte, error) {
	var keep []*Entry
	for _, e := range entries {
		if s := c.Source(e.Name); s != nil && s.Dir == "" {
			keep = append(keep, e)
		}
	}
	slices.SortFunc(keep, func(a, b *Entry) int { return strings.Compare(a.Name, b.Name) })
	sources := &yaml.Node{Kind: yaml.MappingNode}
	for i, e := range keep {
		if i > 0 && keep[i-1].Name == e.Name {
			return nil, fmt.Errorf("lock entry %s is there twice", e.Name)
		}
		if err := check(e); err != nil {
			return nil, fmt.Errorf("lock entry %s: %w", e.Name, err)
		}
		m := &yaml.Node{Kind: yaml.MappingNode}
		add(m, "repo", e.Repo)
		add(m, string(e.Ref.Kind), e.Ref.Name)
		if e.Subdir != "" {
			add(m, "subdir", e.Subdir)
		}
		add(m, "commit", e.Commit)
		addNode(sources, e.Name, m)
	}
	root := &yaml.Node{Kind: yaml.MappingNode}
	add(root, "schema", string(yamlx.LockKind))
	if len(keep) > 0 {
		addNode(root, "sources", sources)
	}
	return yamlx.Encode(root)
}

// check says what makes e incomplete.
func check(e *Entry) error {
	switch {
	case cfg.CheckName(e.Name) != nil:
		return cfg.CheckName(e.Name)
	case e.Repo == "":
		return fmt.Errorf("repo is missing")
	case !slices.Contains([]cfg.RefKind{cfg.Branch, cfg.Tag, cfg.Rev}, e.Ref.Kind) || e.Ref.Name == "":
		return fmt.Errorf("branch, tag or rev is missing")
	case e.Ref.Kind == cfg.Rev && !cfg.IsFullCommitID(e.Ref.Name):
		return errors.New(cfg.CommitIDError("rev", e.Ref.Name))
	case !cfg.IsFullCommitID(e.Commit):
		return errors.New(cfg.CommitIDError("commit", e.Commit))
	case e.Broken():
		return fmt.Errorf("it has errors (%v); replace or remove it before writing the lock", e.Errs[0])
	}
	return nil
}

func add(m *yaml.Node, key, value string) { addNode(m, key, yamlx.Text(value)) }

func addNode(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content, yamlx.Text(key), value)
}
