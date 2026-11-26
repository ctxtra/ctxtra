package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/edit"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/term"
	"github.com/ctxtra/ctxtra/internal/trust"
	"github.com/ctxtra/ctxtra/internal/update"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// sources implements the commands that change the config: sources,
// bindings, MCP servers.
type sources struct {
	IO
	ctx    context.Context
	files  edit.Files
	file   string // the config
	in     *bufio.Reader
	full   bool         // source update --full: show every changed file in full
	prompt bool         // source update may ask whether to accept a commit
	diags  []diagnostic // content warnings, for source update --dry-run --json
}

func newSources(stdio IO) *sources {
	files := configFiles()
	return &sources{IO: stdio, ctx: context.Background(), files: files, file: files.Config, in: bufio.NewReader(stdio.In)}
}

// configFiles are the config and the lock, written under one lock.
func configFiles() edit.Files { return edit.Default(config.Options{Agents: registry.Names()}) }

// loadEnv reads the config and its lock the way sessions do.
func loadEnv() *resolve.Env { return resolve.Load(paths.ConfigPath(), registry.Names()) }

// readEnv is loadEnv for a command: a config or lock that can't be used
// as a whole fails it, with resolve.FatalError, before it does anything.
func readEnv() (*resolve.Env, error) {
	env := loadEnv()
	return env, env.Fatal()
}

// failEnv reports err, the error of readEnv or of a command, and returns
// the exit code: in JSON a fatal file is a document of its diagnostics.
func failEnv(stdio IO, cmd string, asJSON bool, err error) int {
	var fe *resolve.FatalError
	switch {
	case asJSON && errors.As(err, &fe):
		writeJSON(stdio.Out, nil, diagnostics("error", fe.Msgs))
	case errors.As(err, &fe):
		for _, m := range fe.Msgs {
			fmt.Fprintf(term.Writer{W: stdio.Err}, "ctxtra %s: %s; fix it first\n", cmd, m)
		}
	default:
		fmt.Fprintf(stdio.Err, "ctxtra %s: %v\n", cmd, err)
	}
	return 1
}

// load is the config for a command that changes it or its sources.
func (s *sources) load() (*resolve.Env, error) {
	env := resolve.Load(s.file, registry.Names())
	return env, env.Fatal()
}

// write changes the config and the lock, and says so when it creates the
// config.
func (s *sources) write(fn func(tx *edit.Tx) error) error {
	return s.writeCommit(fn, func(write func() error) error { return write() })
}

// writeCommit is write that hands the write to commit; see
// edit.Files.RunCommit.
func (s *sources) writeCommit(fn func(tx *edit.Tx) error, commit func(write func() error) error) error {
	created := s.files.Created()
	if err := s.files.RunCommit(fn, commit); err != nil {
		return err
	}
	if created && !s.files.Created() {
		term.Fprintf(s.Out, term.Green, "created %s\n", paths.Tilde(s.file))
	}
	return nil
}

func (s *sources) fail(cmd string, err error) int {
	fmt.Fprintf(s.Err, "ctxtra %s: %v\n", cmd, err)
	return 1
}

// result is the RunE result of a command that ended with err.
func (s *sources) result(cmd string, err error) error {
	if err != nil {
		return exitCode(s.fail(cmd, err))
	}
	return nil
}

// addOptions are the options of source add.
type addOptions struct {
	repo, dir, name, branch string
	dryRun                  bool
	checkUpdates            bool // --check-updates
}

var reRepoName = regexp.MustCompile(`([^/:]+?)(\.git)?/?$`)

// defaultName is a source name from the last part of a URL or folder.
func defaultName(s string) string {
	m := reRepoName.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return strings.TrimLeft(m[1], ".-_0123456789")
}

// sourceName is the name of a new source: o.name, or one from what.
func sourceName(env *resolve.Env, o addOptions, what string) (string, error) {
	name := o.name
	if name == "" {
		name = defaultName(what)
	}
	if err := config.CheckName(name); err != nil {
		return "", fmt.Errorf("%v; choose a name with --name", err)
	}
	if old := env.Config.Source(name); old != nil {
		return "", fmt.Errorf("a source named %s already exists (%s); choose another name with --name", name, describeEntry(old))
	}
	return name, nil
}

// describeEntry is a source as the config writes it.
func describeEntry(s *config.Source) string {
	if s.Dir != "" {
		return "dir " + s.Dir
	}
	d := "repo " + s.Repo
	if s.Ref.Name != "" {
		d += ", " + s.Ref.String()
	}
	return d
}

// node is a config entry of text values, written in the order of kv.
func node(kv ...[2]string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, p := range kv {
		n.Content = append(n.Content, yamlx.Text(p[0]), yamlx.Text(p[1]))
	}
	return n
}

// isLocal reports whether the argument of --repo is a path on this
// machine: a URL has a scheme or a user@host: form.
func isLocal(repo string) bool {
	return strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "~") || strings.HasPrefix(repo, ".") || !strings.Contains(repo, ":")
}

// add is source add.
func (s *sources) add(o addOptions) error {
	if o.dir != "" {
		return s.addDir(o)
	}
	return s.addRepo(o)
}

// addRepo adds a git repository read at accepted commits: a remote, or a
// clone on this machine, whose checked-out branch it follows.
func (s *sources) addRepo(o addOptions) error {
	written, remote, branch := o.repo, o.repo, o.branch
	if isLocal(o.repo) {
		wd, _ := os.Getwd()
		dir := paths.Resolve(wd, o.repo)
		c, err := source.InspectClone(s.ctx, dir)
		if err != nil {
			return err
		}
		if branch == "" {
			if c.Current == "" {
				return fmt.Errorf("%s has no branch checked out; choose the branch to follow with --branch", paths.Tilde(c.Top))
			}
			branch = c.Current
		}
		written, remote = paths.Tilde(c.Top), c.Top
	} else if err := source.CheckURL(o.repo); err != nil {
		return err
	}
	if branch != "" {
		if err := source.CheckBranch(branch); err != nil {
			return err
		}
	}
	env, err := s.load()
	if err != nil {
		return err
	}
	r := source.Open(remote)
	if branch == "" {
		if o.dryRun {
			branch = "its default branch"
		} else if branch, err = r.DefaultBranch(s.ctx); err != nil {
			return err
		}
	}
	id := config.RepoID(written)
	for _, src := range env.Config.Sources {
		if src.Repo != "" && config.RepoID(src.Repo) == id && src.Ref == (config.Ref{Kind: config.Branch, Name: branch}) && src.Subdir == "" {
			fmt.Fprintf(s.Out, "%s, branch %s, is the source %s already\n", written, branch, src.Name)
			return nil
		}
	}
	name, err := sourceName(env, o, written)
	if err != nil {
		return err
	}
	if o.dryRun {
		fmt.Fprintf(s.Out, "would add source %s: repo %s, branch %s, at the commit fetched then (not fetched now)\nnothing changed (--dry-run)\n", name, written, branch)
		return nil
	}
	started := time.Now()
	commit, err := r.FetchBranch(s.ctx, branch)
	if err != nil {
		return err
	}
	gen, err := r.Materialize(s.ctx, commit)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "fetched %s: %s at %s (%s)\n", name, branch, source.Short(commit), written)
	kv := [][2]string{{"repo", written}, {"branch", branch}}
	if !o.checkUpdates {
		kv = append(kv, [2]string{"check-updates", "false"})
	}
	entry := &lock.Entry{Name: name, Repo: written, Ref: config.Ref{Kind: config.Branch, Name: branch}, Commit: commit}
	var added bool
	err = s.write(func(tx *edit.Tx) error {
		n := node(kv...)
		if !o.checkUpdates {
			n.Content[len(n.Content)-1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
		}
		added, err = tx.Config.Add(config.Sources, name, n, config.Place{})
		if added {
			tx.SetLockEntry(entry)
		}
		return err
	})
	if err != nil {
		return err
	}
	if !added {
		fmt.Fprintf(s.Out, "the source %s is in the config already\n", name)
		return nil
	}
	s.checked(remote, branch, commit, started)
	fmt.Fprintf(s.Out, "added source %s: new commits apply after you accept them in ctxtra source update %s\n", name, name)
	after := loadEnv()
	s.mcpDefinitionWarnings(name, nil, after.Catalog)
	s.showExecutables(r, commit)
	s.checkAdded(after, name, gen)
	s.listFiles(gen)
	s.checkHint(name, o.checkUpdates, !isLocal(o.repo))
	fmt.Fprintf(s.Out, "use its files in a binding as %s:PATH\n", name)
	return nil
}

// addDir adds a folder read live.
func (s *sources) addDir(o addOptions) error {
	wd, _ := os.Getwd()
	dir := paths.Resolve(wd, o.dir)
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a folder", paths.Tilde(dir))
	}
	env, err := s.load()
	if err != nil {
		return err
	}
	for _, src := range env.Config.Sources {
		if src.Dir != "" && sameFile(paths.Expand(src.Dir), dir) {
			fmt.Fprintf(s.Out, "%s is the source %s already\n", paths.Tilde(dir), src.Name)
			return nil
		}
	}
	name, err := sourceName(env, o, dir)
	if err != nil {
		return err
	}
	_, remotes, rerr := trust.Remotes(paths.Real(dir))
	if o.dryRun {
		fmt.Fprintf(s.Out, "would add source %s: dir %s, read live\nnothing changed (--dry-run)\n", name, paths.Tilde(dir))
		return nil
	}
	var added bool
	err = s.write(func(tx *edit.Tx) error {
		added, err = tx.Config.Add(config.Sources, name, node([2]string{"dir", paths.Tilde(dir)}), config.Place{})
		return err
	})
	if err != nil {
		return err
	}
	if !added {
		fmt.Fprintf(s.Out, "the source %s is in the config already\n", name)
		return nil
	}
	fmt.Fprintf(s.Out, "added source %s: dir %s, read live; changes apply to new sessions right away\n", name, paths.Tilde(dir))
	switch {
	case rerr != nil:
		fmt.Fprintf(s.Out, "warning: the remotes of its repository can't be read (%v); it is not read until they can be\n", rerr)
	case len(remotes) > 0:
		fmt.Fprintf(s.Out, "its repository has the remotes %s, which others can push to; to read it live, run ctxtra source trust %s\n",
			strings.Join(remotes, ", "), name)
	}
	s.listFiles(dir)
	fmt.Fprintf(s.Out, "use its files in a binding as %s:PATH\n", name)
	return nil
}

// Bounds of the file list of a new source without a manifest.
const (
	maxListedFiles = 50
	maxListVisits  = 10000
)

// listFiles lists the files of a new source's folder top when it has no
// manifest, which would say what it offers: the first maxListedFiles, in
// name order, without .git, symlinks and what they lead to.
func (s *sources) listFiles(top string) {
	if _, err := os.Lstat(filepath.Join(top, manifest.FileName)); err == nil {
		return
	}
	var files []string
	more, visits := false, 0
	_ = filepath.WalkDir(top, func(p string, d fs.DirEntry, err error) error {
		visits++
		switch {
		case visits > maxListVisits:
			more = true
			return filepath.SkipAll
		case err != nil:
			return nil
		case d.IsDir() && d.Name() == ".git":
			return filepath.SkipDir
		case !d.Type().IsRegular():
			return nil
		case len(files) == maxListedFiles:
			more = true
			return filepath.SkipAll
		}
		files = append(files, filepath.ToSlash(must(filepath.Rel(top, p))))
		return nil
	})
	if len(files) == 0 {
		fmt.Fprintf(s.Out, "it has no files and no %s\n", manifest.FileName)
		return
	}
	fmt.Fprintf(s.Out, "it has no %s; its files:\n", manifest.FileName)
	for _, f := range files {
		fmt.Fprintf(s.Out, "  %s\n", source.Sanitize(f))
	}
	if more {
		fmt.Fprintf(s.Out, "  … (only the first %d are listed)\n", len(files))
	}
}

func sameFile(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

// checkHint says how sessions learn about new commits of source name.
func (s *sources) checkHint(name string, on, remote bool) {
	if !on {
		fmt.Fprintf(s.Out, "update checks are off for %s (check-updates: false)\n", name)
		return
	}
	fmt.Fprintf(s.Out, "sessions check %s for new commits at most once an hour and tell you when there are some\n", name)
	if remote {
		fmt.Fprintln(s.Out, "checks use your git credentials; credential helpers or hardware keys may ask for approval or a touch. To turn checks off, set check-updates: false on this source")
	}
}

// showExecutables lists the executable files of a commit, and what the
// store leaves out.
func (s *sources) showExecutables(r *source.Repo, commit string) {
	entries, skipped, err := r.Tree(s.ctx, commit)
	if err != nil {
		return
	}
	var exec []string
	for _, e := range entries {
		if e.Exec {
			exec = append(exec, e.Path)
		}
	}
	if len(exec) > 0 {
		fmt.Fprintf(s.Out, "executable files: %s\n", source.Sanitize(strings.Join(exec, ", ")))
	}
	for _, sk := range skipped {
		fmt.Fprintf(s.Out, "left out (%s): %s\n", sk.Kind, source.Sanitize(sk.Path))
	}
}

// updateResult is one source in the output of source update --dry-run
// --json.
type updateResult struct {
	Name     string `json:"name"`
	Accepted string `json:"accepted,omitempty"`
	Latest   string `json:"latest,omitempty"`
	Pending  bool   `json:"pending"`
	Error    string `json:"error,omitempty"`
}

// repoSources are the repo sources names, or all of them; cmd names the
// command in errors.
func repoSources(env *resolve.Env, names []string) ([]*catalog.Source, error) {
	var out []*catalog.Source
	for _, n := range names {
		src := env.Catalog.Source(n)
		switch {
		case src == nil:
			return nil, fmt.Errorf("no source named %s", n)
		case src.Live():
			return nil, fmt.Errorf("%s is a dir source: it is read live and has no commits", n)
		}
		out = append(out, src)
	}
	if len(names) == 0 {
		for _, src := range env.Catalog.Sources() {
			if !src.Live() && src.Repo != "" {
				out = append(out, src)
			}
		}
	}
	return out, nil
}

// update is source update for the repo sources names, or all of them.
func (s *sources) update(names []string, dryRun, asJSON bool) int {
	const cmd = "source update"
	env, err := s.load()
	if err != nil {
		return s.fail(cmd, err)
	}
	todo, err := repoSources(env, names)
	if err != nil {
		return s.fail(cmd, err)
	}
	if !dryRun && len(todo) > 0 && !s.prompt {
		return s.fail(cmd, errors.New("accepting a new commit needs a terminal (or leave out --no-input); to only look for new commits: ctxtra source update --dry-run"))
	}
	out := s.Out
	if asJSON {
		s.Out = io.Discard
	}
	results := []updateResult{}
	if len(todo) == 0 {
		fmt.Fprintln(s.Out, "no repo sources")
	}
	n := 0
	for _, src := range todo {
		res := updateResult{Name: src.Name}
		if e := env.Lock.Entry(src.Name); e != nil {
			res.Accepted = e.Commit
		}
		latest, err := s.sync(env, src, dryRun)
		res.Latest, res.Pending = latest, latest != "" && latest != res.Accepted
		if err != nil {
			fmt.Fprintf(s.Err, "%s: %v\n", src.Name, err)
			res.Error = err.Error()
			n = 1
		}
		results = append(results, res)
	}
	if len(todo) > 0 {
		s.cleanStore()
	}
	if asJSON {
		writeJSON(out, results, s.diags)
	}
	return n
}

// sync fetches the branch of one source, reviews its new commit against
// the accepted one, and, unless dryRun, asks whether to accept it. It
// returns the newest commit it found.
func (s *sources) sync(env *resolve.Env, cs *catalog.Source, dryRun bool) (string, error) {
	src := cs.Source
	switch {
	case cs.State == catalog.Broken:
		return "", errors.New("the source has errors; see ctxtra doctor")
	case cs.State == catalog.NotHere:
		fmt.Fprintf(s.Out, "%s: %s; skipped\n", src.Name, cs.Why)
		return "", nil
	case src.Ref.Kind != config.Branch:
		return "", fmt.Errorf("%s is not implemented yet; follow a branch", src.Ref.Kind)
	case src.Subdir != "":
		return "", errors.New("subdir is not implemented yet")
	}
	remote := resolve.RemoteOf(src)
	r := source.Open(remote)
	was := env.Lock.Entry(src.Name)
	old := ""
	if was != nil && !lock.Compare(was, src).NewSource() {
		old = was.Commit
		if !source.Has(resolve.RemoteOf(&config.Source{Repo: was.Repo}), old) {
			if err := source.Open(resolve.RemoteOf(&config.Source{Repo: was.Repo})).FetchCommit(s.ctx, old); err != nil {
				return "", err
			}
			if _, err := source.Open(resolve.RemoteOf(&config.Source{Repo: was.Repo})).Materialize(s.ctx, old); err != nil {
				return "", err
			}
			fmt.Fprintf(s.Out, "%s: fetched %s (the commit in %s)\n", src.Name, source.Short(old), lock.FileName)
		}
	}
	branch := src.Ref.Name
	started := time.Now()
	commit, err := r.FetchBranch(s.ctx, branch)
	if err != nil {
		return "", err
	}
	s.checked(remote, branch, commit, started)
	same := was != nil && lock.Compare(was, src).Same()
	if same && commit == was.Commit {
		if !dryRun {
			// The branch is back at the accepted commit: nothing is dismissed.
			s.record(remote, branch, func(e *state.Source) { e.Dismissed = "" })
		}
		fmt.Fprintf(s.Out, "%s: up to date (%s at %s)\n", src.Name, branch, source.Short(commit))
		return commit, nil
	}
	// The new commit goes to the store before review: a commit over the
	// limits fails here, and the review reads its files.
	if _, err := r.Materialize(s.ctx, commit); err != nil {
		return "", err
	}
	if old == "" {
		fmt.Fprintf(s.Out, "%s: no accepted commit for it yet; %s is at %s (%s)\n", src.Name, branch, source.Short(commit), src.Repo)
	} else {
		fmt.Fprintf(s.Out, "%s: %s -> %s (%s, %s)\n", src.Name, source.Short(old), source.Short(commit), branch, src.Repo)
		if !r.IsAncestor(s.ctx, old, commit) {
			fmt.Fprintf(s.Out, "warning: %s doesn't continue %s: the branch was force-pushed, or %s was never pushed\n",
				source.Short(commit), source.Short(old), source.Short(old))
		}
	}
	if err := s.review(env, r, cs, old, commit); err != nil {
		return "", err
	}
	if dryRun {
		fmt.Fprintf(s.Out, "%s: not accepted (--dry-run); to accept it, run in a terminal: ctxtra source update %s\n", src.Name, src.Name)
		return commit, nil
	}
	q := "Accept? [y/N] "
	if old == "" {
		q = fmt.Sprintf("Accept %s as the first version of %s? [y/N] ", source.Short(commit), src.Name)
	}
	ok, err := s.ask(q)
	if err != nil {
		return commit, err
	}
	if !ok {
		if old == "" {
			fmt.Fprintf(s.Out, "not accepted; %s has no accepted commit yet\n", src.Name)
			return commit, nil
		}
		fmt.Fprintf(s.Out, "not accepted; you keep %s\n", source.Short(old))
		s.record(remote, branch, func(e *state.Source) { e.Dismissed = commit })
		return commit, nil
	}
	accepted := &lock.Entry{Name: src.Name, Repo: src.Repo, Ref: src.Ref, Subdir: src.Subdir, Commit: commit}
	if err := s.files.Accept(accepted, was); err != nil {
		return commit, err
	}
	s.record(remote, branch, func(e *state.Source) { e.Dismissed = "" })
	fmt.Fprintf(s.Out, "accepted %s %s (%s updated)\n", src.Name, source.Short(commit), lock.FileName)
	return commit, nil
}

// ask asks a yes/no question on stderr; the answer is no unless it is y or
// yes. Input that ends before the end of a line decides nothing: an error.
func (s *sources) ask(q string) (bool, error) {
	fmt.Fprint(s.Err, q)
	answer, err := s.in.ReadString('\n')
	if err != nil {
		fmt.Fprintln(s.Err)
		return false, errors.New("no answer; nothing accepted or dismissed")
	}
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes", nil
}

// record changes the local state of a source's branch. The state only
// feeds notices, so an error is a warning.
func (s *sources) record(remote, branch string, change func(*state.Source)) {
	if err := state.Update(source.ID(remote), remote, branch, change); err != nil {
		fmt.Fprintf(s.Err, "warning: can't write %s: %v\n", paths.Tilde(state.Path()), err)
	}
}

// checked records a fetch of the branch that worked, and the commit it
// found. A fetch that started at started counts as an update check too: the
// next one waits, and an update check that started later keeps its result.
func (s *sources) checked(remote, branch, head string, started time.Time) {
	s.record(remote, branch, func(e *state.Source) {
		e.LastCheck = time.Now()
		if update.Record(&e.Check, started, nil) {
			e.Head = head
		}
	})
}

// download is source download: the commits of the lock that this machine
// doesn't have, exactly, with no review and no change to the lock.
func (s *sources) download(names []string) int {
	const cmd = "source download"
	env, err := s.load()
	if err != nil {
		return s.fail(cmd, err)
	}
	todo, err := repoSources(env, names)
	if err != nil {
		return s.fail(cmd, err)
	}
	if len(todo) == 0 {
		fmt.Fprintln(s.Out, "no repo sources")
	}
	n := 0
	for _, cs := range todo {
		e := env.Lock.Entry(cs.Name)
		switch {
		case cs.State == catalog.NotHere:
			fmt.Fprintf(s.Out, "%s: %s; skipped\n", cs.Name, cs.Why)
			continue
		case cs.State == catalog.Broken:
			fmt.Fprintf(s.Err, "%s: the source has errors; see ctxtra doctor\n", cs.Name)
			n = 1
			continue
		case !env.Lock.Readable():
			fmt.Fprintf(s.Err, "%s: %s can't be read; see ctxtra doctor\n", cs.Name, lock.FileName)
			n = 1
			continue
		case e == nil:
			fmt.Fprintf(s.Err, "%s: %s has no accepted commit of it; run `ctxtra source update %s`\n", cs.Name, lock.FileName, cs.Name)
			n = 1
			continue
		case !lock.Compare(e, cs.Source).Same():
			fmt.Fprintf(s.Err, "%s: %s has a commit accepted for another address, branch or subdir; run `ctxtra source update %s` to review it\n",
				cs.Name, lock.FileName, cs.Name)
			n = 1
			continue
		}
		// The lock's address names the commit's folder in the store; the
		// config's equivalent address is the one that works for fetching.
		stored := resolve.RemoteOf(&config.Source{Repo: e.Repo})
		if source.Has(stored, e.Commit) {
			fmt.Fprintf(s.Out, "%s: %s is downloaded\n", cs.Name, source.Short(e.Commit))
			continue
		}
		r := source.Open(resolve.RemoteOf(cs.Source))
		err := r.FetchCommit(s.ctx, e.Commit)
		if err == nil {
			_, err = r.MaterializeAs(s.ctx, e.Commit, stored)
		}
		if err != nil {
			fmt.Fprintf(s.Err, "%s: %v\n", cs.Name, err)
			n = 1
			continue
		}
		fmt.Fprintf(s.Out, "%s: downloaded %s (the commit in %s)\n", cs.Name, source.Short(e.Commit), lock.FileName)
	}
	return n
}

// trust is source trust: it records the remotes of a dir source's
// repository, so that its folder is read live while they stay the same.
func (s *sources) trust(name string, dryRun bool) error {
	env, err := s.load()
	if err != nil {
		return err
	}
	src := env.Config.Source(name)
	switch {
	case src == nil:
		return fmt.Errorf("no source named %s", name)
	case src.Dir == "":
		return fmt.Errorf("%s is a repo source: its commits are reviewed with ctxtra source update; only a dir source is trusted", name)
	case src.Broken():
		return fmt.Errorf("the source %s has errors; see ctxtra doctor", name)
	}
	dir := paths.Expand(src.Dir)
	if st, err := os.Stat(dir); err != nil {
		return err
	} else if !st.IsDir() {
		return fmt.Errorf("%s is not a folder", src.Dir)
	}
	real := paths.Real(dir)
	if why := env.Catalog.Route(name); why != "" {
		return errors.New(why)
	}
	repo, remotes, err := trust.Remotes(real)
	switch {
	case err != nil:
		return fmt.Errorf("the remotes of %s can't be read: %w", paths.Tilde(repo), err)
	case len(remotes) == 0:
		fmt.Fprintf(s.Out, "%s has no remotes: it is read live without trust\n", src.Dir)
		return nil
	case dryRun:
		fmt.Fprintf(s.Out, "would trust %s with the remotes %s\nnothing changed (--dry-run)\n", name, strings.Join(remotes, ", "))
		return nil
	}
	if err := state.Trust(real, remotes); err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "trusted %s: %s is read live while its repository has the remotes %s; when they change, ctxtra stops reading it until you trust it again\n",
		name, src.Dir, strings.Join(remotes, ", "))
	return nil
}

// removeSource is source remove: the source and its lock entry go; entries
// that use it are left for the user, with a warning.
func (s *sources) removeSource(name string, dryRun bool) error {
	env, err := s.load()
	if err != nil {
		return err
	}
	if env.Config.Source(name) == nil {
		return fmt.Errorf("no source named %s in %s", name, paths.Tilde(s.file))
	}
	verb := "removed"
	if dryRun {
		verb = "would remove"
	} else if err := s.write(func(tx *edit.Tx) error { return tx.Config.Remove(config.Sources, name) }); err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "%s source %s from %s\n", verb, name, paths.Tilde(s.file))
	for _, b := range env.Config.Bindings {
		for _, e := range slices.Concat(b.Instructions, b.Skills, b.MCPServers, b.Collections) {
			if e.InSource && e.Source == name {
				fmt.Fprintf(s.Err, "warning: binding %s still uses %s; that entry is an error until you remove it\n", b.Name, e.Text)
			}
		}
	}
	if dryRun {
		fmt.Fprintln(s.Out, "nothing changed (--dry-run)")
		return nil
	}
	for _, notice := range mcpOrphans(loadEnv()) {
		if strings.HasPrefix(notice, "MCP credentials for "+name+":") {
			fmt.Fprintln(s.Out, strings.Replace(notice, "are not declared in this config.", "remain.", 1))
		}
	}
	s.cleanStore()
	return nil
}

// cleanStore deletes the commits no session used for a while, except the
// accepted commits in the lock. It holds the config's lock, so that no
// commit is accepted meanwhile.
func (s *sources) cleanStore() {
	_ = s.files.Locked(func() error {
		env := resolve.Load(s.file, registry.Names())
		if env.Fatal() != nil || !env.Lock.Readable() {
			// A config or lock that can't be read may name anything: keep it all.
			fmt.Fprintf(s.Err, "warning: %s or %s can't be read; the store is not cleaned\n", paths.Tilde(s.file), lock.FileName)
			return nil
		}
		keep := map[string]bool{}
		for _, e := range env.Lock.Entries {
			keep[catalog.CommitDir(e)] = true
		}
		if n := source.Clean(keep, genAge); n > 0 {
			fmt.Fprintf(s.Out, "deleted %d old commits from ctxtra's store\n", n)
		}
		return nil
	})
}

// genAge is how long a commit stays in the store after it was last used.
const genAge = 30 * 24 * time.Hour

// sourceInfo is a source in the output of source list and show.
type sourceInfo struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Location string `json:"location"`
	Branch   string `json:"branch,omitempty"`
	// State is active, inactive, not on this machine or broken; Why says
	// why it isn't active.
	State      string `json:"state"`
	Why        string `json:"why,omitempty"`
	Commit     string `json:"commit,omitempty"`
	Downloaded *bool  `json:"downloaded,omitempty"`
	LastCheck  string `json:"last_check,omitempty"` // the last fetch that worked
	Dismissed  string `json:"dismissed,omitempty"`
	// The update checks of a repo source.
	CheckUpdates *bool  `json:"check_updates,omitempty"`
	Head         string `json:"head,omitempty"`        // the commit the last check or fetch found
	Checked      string `json:"checked,omitempty"`     // when
	CheckError   string `json:"check_error,omitempty"` // why the last check failed
}

var stateNames = map[catalog.State]string{catalog.Active: "active", catalog.Inactive: "inactive", catalog.NotHere: "not on this machine", catalog.Broken: "broken"}

func info(env *resolve.Env, cs *catalog.Source) sourceInfo {
	src := cs.Source
	i := sourceInfo{Name: src.Name, State: stateNames[cs.State], Why: cs.Why}
	if src.Dir != "" {
		i.Kind, i.Location = "dir", src.Dir
		return i
	}
	i.Kind, i.Location, i.Branch = "repo", src.Repo, src.Ref.Name
	if e := env.Lock.Entry(src.Name); e != nil {
		i.Commit = e.Commit
		in := source.Has(resolve.RemoteOf(&config.Source{Repo: e.Repo}), e.Commit)
		i.Downloaded = &in
	}
	remote := resolve.RemoteOf(src)
	e := state.Load().Get(source.ID(remote), remote, src.Ref.Name)
	if !e.LastCheck.IsZero() {
		i.LastCheck = e.LastCheck.Format(time.RFC3339)
	}
	if e.Dismissed != "" && e.Dismissed != i.Commit {
		i.Dismissed = e.Dismissed
	}
	check := src.CheckUpdates == nil || *src.CheckUpdates
	i.CheckUpdates = &check
	i.Head, i.CheckError = e.Head, e.Error
	if !e.Checked.IsZero() {
		i.Checked = e.Checked.Format(time.RFC3339)
	}
	return i
}

// list is source list.
func (s *sources) list(asJSON bool) int {
	env, err := s.load()
	if err != nil {
		return failEnv(s.IO, "source list", asJSON, err)
	}
	if asJSON {
		out := []sourceInfo{}
		for _, cs := range env.Catalog.Sources() {
			out = append(out, info(env, cs))
		}
		writeJSON(s.Out, out, nil)
		return 0
	}
	if len(env.Catalog.Sources()) == 0 {
		fmt.Fprintln(s.Out, "no sources (add one with ctxtra source add)")
	}
	for _, cs := range env.Catalog.Sources() {
		fmt.Fprintf(s.Out, "%s: %s\n", cs.Name, describeSource(env, cs))
	}
	return 0
}

// show is source show.
func (s *sources) show(name string, asJSON bool) int {
	env, err := s.load()
	if err != nil {
		return failEnv(s.IO, "source show", asJSON, err)
	}
	cs := env.Catalog.Source(name)
	if cs == nil {
		return s.fail("source show", errors.New("no source named "+name))
	}
	if asJSON {
		writeJSON(s.Out, info(env, cs), nil)
		return 0
	}
	fmt.Fprintf(s.Out, "source %s: %s\n", name, describeSource(env, cs))
	return 0
}

// describeSource is one line about a source and its state.
func describeSource(env *resolve.Env, cs *catalog.Source) string {
	src := cs.Source
	switch cs.State {
	case catalog.NotHere:
		return describeEntry(src) + ": " + cs.Why
	case catalog.Broken:
		return describeEntry(src) + ": " + cs.Why + " (see ctxtra doctor)"
	}
	if src.Dir != "" {
		if cs.State != catalog.Active {
			return "dir " + src.Dir + ": NOT READ: " + cs.Why
		}
		return "dir " + src.Dir + ": read live, changes apply without review"
	}
	d := fmt.Sprintf("repo %s, branch %s, ", src.Repo, src.Ref.Name)
	remote := resolve.RemoteOf(src)
	e := state.Load().Get(source.ID(remote), remote, src.Ref.Name)
	check := "; update checks off (remove check-updates: false to turn them on)"
	accepted := ""
	if le := env.Lock.Entry(src.Name); le != nil {
		accepted = le.Commit
	}
	if src.CheckUpdates == nil || *src.CheckUpdates {
		check = "; " + checkStatus(src.Name, accepted, e)
	}
	if cs.State != catalog.Active {
		return d + "INACTIVE: " + cs.Why + check
	}
	d += "at " + source.Short(accepted) + " (new commits apply after you accept them in ctxtra source update)"
	if e.LastCheck.IsZero() {
		d += ", not fetched on this machine yet"
	} else {
		d += ", last fetched " + e.LastCheck.Format(timeFormat)
	}
	if e.Dismissed != "" && e.Dismissed != accepted {
		d += ", " + source.Short(e.Dismissed) + " not accepted (dismissed)"
	}
	return d + check
}

// readConfig reads the config for a dry run: a missing one is empty, a
// broken one an error.
func readConfig(file string) ([]byte, error) {
	data, err := fsutil.ReadBounded(file, config.MaxFileBytes)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}
	if _, err := config.NewEditor(data); err != nil {
		return nil, fmt.Errorf("%s: %w", paths.Tilde(file), err)
	}
	return data, nil
}
