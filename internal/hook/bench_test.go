package hook

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// BenchmarkHook is a whole hook run, as Codex runs it: a session start,
// then a subagent start of that session.
func BenchmarkHook(b *testing.B) {
	h := testenv.Home(b)
	b.Setenv("CTXTRA_NO_UPDATE_CHECK", "1")
	dir := filepath.Join(h, "dev", "svc")
	testenv.Write(b, filepath.Join(dir, "main.go"), "package main\n")
	testenv.Write(b, filepath.Join(h, "a.md"), strings.Repeat("Rule A.\n", 500))
	testenv.Write(b, filepath.Join(h, "b.md"), strings.Repeat("Rule B.\n", 500))
	testenv.Config(b, h, `schema: ctxtra/config/v1
bindings:
  - {name: a, instructions: [~/a.md]}
  - {name: b, location: {dirs: ['~/dev/**']}, profiles: [iam], instructions: [~/b.md]}
`)
	input := `{"cwd": "` + dir + `", "session_id": "s1", "source": "startup"}`
	for _, event := range []string{"SessionStart", "SubagentStart"} {
		b.Run(event, func(b *testing.B) {
			for b.Loop() {
				Run("codex", event, strings.NewReader(input), io.Discard, io.Discard)
			}
		})
	}
}
