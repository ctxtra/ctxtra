package resolve

import (
	"os"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/gitrepo"
	"github.com/ctxtra/ctxtra/internal/lock"
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
	// Profiles are the active profiles.
	Profiles []string
}

// Load reads the config at path and its lock. agents are the names the
// agents condition accepts. A config that doesn't exist is empty: sessions
// get nothing from it.
func Load(path string, agents []string) *Env {
	env := &Env{Path: path}
	data, err := fsutil.ReadBounded(path, config.MaxFileBytes)
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
	env.Lock = readLock(lock.Path(path), env.Config)
	for _, e := range env.Lock.Errors {
		env.Errors = append(env.Errors, &catalog.Error{File: lock.Path(path), Err: e})
	}
	env.Catalog = catalog.New(env.Config, env.Lock, path)
	env.Errors = append(env.Errors, env.Catalog.Errors()...)
	return env
}

// readLock reads the lock at path. A config without repo sources needs no
// lock, and a missing lock is empty: every repo source is then inactive.
func readLock(path string, c *config.Config) *lock.Lock {
	repos := false
	for _, s := range c.Sources {
		repos = repos || s.Repo != ""
	}
	if !repos {
		return &lock.Lock{}
	}
	data, err := fsutil.ReadBounded(path, config.MaxFileBytes)
	switch {
	case os.IsNotExist(err):
		return &lock.Lock{}
	case err != nil:
		return &lock.Lock{Errors: []*yamlx.Error{{Mistake: yamlx.Unreadable, Msg: err.Error()}}}
	}
	return lock.Parse(data)
}
