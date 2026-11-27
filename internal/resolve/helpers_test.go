package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

var agentNames = []string{"claude-code", "codex", "cursor", "github-copilot", "opencode", "pi", "antigravity", "devin", "muse", "dsh", "hermes-agent"}

// home is a test's own home folder, with a config in it.
type home struct {
	t   *testing.T
	dir string
}

func newHome(t *testing.T) *home { return &home{t, testenv.Home(t)} }

func (h *home) path(p string) string { return filepath.Join(h.dir, filepath.FromSlash(p)) }

func (h *home) write(p, content string) string {
	testenv.Write(h.t, h.path(p), content)
	return h.path(p)
}

func (h *home) skill(p, name string) {
	testenv.Skill(h.t, h.path(p), "name: "+name+"\ndescription: The "+name+" skill.")
}

// repo makes a git repository at p with remote and returns its folder.
func (h *home) repo(p, remote string) string { return testenv.Repo(h.t, h.path(p), remote) }

func (h *home) mkdir(p string) string {
	if err := os.MkdirAll(h.path(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	return h.path(p)
}

// load writes the config, and the lock when it has entries, and loads them.
func (h *home) load(config string, lockEntries ...string) *Env {
	h.t.Helper()
	path := testenv.Config(h.t, h.dir, "schema: ctxtra/config/v1\n"+config)
	if len(lockEntries) > 0 {
		h.write(".config/ctxtra/ctxtra.lock", "schema: ctxtra/lock/v1\nsources:\n"+strings.Join(lockEntries, ""))
	}
	return Load(path, agentNames)
}

// commit is a full commit ID made of c.
func commit(c string) string { return strings.Repeat(c, 40) }

// accept puts files of commit id of repo into ctxtra's store, as source
// download would, and returns the lock entry of source name accepted for
// ref ("branch: main") and subdir.
func (h *home) accept(name, repo, ref, subdir, id string, files map[string]string) string {
	h.t.Helper()
	remote := repo
	if strings.HasPrefix(repo, "~/") {
		remote = h.path(repo[2:])
	}
	gen := source.GenDir(remote, subdir, id)
	if err := os.MkdirAll(gen, 0o755); err != nil {
		h.t.Fatal(err)
	}
	// The store has the files of subdir, by their paths from it.
	for p, content := range files {
		if rel, ok := strings.CutPrefix(p, subdir+"/"); ok || subdir == "" {
			testenv.Write(h.t, filepath.Join(gen, filepath.FromSlash(rel)), content)
		}
	}
	entry := fmt.Sprintf("  %s:\n    repo: %s\n    %s\n", name, repo, ref)
	if subdir != "" {
		entry += "    subdir: " + subdir + "\n"
	}
	return entry + "    commit: " + id + "\n"
}

// start resolves a session in dir.
func start(env *Env, dir, agent string, profiles ...string) *Result {
	return env.Resolve(Input{Dir: dir, Agent: agent, Profiles: profile.Active{Names: profiles}})
}

// applied are the bindings that applied.
func applied(res *Result) []string {
	var out []string
	for _, t := range res.Trace {
		if t.Matched {
			out = append(out, t.Origin)
		}
	}
	return out
}

func trace(t *testing.T, res *Result, binding string) Trace {
	t.Helper()
	i := slices.IndexFunc(res.Trace, func(tr Trace) bool { return tr.Origin == binding })
	if i < 0 {
		t.Fatalf("no trace of %s in %+v", binding, res.Trace)
	}
	return res.Trace[i]
}

// bases are the base names of hits.
func bases(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, filepath.Base(h.Path))
	}
	return out
}

func skillNames(res *Result) []string {
	var out []string
	for _, h := range res.Skills {
		out = append(out, h.Name)
	}
	return out
}

func served(res *Result) []string {
	var out []string
	for _, s := range res.MCPServers {
		out = append(out, s.Name)
	}
	return out
}

func eq(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: %q, want %q", what, got, want)
	}
}

// errorsOf are the result's errors of mistake m.
func errorsOf(res *Result, m yamlx.Mistake) []*catalog.Error {
	var out []*catalog.Error
	for _, e := range res.Errors {
		if e.Err.Mistake == m {
			out = append(out, e)
		}
	}
	return out
}

// oneError fails unless the result has exactly one error and it says each of
// want.
func oneError(t *testing.T, res *Result, want ...string) *catalog.Error {
	t.Helper()
	if len(res.Errors) != 1 {
		t.Fatalf("errors %v, want one", res.Errors)
	}
	for _, w := range want {
		if !strings.Contains(res.Errors[0].Error(), w) {
			t.Errorf("error %q doesn't say %q", res.Errors[0], w)
		}
	}
	return res.Errors[0]
}

func noErrors(t *testing.T, res *Result) {
	t.Helper()
	for _, e := range res.Errors {
		t.Errorf("error: %v", e)
	}
}
