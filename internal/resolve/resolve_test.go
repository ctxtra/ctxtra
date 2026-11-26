package resolve

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

const head = "bindings:\n"

// load writes the config, with settings before the bindings, and loads
// it.
func loadConfig(t *testing.T, h, bindings, settings string) *Env {
	t.Helper()
	return Load(testenv.Config(t, h, "schema: ctxtra/config/v1\n"+settings+bindings), agentNames)
}

func run(dir string, env *Env) *Result { return env.Resolve(Input{Dir: dir}) }

// messages are the errors' messages.
func messages(errs []*catalog.Error) []string {
	var out []string
	for _, e := range errs {
		out = append(out, e.Err.Msg)
	}
	return out
}

func names(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, filepath.Base(h.Path))
	}
	return out
}

func TestOrderDedupeAndMissing(t *testing.T) {
	h := testenv.Home(t)
	r := testenv.Repo(t, filepath.Join(h, "svc"), "git@github.com:acme/svc.git")
	for _, n := range []string{"a", "b", "c"} {
		testenv.Write(t, filepath.Join(h, "n", n+".md"), strings.ToUpper(n))
	}
	cfg := loadConfig(t, h, head+`  - {name: one, instructions: [~/n/b.md, ~/n/a.md]}
  - {name: two, location: {repos: [github.com/acme/svc]}, instructions: [~/n/a.md, ~/n/c.md, ~/n/gone.md]}
  - {name: three, location: {repos: [github.com/other/x]}, instructions: [~/n/c.md]}
`, "")
	res := run(r, cfg)
	if got := names(res.Files); !slices.Equal(got, []string{"b.md", "a.md", "c.md"}) {
		t.Errorf("files %v", got)
	}
	if got := messages(res.Errors); len(got) != 1 || !strings.Contains(got[0], "gone.md is not a file") {
		t.Errorf("errors %v", got)
	}
	if len(res.Trace) != 3 || res.Trace[2].Matched {
		t.Errorf("trace %+v", res.Trace)
	}
	// Outside git only the global binding applies.
	if got := names(run(h, cfg).Files); !slices.Equal(got, []string{"b.md", "a.md"}) {
		t.Errorf("outside git: %v", got)
	}
}

func TestRender(t *testing.T) {
	h := testenv.Home(t)
	r := testenv.Repo(t, filepath.Join(h, "svc"), "git@github.com:acme/svc.git")
	plain := filepath.Join(h, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	testenv.Write(t, filepath.Join(h, "g.md"), "G\n\n")
	testenv.Write(t, filepath.Join(h, "s.md"), "S")
	cfg := loadConfig(t, h, head+"  - {name: g, instructions: [~/g.md]}\n  - {name: s, location: {dirs: [~/**]}, instructions: [~/s.md]}\n", "")
	text := run(r, cfg).Render()
	for _, want := range []string{
		"Contents of " + filepath.Join(h, "g.md") + " (instructions added by ctxtra for every session; binding: g):\n\nG\n\n",
		"for this repository, ~/svc, and all its worktrees",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if text := run(plain, cfg).Render(); !strings.Contains(text, "for sessions in ~/plain") {
		t.Errorf("plain folder:\n%s", text)
	}
}

func TestBudgetDropsFromTheEnd(t *testing.T) {
	h := testenv.Home(t)
	testenv.Skill(t, filepath.Join(h, "s", "deploy"), "description: Deploy it.")
	// Two bytes per character: the budget counts characters.
	for _, n := range []string{"a", "b", "c"} {
		testenv.Write(t, filepath.Join(h, n+".md"), strings.Repeat("é", 500))
	}
	cfg := loadConfig(t, h, head+"  - {name: x, instructions: [~/a.md, ~/b.md, ~/c.md], skills: [~/s/deploy]}\n", "max-ctx-chars: 1800\n")
	res := run(h, cfg)
	if got := names(res.Files); !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("kept %v, dropped %v, %d chars", got, names(res.Dropped), Chars(res.Render()))
	}
	if len(res.Dropped) != 2 || res.Dropped[0].Over != "max-ctx-chars" {
		t.Errorf("dropped %+v", res.Dropped)
	}
	if len(res.Skills) != 1 || Chars(res.Render()) > 1800 || len(res.Render()) <= 1800 {
		t.Errorf("skills %v, %d chars, %d bytes", names(res.Skills), Chars(res.Render()), len(res.Render()))
	}
}

// Fit drops files from the end, before the ones dropped already, and the
// text it keeps is the same as without them.
func TestFit(t *testing.T) {
	res := &Result{Dir: "/x", Skills: []Hit{{Name: "s", Description: "d", Path: "/s/SKILL.md"}},
		Dropped: []Hit{{Path: "/n/e.md", Over: "max-ctx-chars"}}}
	for _, n := range []string{"a", "b", "c", "d"} {
		res.Files = append(res.Files, Hit{Path: "/n/" + n + ".md", Content: strings.Repeat(n, 100), Global: true})
	}
	calls := 0
	res.Fit(func(r *Result) bool { calls++; return len(r.Files) <= 1 }, "Codex's limit")
	if got := names(res.Files); !slices.Equal(got, []string{"a.md"}) || calls > 4 {
		t.Errorf("kept %v after %d calls", got, calls)
	}
	var over []string
	for _, h := range res.Dropped {
		over = append(over, filepath.Base(h.Path)+": "+h.Over)
	}
	if want := []string{"b.md: Codex's limit", "c.md: Codex's limit", "d.md: Codex's limit", "e.md: max-ctx-chars"}; !slices.Equal(over, want) {
		t.Errorf("dropped %v", over)
	}
	if text := res.Render(); !strings.Contains(text, "aaa") || strings.Contains(text, "bbb") || !strings.Contains(text, "<name>s</name>") {
		t.Errorf("text:\n%s", text)
	}
	// Nothing fits: every file is dropped, the skill list stays.
	res.Fit(func(r *Result) bool { return false }, "x")
	if len(res.Files) != 0 || len(res.Dropped) != 5 || res.Render() == "" {
		t.Errorf("files %v, dropped %v", names(res.Files), names(res.Dropped))
	}
}

func TestSkills(t *testing.T) {
	h := testenv.Home(t)
	testenv.Skill(t, filepath.Join(h, "s", "deploy"), "name: deploy\ndescription: |\n  Deploy\n  it <now>.\nallowed-tools: x")
	testenv.Skill(t, filepath.Join(h, "s", "unnamed"), "description: Uses the folder name.")
	testenv.Skill(t, filepath.Join(h, "t", "deploy"), "description: Same name as the first.")
	testenv.Skill(t, filepath.Join(h, "s", "nodesc"), "name: nodesc")
	testenv.Write(t, filepath.Join(h, "s", "nofront", "SKILL.md"), "# no frontmatter\n")
	testenv.Skill(t, filepath.Join(h, "s", "long"), "description: "+strings.Repeat("é", 600))
	cfg := loadConfig(t, h, head+`  - name: x
    skills: [~/s/deploy, ~/s/unnamed, ~/t/deploy, ~/s/nodesc, ~/s/nofront, ~/s/long, ~/s/missing]
`, "")
	res := run(h, cfg)
	var got []string
	for _, s := range res.Skills {
		got = append(got, s.Name)
	}
	// Two skills with one name both reach the session, as the agent would
	// get them by hand.
	if !slices.Equal(got, []string{"deploy", "unnamed", "deploy", "long"}) {
		t.Errorf("skills %v, errors %v", got, messages(res.Errors))
	}
	if res.Skills[0].Description != "Deploy it <now>." || len(res.Errors) != 3 || len(res.SameSkills) != 1 {
		t.Errorf("description %q, errors %v, same names %v", res.Skills[0].Description, messages(res.Errors), res.SameSkills)
	}
	if d := res.Skills[3].Description; !strings.HasSuffix(d, "…") || !strings.HasPrefix(d, "é") || len(d) > maxSkillDesc {
		t.Errorf("long description %d bytes", len(d))
	}
	if text := res.Render(); !strings.Contains(text, "<description>Deploy it &lt;now&gt;.</description>") {
		t.Errorf("skill list:\n%s", text)
	}
}

func TestLoadLimits(t *testing.T) {
	h := testenv.Home(t)
	big := strings.Repeat("x", config.MaxFileBytes+1)
	testenv.Write(t, filepath.Join(h, "big.md"), big)
	if err := os.Mkdir(filepath.Join(h, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, h, head+"  - {name: x, instructions: [~/big.md, ~/dir.md]}\n", "")
	res := run(h, cfg)
	if len(res.Files) != 0 || len(res.Errors) != 2 {
		t.Errorf("files %v errors %v", names(res.Files), messages(res.Errors))
	}
}

// The same SKILL.md can be an instruction file and a skill at once.
func TestFileAndSkillAreDedupedApart(t *testing.T) {
	h := testenv.Home(t)
	testenv.Skill(t, filepath.Join(h, "s", "deploy"), "description: Deploy it.")
	cfg := loadConfig(t, h, head+"  - {name: x, instructions: [~/s/deploy/SKILL.md], skills: [~/s/deploy]}\n", "")
	if res := run(h, cfg); len(res.Files) != 1 || len(res.Skills) != 1 {
		t.Errorf("files %v, skills %v, errors %v", names(res.Files), names(res.Skills), messages(res.Errors))
	}
}

// Without git (or when git doesn't answer), repos bindings don't apply
// but every other binding still does.
func TestWithoutGit(t *testing.T) {
	h := testenv.Home(t)
	r := testenv.Repo(t, filepath.Join(h, "svc"), "git@github.com:acme/svc.git")
	testenv.Write(t, filepath.Join(h, "a.md"), "A")
	testenv.Write(t, filepath.Join(h, "b.md"), "B")
	cfg := loadConfig(t, h, head+"  - {name: g, instructions: [~/a.md]}\n  - {name: r, location: {repos: [github.com/acme/svc]}, instructions: [~/b.md]}\n", "")
	if got := names(run(r, cfg).Files); !slices.Equal(got, []string{"a.md", "b.md"}) {
		t.Fatalf("with git: %v", got)
	}
	t.Setenv("PATH", t.TempDir())
	if got := names(run(r, cfg).Files); !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("without git: %v", got)
	}
}

func TestParseSkillDelimiters(t *testing.T) {
	for content, ok := range map[string]bool{
		"---\ndescription: x\n---\nbody":     true,
		"---\r\ndescription: x\r\n---  \r\n": true,
		"---\ndescription: x\n---oops\nbody": false,
		"---\ndescription: x\n":              false,
		"description: x\n---\n":              false,
	} {
		if _, _, _, err := parseSkill(content); (err == nil) != ok {
			t.Errorf("%q: err %v", content, err)
		}
	}
	if _, _, body, _ := parseSkill("---\r\ndescription: x\r\n---\r\n\r\n# Deploy\r\nsteps\r\n"); body != "# Deploy\nsteps" {
		t.Errorf("body %q", body)
	}
}

// A file of a source is named by its SOURCE:PATH; a file on disk by its
// path.
func TestRefs(t *testing.T) {
	h := newHome(t)
	h.write("n/me.md", "me")
	one := h.accept("one", "https://h/team", "branch: main", "", commit("a"), map[string]string{"x/a.md": "A", "x/b.md": "B"})
	two := h.accept("two", "https://h/team", "branch: dev", "", commit("a"), nil)
	env := h.load(`sources:
  one: {repo: 'https://h/team', branch: main}
  two: {repo: 'https://h/team', branch: dev}
bindings:
  - {name: me, instructions: ['two:x/a.md', 'one:x/a.md', ~/n/me.md, 'one:x/b.md']}
`, one, two)
	res := env.Resolve(Input{Dir: h.dir})
	noErrors(t, res)
	var got []string
	for _, f := range res.Files {
		got = append(got, f.Ref)
	}
	// The same commit of the same repository: the first reference names the file.
	if want := []string{"two:x/a.md", "", "one:x/b.md"}; !slices.Equal(got, want) {
		t.Errorf("refs %q, want %q", got, want)
	}
}

// Sessions that use a repo source say when it has new commits, and when a
// newer commit wasn't accepted.
func TestNotices(t *testing.T) {
	h := testenv.Home(t)
	url, commit := "https://h/team", strings.Repeat("a", 40)
	hh := &home{t, h}
	entry := hh.accept("team", url, "branch: main", "", commit, map[string]string{"a.md": "A"})
	testenv.Write(t, filepath.Join(h, "n", "me.md"), "me")
	var env *Env
	config := func(check string) {
		env = hh.load("sources:\n  team: {repo: '"+url+"', branch: main"+check+"}\n"+
			head+"  - {name: team, location: {dirs: [~/work]}, instructions: ['team:a.md']}\n  - {name: me, instructions: [~/n/me.md]}\n", entry)
	}
	config("")
	notices := func(dir string) []string {
		t.Helper()
		res := env.Resolve(Input{Dir: dir})
		if len(res.Errors) > 0 {
			t.Fatalf("errors %v", messages(res.Errors))
		}
		return res.Notices
	}
	work := filepath.Join(h, "work")
	os.MkdirAll(work, 0o755)

	if got := notices(work); len(got) != 0 {
		t.Errorf("never checked: %v", got)
	}
	id := source.ID(url)
	state.Update(id, url, "branch main", func(e *state.Source) { e.Head = commit })
	if got := notices(work); len(got) != 0 {
		t.Errorf("up to date: %v", got)
	}
	newCommits := "team has new commits on main\nrun `ctxtra source update team` to review them."
	state.Update(id, url, "branch main", func(e *state.Source) { e.Head = strings.Repeat("c", 40) })
	if got := notices(work); !slices.Equal(got, []string{newCommits}) {
		t.Errorf("new commits: %v", got)
	}
	if got := notices(h); len(got) != 0 {
		t.Errorf("a session that doesn't use the source: %v", got)
	}
	state.Update(id, url, "branch main", func(e *state.Source) { e.Head = strings.Repeat("b", 40); e.Dismissed = strings.Repeat("b", 40) })
	want := []string{"team has a newer commit that you didn't accept\nrun `ctxtra source update team` to review it again."}
	if got := notices(work); !slices.Equal(got, want) {
		t.Errorf("dismissed: %v", got)
	}
	// State about another branch of the repository is kept apart.
	state.Update(id, url, "branch other", func(e *state.Source) { e.Head = strings.Repeat("d", 40); e.Dismissed = "" })
	if got := notices(work); !slices.Equal(got, want) {
		t.Errorf("state of another branch: %v", got)
	}
	// With checks disabled, a commit that an earlier check or fetch found
	// is no notice.
	state.Update(id, url, "branch main", func(e *state.Source) { e.Head = strings.Repeat("c", 40); e.Dismissed = "" })
	config(", check-updates: false")
	if got := notices(work); len(got) != 0 {
		t.Errorf("checks off: %v", got)
	}
}

func serverNames(res *Result) []string {
	var out []string
	for _, s := range res.MCPServers {
		out = append(out, s.Name+"<"+strings.SplitN(s.Origin, " ", 2)[0])
	}
	return out
}

// Sessions get the servers of the bindings that match, in binding order,
// each once, with the binding that added it.
func TestMCPServers(t *testing.T) {
	h := testenv.Home(t)
	mono := testenv.Repo(t, filepath.Join(h, "dev", "mono"), "git@github.com:acme/mono.git")
	svc := testenv.Repo(t, filepath.Join(h, "dev", "svc"), "git@github.com:acme/svc.git")
	wt := filepath.Join(h, "elsewhere", "svc-feature")
	testenv.Run(t, svc, "git", "worktree", "add", "-q", "-b", "feature", wt)
	sub := filepath.Join(mono, "services", "billing", "cmd")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, h, `mcp-servers:
  everywhere: {command: [a]}
  linear_work: {url: https://mcp.linear.app/mcp}
  billing: {command: [b]}
  svc: {command: [c]}
bindings:
  - {name: g, mcp-servers: [everywhere]}
  - {name: work, location: {dirs: ['~/dev/**']}, mcp-servers: [linear_work, everywhere]}
  - {name: bill, location: {repos: [{repo: github.com/acme/mono, subdirs: [services/billing/**]}]}, mcp-servers: [billing, linear_work]}
  - {name: s, location: {dirs: ['~/dev/svc/**']}, mcp-servers: [svc]}
`, "")
	if len(cfg.Errors) > 0 {
		t.Fatal(messages(cfg.Errors))
	}
	for _, tc := range []struct {
		dir  string
		want []string
	}{
		{h, []string{"everywhere<g"}},
		{mono, []string{"everywhere<g", "linear_work<work"}},
		{sub, []string{"everywhere<g", "linear_work<work", "billing<bill"}},
		{wt, []string{"everywhere<g", "linear_work<work", "svc<s"}},
	} {
		res := run(tc.dir, cfg)
		if got := serverNames(res); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.dir, got, tc.want)
		}
	}
	res := run(sub, cfg)
	if res.MCPServers[0].ID != "everywhere" || !res.MCPServers[0].Local() || !slices.Equal(res.Trace[2].MCPServers, []string{"billing", "linear_work"}) {
		t.Errorf("%+v %+v", res.MCPServers[0], res.Trace[2])
	}
}
