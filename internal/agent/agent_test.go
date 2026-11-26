package agent_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/chunk"
	"github.com/ctxtra/ctxtra/internal/jsonorder"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

func TestSummary(t *testing.T) {
	files := func(names ...string) []resolve.Hit {
		var out []resolve.Hit
		for _, n := range names {
			out = append(out, resolve.Hit{Path: "/team/" + n, Source: "team", SourcePath: n})
		}
		return out
	}
	over := func(hits []resolve.Hit, limit string) []resolve.Hit {
		for i := range hits {
			hits[i].Over = limit
		}
		return hits
	}
	skills := []resolve.Hit{{Name: "deploy", Source: "team"}, {Name: "review", Source: "team"}}
	for want, res := range map[string]*resolve.Result{
		"✓ ctxtra added 2 instructions\nInstructions:\n  team\n  - a.md\n  - b.md": {Files: files("a.md", "b.md")},
		"✓ ctxtra added 1 instruction and 2 skills\nInstructions:\n  team\n  - a.md\nSkills:\n  team\n  - deploy\n  - review": {
			Files: files("a.md"), Skills: skills},
		"✓ ctxtra added 2 skills\nSkills:\n  team\n  - deploy\n  - review": {Skills: skills},
		"✓ ctxtra added 1 instruction; 2 errors, dropped b.md, c.md (over max-ctx-chars) (run ctxtra doctor)\nInstructions:\n  team\n  - a.md": {
			Files: files("a.md"), Errors: errs(2), Dropped: over(files("b.md", "c.md"), "max-ctx-chars")},
		"✓ ctxtra added 1 instruction; dropped b.md, c.md, d.md, +1 more (over Claude Code's limit of 360,000 characters), dropped z.md (over max-ctx-chars) (run ctxtra doctor)\nInstructions:\n  team\n  - a.md": {
			Files: files("a.md"), Dropped: append(over(files("b.md", "c.md", "d.md", "e.md"), "Claude Code's limit of 360,000 characters"),
				over(files("z.md"), "max-ctx-chars")...)},
		"ctxtra added nothing; 2 errors (run ctxtra doctor)": {Errors: errs(2)},
		"ctxtra added nothing; 1 error (run ctxtra doctor)":  {Errors: errs(1)},
		"✓ ctxtra added 1 instruction\nInstructions:\n  team\n  - a.md\n\nℹ team has new commits on main\nℹ n2": {
			Files: files("a.md"), Notices: []string{"team has new commits on main", "n2"}},
		// A notice after the skills list doesn't read as one of the skills.
		// The command on a notice's second line is indented under it.
		"✓ ctxtra added 2 skills\nSkills:\n  team\n  - deploy\n  - review\n\nℹ team has new commits on main\n  run `ctxtra source update team` to review them.": {
			Skills: skills, Notices: []string{"team has new commits on main\nrun `ctxtra source update team` to review them."}},
		"ctxtra added nothing\n\nℹ n": {Notices: []string{"n"}},
	} {
		res.Summary = resolve.DefaultSessionMessage
		if got := agent.Summary(res); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	}
	if agent.Summary(nil) != "" || agent.Summary(&resolve.Result{Summary: resolve.DefaultSessionMessage}) != "" {
		t.Error("summary when nothing was added")
	}
}

// Entries are grouped by source, in the order of each group's first entry,
// and keep their order in the group. Files are shown by their path in the
// source; outside sources by full path. Skills that ctxtra installed are in
// a group of their own.
func TestSummaryGroups(t *testing.T) {
	h := testenv.Home(t)
	src := func(name, rel string) resolve.Hit {
		return resolve.Hit{Path: filepath.Join("/src", name, rel), Source: name, SourcePath: rel}
	}
	local := func(rel string) resolve.Hit { return resolve.Hit{Path: filepath.Join(h, rel)} }
	skill := func(hit resolve.Hit, name string) resolve.Hit { hit.Name = name; return hit }
	res := &resolve.Result{
		// A file in ctxtra's skills folder is not one of ctxtra's skills.
		Files: []resolve.Hit{src("aictx", "global/style.md"), local("work/AGENTS.md"), src("team", "style.md"),
			src("aictx", "personal/style.md"), {Path: "/etc/ctx/style.md"}, local(".local/share/ctxtra/skills/ctxtra/SKILL.md")},
		Skills: []resolve.Hit{skill(local(".local/share/ctxtra/skills/ctxtra/SKILL.md"), "ctxtra"), skill(src("openspec", "skills/explore/SKILL.md"), "explore"),
			skill(local("skills/deploy/SKILL.md"), "deploy"), skill(src("openspec", "skills/propose/SKILL.md"), "propose"),
			// A source named like a group of its own is not that group.
			skill(src("local", "skills/x/SKILL.md"), "x")},
		Summary: resolve.DefaultSessionMessage,
	}
	want := `✓ ctxtra added 6 instructions and 5 skills
Instructions:
  aictx
  - global/style.md
  - personal/style.md
  local
  - ~/work/AGENTS.md
  - /etc/ctx/style.md
  - ~/.local/share/ctxtra/skills/ctxtra/SKILL.md
  team
  - style.md
Skills:
  ctxtra
  - ctxtra
  openspec
  - explore
  - propose
  local
  - deploy
  local
  - x`
	if got := agent.Summary(res); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Dropped files are named as before: by base name, by full path when two
// have the same name.
func TestSummarySameNames(t *testing.T) {
	h := testenv.Home(t)
	local := func(rel string) resolve.Hit { return resolve.Hit{Path: filepath.Join(h, rel)} }
	dropped := func(hit resolve.Hit) resolve.Hit { hit.Over = "max-ctx-chars"; return hit }
	pinned := func(ref string) resolve.Hit {
		return resolve.Hit{Path: "/store/" + strings.ReplaceAll(ref, ":", "/"), Ref: ref}
	}
	for want, res := range map[string]*resolve.Result{
		"✓ ctxtra added 1 instruction; dropped ~/b/style.md, a:style.md, c.md (over max-ctx-chars) (run ctxtra doctor)\nInstructions:\n  local\n  - ~/a/style.md": {
			Files: []resolve.Hit{local("a/style.md")}, Dropped: []resolve.Hit{dropped(local("b/style.md")), dropped(pinned("a:style.md")), dropped(local("b/c.md"))}},
		"ctxtra added nothing; dropped ~/a/style.md, ~/b/style.md (over max-ctx-chars) (run ctxtra doctor)": {
			Dropped: []resolve.Hit{dropped(local("a/style.md")), dropped(local("b/style.md"))}},
		// A line end in a parent folder stays on the line.
		`ctxtra added nothing; dropped "~/a\nb/style.md", ~/b/style.md (over max-ctx-chars) (run ctxtra doctor)`: {
			Dropped: []resolve.Hit{dropped(local("a\nb/style.md")), dropped(local("b/style.md"))}},
		"✓ ctxtra added 1 instruction\nInstructions:\n  local\n  - \"~/a\\nb/style.md\"": {Files: []resolve.Hit{local("a\nb/style.md")}},
	} {
		res.Summary = resolve.DefaultSessionMessage
		if got := agent.Summary(res); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	}
}

// Resolved from a config: files and skills of sources by their path in the
// source, never by their path in the store; other files by full path.
func TestSummarySources(t *testing.T) {
	h := testenv.Home(t)
	commit := strings.Repeat("a", 40)
	a, b := source.GenDir("https://h/a", "", commit), source.GenDir("https://h/b", "", commit)
	for _, f := range []string{filepath.Join(a, "style.md"), filepath.Join(a, "go", "style.md"), filepath.Join(b, "style.md"),
		filepath.Join(h, "ctx", "style.md"), filepath.Join(h, "dots", "ai", "me.md")} {
		testenv.Write(t, f, "text")
	}
	testenv.Write(t, filepath.Join(b, "skills", "deploy", "SKILL.md"), "---\nname: deploy\ndescription: d\n---\nbody")
	testenv.Write(t, filepath.Join(h, "dots", "skills", "review", "SKILL.md"), "---\nname: review\ndescription: d\n---\nbody")
	res := session(t, h, `sources:
  a: {repo: 'https://h/a', branch: main}
  b: {repo: 'https://h/b', branch: main}
  dots: {dir: ~/dots}
  ai: {dir: ~/dots/ai}
bindings:
  - {name: all, instructions: [~/ctx/style.md, 'a:**/style.md', 'b:style.md', 'ai:me.md'],
     skills: ['b:skills/deploy', 'dots:skills/review']}
`, `sources:
  a: {repo: 'https://h/a', branch: main, commit: '`+commit+`'}
  b: {repo: 'https://h/b', branch: main, commit: '`+commit+`'}
`)
	if len(res.Errors) > 0 {
		t.Fatalf("errors %v", res.Errors)
	}
	want := `✓ ctxtra added 5 instructions and 2 skills
Instructions:
  local
  - ~/ctx/style.md
  a
  - go/style.md
  - style.md
  b
  - style.md
  ai
  - me.md
Skills:
  b
  - deploy
  dots
  - review`
	if got := agent.Summary(res); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The limits are per group, not per list; the line after the lists tells
// where to see them all. A limit of 0 shows each group's number of entries.
// A hidden list is still counted on the first line.
func TestSummaryMessage(t *testing.T) {
	res := &resolve.Result{Notices: []string{"n"}}
	for i := 1; i <= 4; i++ {
		res.Files = append(res.Files, resolve.Hit{Source: "a", SourcePath: fmt.Sprintf("a%d.md", i)})
		res.Skills = append(res.Skills, resolve.Hit{Source: "a", Name: fmt.Sprintf("s%d", i)})
	}
	res.Files = append(res.Files, resolve.Hit{Source: "b", SourcePath: "b1.md"})
	res.Skills = append(res.Skills, resolve.Hit{Source: "b", Name: "t1"})
	show := func(maxInstructions, maxSkills int) resolve.SessionMessage {
		return resolve.SessionMessage{MaxInstructions: maxInstructions, MaxSkills: maxSkills}
	}
	for _, c := range []struct {
		name string
		s    resolve.SessionMessage
		want string
	}{
		{"cut", show(2, 3), `✓ ctxtra added 5 instructions and 5 skills
Instructions:
  a
  - a1.md
  - a2.md
  - +2 more
  b
  - b1.md
Skills:
  a
  - s1
  - s2
  - s3
  - +1 more
  b
  - t1
Full list: ctxtra context files

ℹ n`},
		{"all fit", show(4, 4), `✓ ctxtra added 5 instructions and 5 skills
Instructions:
  a
  - a1.md
  - a2.md
  - a3.md
  - a4.md
  b
  - b1.md
Skills:
  a
  - s1
  - s2
  - s3
  - s4
  b
  - t1

ℹ n`},
		{"no instructions", show(0, 4), `✓ ctxtra added 5 instructions and 5 skills
Skills:
  a
  - s1
  - s2
  - s3
  - s4
  b
  - t1

ℹ n`},
		{"no skills", show(4, 0), `✓ ctxtra added 5 instructions and 5 skills
Instructions:
  a
  - a1.md
  - a2.md
  - a3.md
  - a4.md
  b
  - b1.md

ℹ n`},
		{"no lists", show(0, 0), "✓ ctxtra added 5 instructions and 5 skills\n\nℹ n"},
	} {
		res.Summary = c.s
		if got := agent.Summary(res); got != c.want {
			t.Errorf("%s: got\n%s\nwant\n%s", c.name, got, c.want)
		}
	}
	// Hidden lists still leave the errors and dropped files.
	hidden := &resolve.Result{Files: res.Files[:1], Skills: res.Skills[:1], Errors: errs(1),
		Dropped: []resolve.Hit{{Path: "/x/big.md", Over: "max-ctx-chars"}}, Summary: show(0, 0)}
	if got, want := agent.Summary(hidden), "✓ ctxtra added 1 instruction and 1 skill; 1 error, dropped big.md (over max-ctx-chars) (run ctxtra doctor)"; got != want {
		t.Errorf("hidden: got %q\nwant %q", got, want)
	}
}

// From a config: the defaults list 30 entries of a group; session-message
// changes each list on its own.
func TestSummaryMessageConfig(t *testing.T) {
	h := testenv.Home(t)
	for i := range 31 {
		testenv.Write(t, filepath.Join(h, "ctx", fmt.Sprintf("f%02d.md", i)), "text")
	}
	testenv.Write(t, filepath.Join(h, "ctx", "skills", "deploy", "SKILL.md"), "---\nname: deploy\ndescription: d\n---\nbody")
	const bindings = `sources:
  ctx: {dir: ~/ctx}
bindings:
  - {name: all, instructions: ['ctx:*.md'], skills: ['ctx:skills/deploy']}
`
	res := session(t, h, bindings, "")
	if got := agent.Summary(res); !strings.Contains(got, "  - f29.md\n  - +1 more\nSkills:\n  ctx\n  - deploy\nFull list") || strings.Contains(got, "f30.md") {
		t.Errorf("defaults:\n%s", got)
	}
	res = session(t, h, "session-message: {max-skills: 0, max-instructions: 2}\n"+bindings, "")
	if got, want := agent.Summary(res), "✓ ctxtra added 31 instructions and 1 skill\nInstructions:\n  ctx\n  - f00.md\n  - f01.md\n  - +29 more\nFull list: ctxtra context files"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestDelivery(t *testing.T) {
	d := agent.Delivery{Title: "Claude Code", Capacity: 360_000, Chunk: 9000, Slots: 40}
	if got := d.Over(); got != "Claude Code's limit of 360,000 characters" {
		t.Errorf("over: %q", got)
	}
	if got := (agent.Delivery{Title: "Codex", Capacity: 1_000_000, Unit: chunk.Bytes}).Over(); got != "Codex's limit of 1,000,000 bytes" {
		t.Errorf("over: %q", got)
	}
	// Three files of 140,000 characters: two fit 40 parts of 9,000, the third doesn't.
	res := &resolve.Result{Dir: "/x"}
	for _, n := range []string{"x", "y", "z"} {
		res.Files = append(res.Files, resolve.Hit{Path: "/" + n + ".md", Global: true,
			Content: strings.Repeat(strings.Repeat(n, 99)+"\n", 1400)})
	}
	d.Fit(res)
	if len(res.Files) != 2 || len(res.Dropped) != 1 || res.Dropped[0].Over != d.Over() {
		t.Fatalf("files %d, dropped %+v", len(res.Files), res.Dropped)
	}
	if n := len(d.Chunks(res)); n > 40 || n < 30 {
		t.Errorf("%d parts", n)
	}
	// Muse's parts count bytes of JSON: CJK text needs three times the
	// parts, and so do quotes and backslashes.
	muse := agent.Delivery{Title: "Muse", Capacity: 360_000, Unit: chunk.UTF16, Chunk: 13_000, ChunkJSON: true, Slots: 40}
	for _, line := range []string{"観測", `\"\"\"`} {
		res := &resolve.Result{Dir: "/x", Files: []resolve.Hit{{Path: "/c.md", Global: true, Content: strings.Repeat(line+"\n", 30_000)}}}
		parts := muse.Chunks(res)
		for _, p := range parts {
			if b, _ := jsonorder.Marshal(p); len(b)-2 > 13_000 {
				t.Errorf("%q: part of %d bytes of JSON", line, len(b)-2)
			}
		}
		if len(parts) < 17 {
			t.Errorf("%q: %d parts for over 210,000 bytes of JSON", line, len(parts))
		}
	}
	// Parts that don't fit the slots, below the capacity: a smaller chunk.
	small := agent.Delivery{Title: "Hermes", Capacity: 1_000_000, Chunk: 5000, Slots: 40}
	res.Files, res.Dropped = append(res.Files, res.Dropped...), nil
	small.Fit(res)
	if len(res.Files) != 1 {
		t.Errorf("files %d with 40 parts of 5,000", len(res.Files))
	}
}

// A setup with fewer slots gets fewer parts; one with more gets no more
// than the agent takes.
func TestWithSlots(t *testing.T) {
	d := agent.Delivery{Title: "Claude Code", Capacity: 360_000, Chunk: 9000, Slots: 40}
	if got := d.WithSlots(20); got.Slots != 20 || got.Capacity != 180_000 {
		t.Errorf("20 slots: %+v", got)
	}
	for _, n := range []int{0, 40, 64} {
		if got := d.WithSlots(n); got != d {
			t.Errorf("%d slots: %+v", n, got)
		}
	}
	muse := agent.Delivery{Title: "Muse", Capacity: 360_000, Unit: chunk.UTF16, Chunk: 13_000, ChunkJSON: true, Slots: 40}
	if got := muse.WithSlots(2); got.Slots != 2 || got.Capacity != 360_000 {
		t.Errorf("Muse, 2 slots: %+v", got)
	}
	if one := (agent.Delivery{Title: "Codex", Capacity: 1_000_000}); one.WithSlots(3) != one {
		t.Errorf("an agent without slots: %+v", one.WithSlots(3))
	}
}

func TestSystemMessage(t *testing.T) {
	if got := agent.SystemMessage("SessionStart", "a <b> & \"c\""); got != `{"systemMessage":"a <b> & \"c\""}` {
		t.Errorf("session start: %s", got)
	}
	if got := agent.SystemMessage("SubagentStart", "x"); got != "" {
		t.Errorf("subagent: %s", got)
	}
}

func TestSummaryMCPList(t *testing.T) {
	testenv.Home(t)
	res := &resolve.Result{MCPServers: []resolve.MCPServer{
		{Server: upstream.Server{ID: "team/one", Source: "team"}},
		{Server: upstream.Server{ID: "local_one"}},
		{Server: upstream.Server{ID: "team/two", Source: "team"}},
	}}
	want := "✓ ctxtra added 3 MCP servers\nMCP servers:\n  - team/one\n  - local_one\n  - team/two"
	if got := agent.Summary(res); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range 31 {
		res.MCPServers = append(res.MCPServers, resolve.MCPServer{Server: upstream.Server{ID: fmt.Sprintf("server%d", i)}})
	}
	if got := agent.Summary(res); !strings.Contains(got, "  - server26\n  - +4 more\nFull MCP list: ctxtra context explain") || strings.Contains(got, "  - server27") {
		t.Fatalf("unexpected truncation: %q", got)
	}
}

// errs are n errors.
func errs(n int) []*catalog.Error {
	var out []*catalog.Error
	for i := range n {
		out = append(out, &catalog.Error{Err: &yamlx.Error{Msg: fmt.Sprintf("e%d", i)}})
	}
	return out
}

// session is what a session in h gets from the config cfg and the lock
// lockFile, without their schema lines.
func session(t *testing.T, h, cfg, lockFile string) *resolve.Result {
	t.Helper()
	path := testenv.Config(t, h, "schema: ctxtra/config/v1\n"+cfg)
	if lockFile != "" {
		testenv.Write(t, lock.Path(path), "schema: ctxtra/lock/v1\n"+lockFile)
	}
	return resolve.Load(path, registry.Names()).Resolve(resolve.Input{Dir: h})
}
