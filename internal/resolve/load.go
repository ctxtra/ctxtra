package resolve

import (
	"os"
	"strings"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/gitrepo"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Env is what resolving reads besides the session: the config, its lock,
// and the catalog of what they make available. The hook, the MCP proxy and
// the commands that show what a session gets all load it here. An Env
// reads sources' manifests once, when first used, and is not safe for
// concurrent use.
type Env struct {
	// Path is the config file.
	Path    string
	Config  *config.Config
	Lock    *lock.Lock
	Catalog *catalog.Catalog
	// Errors are the errors of the config, the lock and the sources, which
	// every session reports.
	Errors []*catalog.Error
	// read are the files Load read, with their sizes: a session's total
	// read limit counts them.
	read map[string]int
}

// Input is the session to resolve.
type Input struct {
	// Dir is the folder the session starts in.
	Dir string
	// Repo is Dir's repository, nil outside git. When nil, it is found from
	// Dir, and only if a binding's location needs it.
	Repo *gitrepo.Repo
	// Agent is the session's agent, by the name ctxtra setup uses; "" when
	// it is not known, and then no binding with agents applies.
	Agent string
	// Profiles are the active profiles, with their origin and errors.
	Profiles profile.Active
}

// Load reads the config at path and its lock. agents are the names the
// agents condition accepts. A config that doesn't exist is empty: sessions
// get nothing from it.
func Load(path string, agents []string) *Env {
	data, err := fsutil.ReadBounded(path, config.MaxFileBytes)
	return load(path, data, err, agents)
}

// LoadData is Load with data as the config file's content, for a command
// that checks a change before it writes it.
func LoadData(path string, data []byte, agents []string) *Env {
	return load(path, data, nil, agents)
}

func load(path string, data []byte, err error, agents []string) *Env {
	env := &Env{Path: path, read: map[string]int{path: len(data)}}
	switch {
	case os.IsNotExist(err):
		env.Config = &config.Config{}
	case err != nil:
		env.Config = &config.Config{Errors: []*yamlx.Error{{Mistake: yamlx.Unreadable, Msg: err.Error()}}}
	default:
		env.Config = config.Parse(data, config.Options{Agents: agents})
	}
	for _, e := range env.Config.Errors {
		env.Errors = append(env.Errors, &catalog.Error{File: path, Err: e})
	}
	env.Lock = env.readLock(lock.Path(path))
	for _, e := range env.Lock.Errors {
		env.Errors = append(env.Errors, &catalog.Error{File: lock.Path(path), Err: e})
	}
	env.Catalog = catalog.New(env.Config, env.Lock, path)
	env.Errors = append(env.Errors, env.Catalog.Errors()...)
	return env
}

// FatalError is the errors of the config and the lock that leave out a
// whole file: invalid YAML, another schema, a file that can't be read.
type FatalError struct {
	// Msgs are the errors, each with its file and line.
	Msgs []string
}

func (e *FatalError) Error() string { return strings.Join(e.Msgs, "; ") + "; fix it first" }

// Fatal is the FatalError of the files env read, nil when they can be
// used. Commands fail with it before they do anything; sessions carry on
// without the file.
func (env *Env) Fatal() error {
	var msgs []string
	for _, e := range env.Config.Errors {
		if e.Out() == yamlx.OutFile {
			msgs = append(msgs, paths.Tilde(env.Path)+": "+e.Error())
		}
	}
	for _, e := range env.Lock.Errors {
		if e.Out() == yamlx.OutFile {
			msgs = append(msgs, paths.Tilde(lock.Path(env.Path))+": "+e.Error())
		}
	}
	if msgs == nil {
		return nil
	}
	return &FatalError{msgs}
}

// reader is a reader with a session's limits that has counted what Load
// read.
func (env *Env) reader() *fsutil.Reader {
	rd := fsutil.NewReader(config.MaxFileBytes, config.MaxSessionBytes)
	for name, n := range env.read {
		_ = rd.Count(name, n)
	}
	return rd
}

// readLock reads the lock at path. A missing lock is empty: every repo
// source is then inactive. A lock that exists is read also when the config
// has no repo source: invalid YAML in it is an error all the same.
func (env *Env) readLock(path string) *lock.Lock {
	data, err := fsutil.ReadBounded(path, config.MaxFileBytes)
	switch {
	case os.IsNotExist(err):
		return &lock.Lock{}
	case err != nil:
		return &lock.Lock{Errors: []*yamlx.Error{{Mistake: yamlx.Unreadable, Msg: err.Error()}}}
	}
	env.read[path] = len(data)
	return lock.Parse(data)
}
