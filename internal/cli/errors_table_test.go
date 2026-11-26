package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/setup"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// Each case checks the error at the session boundary and in the commands
// that diagnose it, with a healthy binding beside the affected part.
func TestErrorTableReports(t *testing.T) {
	const goodBinding = "  - {name: good, instructions: [~/good.md]}\n"
	const sourceBinding = "  - {name: affected, instructions: ['team:a.md', ~/rest.md]}\n"
	const repoSource = "sources: {team: {repo: 'https://github.com/example/team.git', branch: main, check-updates: false}}\n"
	const dirSource = "sources: {team: {dir: ~/team}}\n"
	type row struct {
		name, top, bindings, raw, manifest, lock string
		prepare                                  func(*testing.T, string)
		check                                    func(*testing.T, string)
		forbidden                                []string
		want                                     []string
		files                                    []string
		profiles                                 []string
		unavailable                              bool
		whole                                    bool
		repoFiles                                bool
	}
	rows := []row{
		{name: "unreadable config", raw: "schema: ctxtra/config/v1\nbindings:\n  - {name: valid, instructions: [~/good.md]}\nunknown: [\n", want: []string{"YAML"}, whole: true},
		{name: "duplicate key", bindings: "  - name: affected\n    instructions: [~/suspect.md]\n    instructions: [~/rest.md]\n", want: []string{"instructions is there twice"}},
		{name: "unknown top-level key", top: "unknown: true\n", bindings: "  - {name: affected, instructions: [~/rest.md]}\n", want: []string{`unknown key "unknown"`}, files: []string{"rest.md"}},
		{name: "broken source", top: "sources: {team: {dir: ~/team, unknown: true}}\n", bindings: sourceBinding, want: []string{`unknown key "unknown"`}, files: []string{"rest.md"}},
		{name: "source not on this machine", top: "sources: {team: {dir: ~/missing}}\n", bindings: sourceBinding, unavailable: true, files: []string{"rest.md"}},
		{name: "missing lock entry", top: repoSource, bindings: sourceBinding, want: []string{"ctxtra.lock has no accepted commit", "ctxtra source update team"}, files: []string{"rest.md"}},
		{name: "commit not downloaded", top: repoSource, bindings: sourceBinding, lock: "schema: ctxtra/lock/v1\nsources:\n  team:\n    repo: https://github.com/example/team.git\n    branch: main\n    commit: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", want: []string{"not downloaded", "ctxtra source download"}, files: []string{"rest.md"}},
		{name: "unreadable lock", top: "sources:\n  team: {repo: 'https://github.com/example/team.git', branch: main, check-updates: false}\n  other: {repo: 'https://github.com/example/other.git', branch: main, check-updates: false}\n  live: {dir: ~/team}\n", bindings: "  - {name: affected, instructions: ['team:a.md', 'other:x.md', 'live:a.md', ~/rest.md]}\n", lock: "schema: ctxtra/lock/v1\nsources: [\n", prepare: func(t *testing.T, h string) {
			bad, err := os.ReadFile(filepath.Join(h, ".config", "ctxtra", "ctxtra.lock"))
			if err != nil {
				t.Fatal(err)
			}
			defer testenv.Write(t, filepath.Join(h, ".config", "ctxtra", "ctxtra.lock"), string(bad))
			team := lockedSource(t, "team", "https://github.com/example/team.git", map[string]string{"a.md": "LOCKED-TEAM-CONTENT"})
			other := lockedSource(t, "other", "https://github.com/example/other.git", map[string]string{"x.md": "LOCKED-OTHER-CONTENT"})
			writeLock(t, paths.ConfigPath(), team, other)
			if code, out, errOut := run("context", "files", filepath.Join(h, "work")); code != 0 || !strings.Contains(out, "/x.md") || !strings.Contains(out, source.GenDir("https://github.com/example/team.git", "", strings.Repeat("a", 40))) {
				t.Fatalf("readable lock control: exit %d\n%s%s", code, out, errOut)
			}
		}, want: []string{"ctxtra.lock", "YAML"}, files: []string{"team/a.md", "rest.md"}, forbidden: []string{"LOCKED-TEAM-CONTENT", "LOCKED-OTHER-CONTENT"}},
		{name: "broken binding", bindings: "  - {name: affected, instructions: [~/suspect.md], unknown: true}\n", want: []string{`unknown key "unknown"`}},
		{name: "duplicate binding names", bindings: "  - {name: affected, instructions: [~/suspect.md]}\n  - {name: affected, instructions: [~/rest.md]}\n", want: []string{"2 bindings are named affected"}},
		{name: "broken content entry", bindings: "  - {name: affected, instructions: [~/missing.md, ~/rest.md]}\n", want: []string{"missing.md is not a file"}, files: []string{"rest.md"}},
		{name: "empty condition", bindings: "  - {name: affected, profiles: [], instructions: [~/suspect.md]}\n", want: []string{"profiles", "empty"}},
		{name: "broken positive location", bindings: "  - {name: affected, location: {dirs: [relative]}, instructions: [~/suspect.md]}\n", want: []string{"relative", "absolute"}},
		{name: "broken excluded location", bindings: "  - {name: affected, location: {dirs: [~/work, '!relative']}, instructions: [~/suspect.md]}\n", want: []string{"relative is a relative folder", "absolute"}},
		{name: "untrusted file", bindings: "  - {name: affected, instructions: [~/foreign/AGENTS.md, ~/rest.md]}\n", prepare: func(t *testing.T, h string) {
			testenv.Repo(t, filepath.Join(h, "foreign"), "https://github.com/example/foreign.git")
		}, want: []string{"source trust"}, files: []string{"rest.md"}},
		{name: "unreadable manifest", top: dirSource, manifest: "schema: ctxtra/source/v1\nbindings: []\ncollections: {offered: {instructions: [b.md]}}\n", bindings: "  - {name: affected, collections: ['team:offered'], instructions: ['team:a.md', ~/rest.md]}\n", want: []string{"a manifest has no bindings"}, files: []string{"team/a.md", "rest.md"}},
		{name: "broken manifest entry", top: dirSource, manifest: "schema: ctxtra/source/v1\ncollections: {offered: {instructions: [/outside.md, a.md]}}\n", bindings: "  - {name: affected, collections: ['team:offered']}\n", want: []string{"relative to the top of this source"}, files: []string{"team/a.md"}},
		{name: "unknown collection key", top: "collections: {offered: {instructions: [~/suspect.md], unknown: true}, own: {instructions: [~/rest.md]}}\n", bindings: "  - {name: affected, collections: [offered, own]}\n", want: []string{`unknown key "unknown"`}, files: []string{"rest.md"}},
		{name: "collection cycle", top: "collections:\n  a: {collections: [b], instructions: [~/suspect.md]}\n  b: {collections: [a], instructions: [~/suspect.md]}\n  own: {collections: [a], instructions: [~/rest.md]}\n", bindings: "  - {name: affected, collections: [own, b]}\n", want: []string{"collections a and b nest each other"}, files: []string{"rest.md"}},
		{name: "unapproved source server", top: repoSource, bindings: "  - {name: affected, instructions: [~/rest.md], mcp-servers: ['team:deploy', healthy]}\n", prepare: func(t *testing.T, h string) {
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			definition := func(name string) string {
				command, _ := json.Marshal([]string{bin, "-pidfile", filepath.Join(h, name+"-started")})
				return "{command: " + string(command) + ", env: {CTXTRA_TEST_MCPSERVER: '1'}}"
			}
			entry := lockedSource(t, "team", "https://github.com/example/team.git", map[string]string{
				"ctxtra-source.yaml": "schema: ctxtra/source/v1\nmcp-servers: {deploy: " + definition("rejected") + "}\n",
			})
			writeLock(t, paths.ConfigPath(), entry)
			conf, err := os.ReadFile(paths.ConfigPath())
			if err != nil {
				t.Fatal(err)
			}
			testenv.Write(t, paths.ConfigPath(), string(conf)+"mcp-servers: {healthy: "+definition("healthy")+"}\n")
		}, check: checkErrorTableApproval, want: []string{"ctxtra mcp approve team:deploy"}, files: []string{"rest.md"}},
		{name: "over capacity", top: "max-ctx-chars: 1000\n", bindings: "  - {name: affected, instructions: [~/suspect.md]}\n", prepare: func(t *testing.T, h string) {
			testenv.Write(t, filepath.Join(h, "suspect.md"), "MARKER-suspect.md\n"+strings.Repeat("large", 400))
		}, want: []string{"over max-ctx-chars"}},
		{name: "unreadable stored profiles", bindings: "  - {name: affected, profiles: [work], instructions: [~/suspect.md]}\n", prepare: func(t *testing.T, h string) {
			testenv.Write(t, profile.StoredPath(), `{"profiles":["work"],`)
		}, want: []string{"not a valid profile selection"}},
		{name: "invalid environment profile", bindings: "  - {name: affected, profiles: [work], instructions: [~/rest.md]}\n", prepare: func(t *testing.T, _ string) {
			t.Setenv("CTXTRA_PROFILES", "work,on call")
		}, want: []string{"on call", "not a valid profile name"}, files: []string{"rest.md"}, profiles: []string{"work"}},
		{name: "non-text scalar", bindings: "  - {name: affected, profiles: [true], instructions: [~/suspect.md]}\n", want: []string{"quote", "true"}},
		{name: "local repository not on this machine", top: "sources: {team: {repo: ~/missing, branch: main}}\n", bindings: sourceBinding, unavailable: true, files: []string{"rest.md"}},
		{name: "unknown source entry", bindings: "  - {name: affected, instructions: ['missing:a.md', ~/rest.md]}\n", want: []string{"no source missing"}, files: []string{"rest.md"}},
		{name: "glob matches nothing", bindings: "  - {name: affected, instructions: ['~/absent-*.md', ~/rest.md]}\n", want: []string{"matches no file"}, files: []string{"rest.md"}},
		{name: "path leaves source", top: dirSource, bindings: "  - {name: affected, instructions: ['team:../suspect.md', ~/rest.md]}\n", want: []string{"the path leaves its source"}, files: []string{"rest.md"}},
		{name: "missing collection", top: dirSource, bindings: "  - {name: affected, collections: ['team:missing'], instructions: [~/rest.md]}\n", manifest: "schema: ctxtra/source/v1\n", want: []string{"no collection missing"}, files: []string{"rest.md"}},
		{name: "missing positive repo set", top: dirSource, bindings: "  - {name: affected, location: {repo-sets: ['team:missing']}, instructions: [~/suspect.md]}\n", manifest: "schema: ctxtra/source/v1\n", want: []string{"no repo set missing", "matches nothing"}},
		{name: "broken positive location with matching sibling", bindings: "  - {name: affected, location: {dirs: [relative, ~/work]}, instructions: [~/rest.md]}\n", want: []string{"relative", "absolute"}, files: []string{"rest.md"}},
		{name: "missing positive set with matching folder", top: dirSource, bindings: "  - {name: affected, location: {dirs: [~/work], repo-sets: ['team:missing']}, instructions: [~/rest.md]}\n", manifest: "schema: ctxtra/source/v1\n", want: []string{"no repo set missing"}, files: []string{"rest.md"}},
		{name: "missing excluded set with matching folder", top: dirSource, bindings: "  - {name: affected, location: {dirs: [~/work], repo-sets: ['!team:missing']}, instructions: [~/suspect.md]}\n", manifest: "schema: ctxtra/source/v1\n", want: []string{"!team:missing", "binding affected is left out"}},
		{name: "empty repo set", top: "repo-sets: {empty: []}\n", bindings: "  - {name: affected, location: {repo-sets: [empty]}, instructions: [~/suspect.md]}\n", want: []string{"the set has no repositories, so it matches nothing"}},
		{name: "untrusted symlink target", top: dirSource, bindings: "  - {name: affected, instructions: ['team:link.md', 'team:a.md']}\n", prepare: func(t *testing.T, h string) {
			target := testenv.Repo(t, filepath.Join(h, "foreign"), "https://github.com/example/foreign.git")
			if err := os.Symlink(filepath.Join(target, "AGENTS.md"), filepath.Join(h, "team", "link.md")); err != nil {
				t.Fatal(err)
			}
		}, want: []string{"leads through a symlink", "not a trusted source"}, files: []string{"team/a.md"}},
		{name: "manifest sources key", top: dirSource, manifest: "schema: ctxtra/source/v1\nsources: {}\ncollections: {offered: {instructions: [b.md]}}\n", bindings: "  - {name: affected, collections: ['team:offered'], instructions: ['team:a.md']}\n", want: []string{"a manifest has no sources"}, files: []string{"team/a.md"}},
		{name: "manifest home path", top: dirSource, manifest: "schema: ctxtra/source/v1\ncollections: {offered: {instructions: [~/suspect.md, a.md]}}\n", bindings: "  - {name: affected, collections: ['team:offered']}\n", want: []string{"relative to the top of this source"}, files: []string{"team/a.md"}},
		{name: "manifest escaping path", top: dirSource, manifest: "schema: ctxtra/source/v1\ncollections: {offered: {instructions: [../suspect.md, a.md]}}\n", bindings: "  - {name: affected, collections: ['team:offered']}\n", want: []string{"the path leaves its source"}, files: []string{"team/a.md"}},
		{name: "manifest source reference", top: dirSource, manifest: "schema: ctxtra/source/v1\ncollections: {offered: {collections: ['other:base'], instructions: [a.md]}}\n", bindings: "  - {name: affected, collections: ['team:offered']}\n", want: []string{"a manifest references only its own collections"}, files: []string{"team/a.md"}},
		{name: "manifest set keeps valid entries", top: dirSource, manifest: "schema: ctxtra/source/v1\nrepo-sets: {offered: [~/work, github.com/example/app]}\n", bindings: "  - {name: affected, location: {repo-sets: ['team:offered']}, instructions: [~/rest.md]}\n", prepare: func(t *testing.T, h string) {
			testenv.Repo(t, filepath.Join(h, "work"), "https://github.com/example/app.git")
		}, want: []string{"a set lists repositories only"}, files: []string{"rest.md"}},
		{name: "unknown manifest collection key", top: dirSource, manifest: "schema: ctxtra/source/v1\ncollections: {broken: {instructions: [b.md], unknown: true}, offered: {instructions: [a.md]}}\n", bindings: "  - {name: affected, collections: ['team:broken', 'team:offered']}\n", want: []string{`unknown key "unknown"`}, files: []string{"team/a.md"}},
		{name: "unknown config repo set key", top: "repo-sets: {broken: [{repo: github.com/example/app, unknown: true}], healthy: [github.com/example/app]}\n", bindings: "  - {name: affected, location: {repo-sets: [broken]}, instructions: [~/suspect.md]}\n  - {name: healthy-set, location: {repo-sets: [healthy]}, instructions: [~/rest.md]}\n", prepare: matchingErrorTableRepo, want: []string{`unknown key "unknown"`}, files: []string{"rest.md"}},
		{name: "unknown manifest repo set key", top: dirSource, manifest: "schema: ctxtra/source/v1\nrepo-sets: {broken: [{repo: github.com/example/app, unknown: true}], healthy: [github.com/example/app]}\n", bindings: "  - {name: affected, location: {repo-sets: ['team:broken']}, instructions: [~/suspect.md]}\n  - {name: healthy-set, location: {repo-sets: ['team:healthy']}, instructions: [~/rest.md]}\n", prepare: matchingErrorTableRepo, want: []string{`unknown key "unknown"`}, files: []string{"rest.md"}},
		{name: "agent capacity", bindings: "  - {name: affected, instructions: [~/suspect.md]}\n", prepare: func(t *testing.T, h string) {
			testenv.Write(t, filepath.Join(h, "suspect.md"), "MARKER-suspect.md\n"+strings.Repeat("large", 80000))
		}, want: []string{"limit of"}},
		{name: "repo source symlink in glob", top: repoSource, bindings: "  - {name: affected, instructions: ['team:*.md', ~/rest.md]}\n", prepare: downloadErrorTableSymlinks, want: []string{"team:link.md", "symlink"}, files: []string{"team/a.md", "rest.md"}, repoFiles: true},
		{name: "repo source symlink named directly", top: repoSource, bindings: "  - {name: affected, instructions: ['team:link.md', 'team:a.md', ~/rest.md]}\n", prepare: downloadErrorTableSymlinks, want: []string{"team:link.md", "symlink"}, files: []string{"team/a.md", "rest.md"}, repoFiles: true},
		{name: "repo source folder symlink in glob", top: repoSource, bindings: "  - {name: affected, instructions: ['team:linked/*.md', 'team:a.md', ~/rest.md]}\n", prepare: downloadErrorTableSymlinks, want: []string{"team:linked", "symlink"}, files: []string{"team/a.md", "rest.md"}, repoFiles: true},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := testenv.Home(t)
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			binary = func() (string, []string, error) { return bin, nil, nil }
			t.Cleanup(func() { binary = setup.Binary })
			testenv.Config(t, h, "schema: ctxtra/config/v1\ncheck-releases: false\n")
			setUp(t, bin, "claude-code")
			for _, f := range []string{"good.md", "suspect.md", "rest.md", "team/a.md", "team/b.md", "work/.keep"} {
				testenv.Write(t, filepath.Join(h, f), "MARKER-"+f+"\n")
			}
			conf := "schema: ctxtra/config/v1\ncheck-releases: false\n" + r.top + "bindings:\n" + goodBinding + r.bindings
			if r.raw != "" {
				conf = r.raw
			}
			testenv.Config(t, h, conf)
			if r.manifest != "" {
				testenv.Write(t, filepath.Join(h, "team", "ctxtra-source.yaml"), r.manifest)
			}
			if r.lock != "" {
				testenv.Write(t, filepath.Join(h, ".config", "ctxtra", "ctxtra.lock"), r.lock)
			}
			if r.prepare != nil {
				r.prepare(t, h)
			}
			dir := filepath.Join(h, "work")
			filePath := func(f string) string {
				if r.repoFiles && strings.HasPrefix(f, "team/") {
					return filepath.Join(loadEnv().Catalog.Source("team").Top, strings.TrimPrefix(f, "team/"))
				}
				return filepath.Join(h, f)
			}
			input, _ := json.Marshal(map[string]string{"cwd": dir, "source": "startup", "session_id": "errors"})
			answers := runHooks(t, [][]string{{"hook", "claude-code", "SessionStart", agent.SetupVersionArg, "--slot", "0", "--of", "40"}}, string(input))
			var answer struct {
				SystemMessage      string
				HookSpecificOutput struct{ AdditionalContext string }
			}
			if err := json.Unmarshal([]byte(answers[0].raw), &answer); err != nil {
				t.Fatal(err)
			}
			entry := lastEntry(t, "claude-code", "SessionStart")
			wantFiles := make([]string, 0, 1+len(r.files))
			if !r.whole {
				wantFiles = append(wantFiles, filepath.Join(h, "good.md"))
			}
			for _, f := range r.files {
				wantFiles = append(wantFiles, filePath(f))
			}
			if !slices.Equal(entry.Files, wantFiles) {
				t.Errorf("session files %v, want %v", entry.Files, wantFiles)
			}
			for _, f := range []string{"good.md", "suspect.md", "rest.md", "team/a.md", "team/b.md"} {
				want := slices.Contains(wantFiles, filePath(f))
				if strings.Contains(answer.HookSpecificOutput.AdditionalContext, "MARKER-"+f) != want {
					t.Errorf("session content for %s: want present %v", f, want)
				}
			}
			for _, marker := range r.forbidden {
				if strings.Contains(answer.HookSpecificOutput.AdditionalContext, marker) {
					t.Errorf("omitted content was delivered: %s", marker)
				}
			}
			if !slices.Equal(entry.Profiles, r.profiles) {
				t.Errorf("active profiles %v, want %v", entry.Profiles, r.profiles)
			}
			wantCode := 1
			if r.unavailable {
				wantCode = 0
				if len(entry.Errors) != 0 || strings.Contains(answer.SystemMessage, "error") {
					t.Errorf("unavailable source reported as an error: %v, %s", entry.Errors, answer.SystemMessage)
				}
			} else {
				if !strings.Contains(answer.SystemMessage, strconv.Itoa(len(entry.Errors))+" error") || !strings.Contains(answer.SystemMessage, "ctxtra doctor") {
					t.Errorf("session message doesn't report the error: %s", answer.SystemMessage)
				}
				for _, want := range r.want {
					if !strings.Contains(strings.Join(entry.Errors, "\n"), want) {
						t.Errorf("log doesn't report %q: %v", want, entry.Errors)
					}
				}
			}
			for _, args := range [][]string{{"context", "explain", dir, "--agent", "claude-code"}, {"doctor", dir}} {
				code, out, errOut := run(args...)
				if code != wantCode {
					t.Errorf("%v: exit %d, want %d\n%s%s", args, code, wantCode, out, errOut)
				}
				for _, want := range r.want {
					if !strings.Contains(out, want) {
						t.Errorf("%v doesn't report %q:\n%s%s", args, want, out, errOut)
					}
				}
				if r.unavailable && !strings.Contains(out, "not on this machine") {
					t.Errorf("%v doesn't list the unavailable source:\n%s", args, out)
				}
				jsonArgs := append(slices.Clone(args), "--json")
				code, out, errOut = run(jsonArgs...)
				var doc struct {
					Data struct {
						Sources  []sourceInfo
						Bindings []traceInfo
					}
					Diagnostics []diagnostic
				}
				if err := json.Unmarshal([]byte(out), &doc); err != nil || code != wantCode {
					t.Fatalf("%v: exit %d, parse error %v\n%s%s", jsonArgs, code, err, out, errOut)
				}
				for _, want := range r.want {
					if !slices.ContainsFunc(doc.Diagnostics, func(d diagnostic) bool {
						return d.Severity == "error" && strings.Contains(d.Message, want)
					}) {
						t.Errorf("%v doesn't report error %q: %+v", jsonArgs, want, doc.Diagnostics)
					}
				}
				if r.unavailable {
					if slices.ContainsFunc(doc.Diagnostics, func(d diagnostic) bool { return d.Severity == "error" }) {
						t.Errorf("%v reports an error for a source not on this machine: %+v", jsonArgs, doc.Diagnostics)
					}
					if args[0] == "doctor" {
						if len(doc.Data.Sources) != 1 {
							t.Fatalf("doctor sources: %+v", doc.Data.Sources)
						}
						src := doc.Data.Sources[0]
						if src.Name != "team" || src.State != "not on this machine" || src.Location != "~/missing" || src.Why != "~/missing is not on this machine" {
							t.Errorf("doctor unavailable source: %+v", src)
						}
					} else {
						index := slices.IndexFunc(doc.Data.Bindings, func(b traceInfo) bool { return b.Binding == "affected" })
						if index < 0 || !slices.Equal(doc.Data.Bindings[index].Unavailable, []string{"team"}) {
							t.Errorf("explain unavailable binding: %+v", doc.Data.Bindings)
						}
					}
				}
			}
			if r.check != nil {
				r.check(t, h)
			}
		})
	}
}

func matchingErrorTableRepo(t *testing.T, h string) {
	t.Helper()
	testenv.Repo(t, filepath.Join(h, "work"), "https://github.com/example/app.git")
}

func downloadErrorTableSymlinks(t *testing.T, h string) {
	t.Helper()
	repo := testenv.Repo(t, filepath.Join(h, "srv", "team"), "")
	if err := os.Remove(filepath.Join(repo, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	testenv.Write(t, filepath.Join(repo, "a.md"), "MARKER-team/a.md\n")
	for name, target := range map[string]string{"link.md": "a.md", "linked": "."} {
		if err := os.Symlink(target, filepath.Join(repo, name)); err != nil {
			t.Fatal(err)
		}
	}
	testenv.Run(t, repo, "git", "add", "-A")
	testenv.Run(t, repo, "git", "commit", "-qm", "add content")
	commit, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	url, stop, err := testenv.StartGitServer(filepath.Join(h, "srv"))
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	t.Setenv("GIT_SSL_NO_VERIFY", "1")
	url += "/team"
	conf, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	testenv.Write(t, paths.ConfigPath(), strings.ReplaceAll(string(conf), "https://github.com/example/team.git", url))
	writeLock(t, paths.ConfigPath(), fmt.Sprintf("  team: {repo: '%s', branch: main, commit: %s}\n", url, strings.TrimSpace(string(commit))))
	if code, out, errOut := run("source", "download"); code != 0 {
		t.Fatalf("download: exit %d\n%s%s", code, out, errOut)
	}
	if err := os.RemoveAll(source.GitDir()); err != nil {
		t.Fatal(err)
	}
}

func checkErrorTableApproval(t *testing.T, h string) {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "mcp", "proxy", "codex", agent.SetupVersionArg)
	cmd.Dir = filepath.Join(h, "work")
	cmd.Env = append(os.Environ(), "CTXTRA_TEST_HOOK=1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "approval-test", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	tools, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	healthy := false
	for _, tool := range tools.Tools {
		healthy = healthy || tool.Name == "healthy__echo"
		if strings.HasPrefix(tool.Name, "deploy__") {
			t.Errorf("unapproved tool exposed: %s", tool.Name)
		}
	}
	if !healthy {
		t.Fatal("the healthy server has no echo tool")
	}
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "healthy__echo", Arguments: map[string]any{"value": "works"}})
	if err != nil || result.IsError || len(result.Content) != 1 || result.Content[0].(*mcp.TextContent).Text != `echo {"value":"works"}` {
		t.Errorf("healthy tool: %v, %+v", err, result)
	}
	if result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "deploy__echo", Arguments: map[string]any{}}); err == nil && !result.IsError {
		t.Errorf("unapproved tool succeeded: %+v", result)
	}
	if err := client.Close(); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(h, "healthy-started")); err != nil {
		t.Errorf("healthy server never started: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h, "rejected-started")); !os.IsNotExist(err) {
		t.Errorf("unapproved server started: %v", err)
	}
}
