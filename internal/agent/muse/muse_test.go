package muse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/jsonorder"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// A long summary is cut, so the answer stays within what Muse takes.
func TestClip(t *testing.T) {
	if got := clip("ctxtra: added 1 file (a.md)"); got != "ctxtra: added 1 file (a.md)" {
		t.Errorf("short summary: %q", got)
	}
	long := strings.Repeat("ctxtra: \"source\" last checked 9 days ago; run ctxtra source update\n", 100)
	got := clip(long)
	if b, _ := jsonorder.Marshal(got); len(b)-2 > maxSummary || !strings.HasSuffix(got, "…") || !strings.HasPrefix(long, strings.TrimSuffix(got, "…")) {
		t.Errorf("%d bytes of JSON: %q", len(b)-2, got)
	}
}

func TestLongSummaryLists(t *testing.T) {
	res := &resolve.Result{Notices: []string{"source needs checking"}, Skills: []resolve.Hit{{Name: "deploy", Source: "team"}}, Summary: resolve.DefaultSessionMessage}
	for i := 0; i < 16; i++ {
		res.Files = append(res.Files, resolve.Hit{Path: "/notes/" + strings.Repeat("a", 140) + ".md"})
	}
	res.Files = append(res.Files, resolve.Hit{Source: "team", SourcePath: "short.md"})
	a := Agent{}
	for _, answer := range []any{a.Answer("SessionStart", res), a.SlotAnswer("SessionStart", "context", agent.Summary(res))} {
		got := answer.(map[string]any)["systemMessage"].(string)
		b, _ := jsonorder.Marshal(got)
		if len(b)-2 > maxSummary || !strings.Contains(got, "\n  team\n  - short.md\nSkills:\n  team\n  - deploy\n"+agent.FullList+"\n\nℹ source needs checking") ||
			!strings.Contains(got, " more\n  team\n") || strings.HasSuffix(got, "…") {
			t.Fatalf("lost summary information (%d JSON bytes): %q", len(b)-2, got)
		}
	}
}

// Long paths of same-named files don't hide the problems: those in a list
// are omitted first, and dropped ones come after the count of problems.
func TestLongPathsKeepErrors(t *testing.T) {
	long := func(i int) resolve.Hit {
		return resolve.Hit{Path: fmt.Sprintf("/%d/%s/style.md", i, strings.Repeat("é", 400)), Over: "budget.max-chars"}
	}
	res := &resolve.Result{Errors: []*catalog.Error{{Err: &yamlx.Error{Msg: "p"}}}, Notices: []string{"source needs checking"}, Summary: resolve.DefaultSessionMessage}
	for i := range 15 {
		h := long(i)
		h.Over = ""
		res.Files = append(res.Files, h)
	}
	if got := clip(agent.Summary(res)); !strings.Contains(got, "1 error (run ctxtra doctor)") || !strings.Contains(got, "\n\nℹ source needs checking") {
		t.Errorf("long list: %q", got)
	}
	res.Dropped = []resolve.Hit{long(20), long(21), long(22)}
	got := clip(agent.Summary(res))
	if b, _ := jsonorder.Marshal(got); len(b)-2 > maxSummary || !strings.Contains(got, "; 1 error, dropped /20/") {
		t.Errorf("long dropped names (%d JSON bytes): %q", len(b)-2, got)
	}
}

func TestLiteralOverflowName(t *testing.T) {
	res := &resolve.Result{Summary: resolve.DefaultSessionMessage}
	for i := 0; i < 14; i++ {
		res.Files = append(res.Files, resolve.Hit{Source: "notes", SourcePath: strings.Repeat("a", 140) + ".md"})
	}
	res.Files = append(res.Files, resolve.Hit{Source: "notes", SourcePath: "+100 more"})
	got := clip(agent.Summary(res))
	if !strings.Contains(got, "\n  - \"+100 more\"") {
		t.Fatalf("lost literal filename: %q", got)
	}
	listed, missing := 0, 0
	for _, line := range strings.Split(got, "\n") {
		if n, ok := omitted(line); ok {
			missing += n
		} else if strings.HasPrefix(line, agent.EntryPrefix) {
			listed++
		}
	}
	if listed+missing != 15 || missing == 0 {
		t.Fatalf("listed %d, omitted %d: %q", listed, missing, got)
	}
}

// Many groups with long names: whole groups are omitted, counted in one line
// per list, before the end is cut, so the notices stay. Also when the lists
// show one entry of each group.
func TestManyLongGroups(t *testing.T) {
	counts := resolve.DefaultSessionMessage
	counts.MaxInstructions, counts.MaxSkills = 1, 1
	for _, s := range []resolve.SessionMessage{resolve.DefaultSessionMessage, counts} {
		res := &resolve.Result{Errors: []*catalog.Error{{Err: &yamlx.Error{Msg: "p"}}}, Notices: []string{"team has new commits on main"}, Summary: s}
		for i := range 20 {
			src := fmt.Sprintf("%02d-%s", i, strings.Repeat("s", 100))
			res.Files = append(res.Files, resolve.Hit{Source: src, SourcePath: "a.md"}, resolve.Hit{Source: src, SourcePath: "b.md"})
			res.Skills = append(res.Skills, resolve.Hit{Source: src, Name: "deploy"})
		}
		got := clip(agent.Summary(res))
		b, _ := jsonorder.Marshal(got)
		if len(b)-2 > maxSummary || strings.HasSuffix(got, "…") || !strings.Contains(got, "; 1 error (run ctxtra doctor)\n") ||
			!strings.HasSuffix(got, "\n"+agent.FullList+"\n\nℹ team has new commits on main") {
			t.Fatalf("%+v: lost summary information (%d JSON bytes): %q", s, len(b)-2, got)
		}
		// Every entry is still counted, in its list.
		total := map[string]int{}
		section := ""
		for _, line := range strings.Split(got, "\n") {
			switch {
			case line == "Instructions:" || line == "Skills:":
				section = line
			case strings.HasPrefix(line, agent.EntryPrefix):
				n, ok := omitted(line)
				if !ok {
					n = 1
				}
				total[section] += n
			default:
				if n, _, ok := omittedGroups(line); ok {
					total[section] += n
				} else if strings.HasPrefix(line, agent.GroupIndent) {
					total[section] += counted(line)
				}
			}
		}
		if total["Instructions:"] != 40 || total["Skills:"] != 20 || !strings.Contains(got, " more from ") {
			t.Errorf("%+v: counts %v: %q", s, total, got)
		}
	}
}

func TestOmitGroup(t *testing.T) {
	lines := strings.Split("✓ x\nInstructions:\n  aa\n  - a.md\n  - +2 more\n  b\n  - b.md\nSkills:\n  c\n  - c\n\nℹ n", "\n")
	got, ok := omitGroup(lines)
	want := "✓ x\nInstructions:\n  b\n  - b.md\n  +3 more from 1 source\nSkills:\n  c\n  - c\n\nℹ n"
	if !ok || strings.Join(got, "\n") != want {
		t.Fatalf("first: %q", strings.Join(got, "\n"))
	}
	got, _ = omitGroup(got)
	if want := "✓ x\nInstructions:\n  +4 more from 2 sources\nSkills:\n  c\n  - c\n\nℹ n"; strings.Join(got, "\n") != want {
		t.Fatalf("second: %q", strings.Join(got, "\n"))
	}
	// A group of a list that shows only numbers.
	lines = strings.Split("✓ x\nInstructions:\n  aa (3)\n  b (1)\n\nℹ n", "\n")
	got, _ = omitGroup(lines)
	if want := "✓ x\nInstructions:\n  b (1)\n  +3 more from 1 source\n\nℹ n"; strings.Join(got, "\n") != want {
		t.Fatalf("numbers: %q", strings.Join(got, "\n"))
	}
}

func TestCounted(t *testing.T) {
	for line, want := range map[string]int{"  a (3)": 3, "  a-b.c (12)": 12, "  a": 0, "  a ()": 0, "  a (x)": 0, "  a (-1)": 0, "  +3 more from 1 source": 0} {
		if got := counted(line); got != want {
			t.Errorf("%q: %d", line, got)
		}
	}
}

func TestMCPListClipping(t *testing.T) {
	lines := strings.Split("✓ x\nMCP servers:\n  team\n  - one\n  - two\n  local\n  - three\n\n⚠ sign in", "\n")
	got, ok := omitGroup(lines)
	want := "✓ x\nMCP servers:\n  team\n  - one\n  - two\n  +1 more from 1 source\n\n⚠ sign in"
	if !ok || strings.Join(got, "\n") != want {
		t.Fatalf("got %q, want %q", strings.Join(got, "\n"), want)
	}
}

func TestLongMCPSummary(t *testing.T) {
	testenv.Home(t)
	res := &resolve.Result{Notices: []string{"check source"}}
	source := strings.Repeat("a", 100)
	for i := range 20 {
		res.MCPServers = append(res.MCPServers, resolve.MCPServer{Server: upstream.Server{ID: fmt.Sprintf("%s/server%d", source, i), Source: source}})
	}
	a := Agent{}
	for _, answer := range []any{a.Answer("SessionStart", res), a.SlotAnswer("SessionStart", "context", agent.Summary(res))} {
		got := answer.(map[string]any)["systemMessage"].(string)
		if chunkLen := lenMustJSON(t, got); chunkLen > maxSummary {
			t.Fatalf("summary exceeds cap: %d", chunkLen)
		}
		if !strings.HasPrefix(got, "✓ ctxtra added 20 MCP servers\nMCP servers:") || !strings.Contains(got, agent.FullMCPList) || !strings.HasSuffix(got, "\n\nℹ check source") || !strings.Contains(got, " more") {
			t.Fatalf("lost MCP summary information: %q", got)
		}
		shown, omitted := 0, 0
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(line, agent.EntryPrefix+source+"/") {
				shown++
			}
			if strings.HasPrefix(line, agent.EntryPrefix+"+") {
				_, _ = fmt.Sscanf(line, "  - +%d more", &omitted)
			}
		}
		if shown+omitted != 20 {
			t.Fatalf("shown %d + omitted %d != 20: %q", shown, omitted, got)
		}
	}
}

func lenMustJSON(t *testing.T, value string) int {
	t.Helper()
	b, err := jsonorder.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return len(b) - 2
}
