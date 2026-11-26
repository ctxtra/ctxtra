package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// Every error line of the session message stays one line of bounded
// length, without control characters, and keeps its repair command; doctor
// and the log keep the whole text.
func TestSessionMessageErrorLines(t *testing.T) {
	h := testenv.Home(t)
	logPath := filepath.Join(h, "log.jsonl")
	t.Setenv("CTXTRA_LOG", logPath)
	svc := testenv.Repo(t, filepath.Join(h, "svc"), "git@github.com:acme/svc.git")
	team := testenv.Repo(t, filepath.Join(h, "a\x1b[2Jb\nc"), "git@github.com:acme/team.git")
	long := strings.Repeat("p", 5000)
	t.Setenv("CTXTRA_PROFILES", long)
	binding, set := "b"+strings.Repeat("x", 63), "s"+strings.Repeat("y", 63)
	testenv.Config(t, h, `schema: ctxtra/config/v1
sources:
  team: {dir: "~/a\e[2Jb\nc"}
bindings:
  - {name: `+binding+`, location: {repo-sets: [`+set+`]}, instructions: [team:AGENTS.md]}
`)
	if _, err := os.Stat(team); err != nil {
		t.Fatal(err)
	}
	in, _ := json.Marshal(map[string]string{"cwd": svc, "session_id": "s", "source": "startup"})
	var out, errOut strings.Builder
	if n := Run([]string{"hook", "claude-code", "SessionStart", "--slot", "0", "--of", "40", "--setup-version=1"}, IO{In: strings.NewReader(string(in)), Out: &out, Err: &errOut}, "test"); n != 0 {
		t.Fatalf("hook: %d %s", n, errOut.String())
	}
	var answer struct{ SystemMessage string }
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	_, lines, ok := strings.Cut(answer.SystemMessage, "\n\n")
	if !ok {
		t.Fatalf("no error lines: %s", out.String())
	}
	var named []string
	for _, l := range strings.Split(lines, "\n") {
		named = append(named, l)
		if strings.ContainsFunc(l, func(r rune) bool { return r < ' ' || r == 0x7f }) || utf8.RuneCountInString(l) > 2+240 {
			t.Errorf("not one short line: %q", l)
		}
	}
	for i, want := range []string{"⚠ CTXTRA_PROFILES: \"ppp", "⚠ source team: ~/a\\u001b[2Jb c is in a repository", "⚠ binding " + binding + ": repo set " + set + " can't be read"} {
		if i >= len(named) || !strings.HasPrefix(named[i], want) {
			t.Errorf("line %d: want %q in\n%s", i, want, answer.SystemMessage)
		}
	}
	for i, end := range []string{"…; the entry is left out", "… run ctxtra source trust team; to read reviewed commits, use a repo source: ctxtra source add --repo ~/a\\u001b[2Jb c", "…; the entry matches nothing"} {
		if i < len(named) && !strings.HasSuffix(named[i], end) {
			t.Errorf("line %d: want it to end with %q: %q", i, end, named[i])
		}
	}
	code, doctor, _ := run("doctor", svc)
	if code != 1 || !strings.Contains(doctor, long) || !strings.Contains(doctor, "ctxtra source trust team") || !strings.Contains(doctor, "repo set "+set) {
		t.Errorf("doctor lost the whole text: %d\n%.2000s", code, doctor)
	}
	if b, err := os.ReadFile(logPath); err != nil || !strings.Contains(string(b), long) {
		t.Errorf("the log lost the whole text: %v", err)
	}
}
