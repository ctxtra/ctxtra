package agent

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// No error line of the session message is ever longer than maxErrorLine
// or has a control character, whatever the names, messages and files.
func TestSummaryErrorLinesAreBounded(t *testing.T) {
	testenv.Home(t)
	r := rand.New(rand.NewPCG(1, 2))
	lengths := []int{0, 1, 20, 39, 40, 41, 64, 100, 119, 120, 121, 200, 239, 240, 241, 300, 5000}
	pieces := []string{"a", "é", "界", "\x1b", "\n", "\r", "\t", "`", " ", ":", "\x00", "\u202e"}
	str := func() string {
		var b strings.Builder
		for range lengths[r.IntN(len(lengths))] {
			if r.IntN(4) == 0 {
				b.WriteString(pieces[r.IntN(len(pieces))])
			} else {
				b.WriteByte('a' + byte(r.IntN(26)))
			}
		}
		return b.String()
	}
	kinds := []string{"", "source", "binding", "MCP server", "key"}
	for range 2000 {
		res := &resolve.Result{}
		for range 1 + r.IntN(5) {
			part := yamlx.Part{Kind: kinds[r.IntN(len(kinds))], Name: str()}
			msg := str()
			mistake := yamlx.Mistake(1 + r.IntN(int(yamlx.UnusedProfile)))
			if r.IntN(3) == 0 {
				mistake, part.Kind = yamlx.Inactive, "source"
				cmd := []string{"trust", "update"}[r.IntN(2)]
				msg += "; run `ctxtra source " + cmd + " " + part.Name + "`" + str()
			}
			e := &catalog.Error{File: str(), Err: &yamlx.Error{Pos: yamlx.Pos{Line: r.IntN(100000)}, Mistake: mistake, Part: part, Msg: msg}}
			res.Errors = append(res.Errors, e)
			if r.IntN(3) == 0 {
				entry := str()
				if r.IntN(2) == 0 {
					entry = "!" + entry
				}
				res.UnreadableSets = append(res.UnreadableSets, resolve.UnreadableSet{Binding: str(), Entry: entry, Why: str(), Err: e})
			}
		}
		for range r.IntN(3) {
			s := upstream.Server{ID: str() + ":" + str(), Command: []string{"srv"}}
			if r.IntN(2) == 0 {
				s.Command, s.URL = nil, "https://mcp.example.com"
			}
			res.MCPServers = append(res.MCPServers, resolve.MCPServer{Server: s, Unapproved: "new"})
		}
		_, lines, ok := strings.Cut(Summary(res), "\n\n")
		if !ok {
			continue
		}
		for _, l := range strings.Split(lines, "\n") {
			l, ok := strings.CutPrefix(l, "⚠ ")
			if !ok {
				t.Fatalf("not an error line: %q", l)
			}
			if utf8.RuneCountInString(l) > maxErrorLine || strings.ContainsFunc(l, func(r rune) bool { return r < ' ' || r == 0x7f }) {
				t.Fatalf("%d characters: %q", utf8.RuneCountInString(l), l)
			}
		}
	}
}

// Diagnostic keeps every line within maxErrorLine, tails included.
func FuzzDiagnostic(f *testing.F) {
	f.Add("text", "; the binding is left out", "")
	f.Add(strings.Repeat("b", 5000), "; binding "+strings.Repeat("b", 5000)+" is left out", "; the binding is left out")
	f.Add("x", strings.Repeat("é", 300), strings.Repeat("\x1b", 300))
	f.Fuzz(func(t *testing.T, text, tail, short string) {
		if l := Diagnostic(text, tail, short); utf8.RuneCountInString(l) > maxErrorLine {
			t.Errorf("%d characters: %q", utf8.RuneCountInString(l), l)
		}
	})
}

// A long tail gives way to the shorter one, so that the line keeps minText
// characters of text; a tail that fits is kept whole.
func TestDiagnosticTails(t *testing.T) {
	long := "; binding " + strings.Repeat("b", 300) + " is left out"
	if got := Diagnostic("binding x: bad", long, "; the binding is left out"); got != "binding x: bad; the binding is left out" {
		t.Errorf("short tail: %q", got)
	}
	tail := "; binding " + strings.Repeat("b", 150) + " is left out"
	if got := Diagnostic(strings.Repeat("t", 300), tail, "; the binding is left out"); !strings.HasSuffix(got, "t…"+tail) || utf8.RuneCountInString(got) != maxErrorLine {
		t.Errorf("long tail that fits: %q", got)
	}
}
