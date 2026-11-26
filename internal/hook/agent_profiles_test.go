package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"testing"
)

func TestHookSwitchingWhileASessionRunsInEachAgent(t *testing.T) {
	for _, c := range []struct {
		agent, start, child, id, source string
		inherits                        bool
	}{
		{"github-copilot", "sessionStart", "subagentStart", "sessionId", "new", true},
		{"muse", "SessionStart", "SubagentStart", "session_id", "startup", true},
		{"opencode", "context", "subagent", "session_id", "", true},
		{"hermes-agent", "pre_llm_call", "subagent", "session_id", "", true},
		{"dsh", "startup", "subagent", "session_id", "", true},
		{"antigravity", "SessionStart", "SessionStart", "conversationId", "", false},
	} {
		t.Run(c.agent, func(t *testing.T) {
			h, dir := profileSession(t)
			original := endLock
			endLock = func(unlock func()) { unlock() }
			t.Cleanup(func() { endLock = original })
			run := func(event string, fields map[string]any) []string {
				t.Helper()
				fields["cwd"], fields["ctxtra_plugin"] = dir, 4
				if c.agent == "hermes-agent" {
					fields["ctxtra_slots"] = 125
				}
				in, _ := json.Marshal(fields)
				var out bytes.Buffer
				if n := RunSlot(c.agent, event, 0, bytes.NewReader(in), &out, io.Discard); n != 0 {
					t.Fatalf("hook: %d %s", n, out.String())
				}
				return rules(out.String())
			}
			setSelection(t, "iam")
			if got := run(c.start, map[string]any{c.id: "p", "source": c.source}); !slices.Equal(got, []string{"IAM-RULES"}) {
				t.Fatalf("start: %v", got)
			}
			setSelection(t, "observability")
			child := "c"
			if c.agent == "github-copilot" {
				child = "p"
			}
			t.Setenv("MUSE_SESSION_ID", "p")
			want := "IAM-RULES"
			if !c.inherits {
				want = "OBS-RULES"
			}
			if got := run(c.child, map[string]any{c.id: child, "ctxtra_parent_session": "p"}); !slices.Equal(got, []string{want}) {
				t.Errorf("subagent: %v, want %s", got, want)
			}
			if e := lastEntry(t, h); e.ProfilesKept != c.inherits {
				t.Errorf("kept profiles in the log: %+v", e)
			}
			// With no native IDs, even a subagent uses the current selection.
			t.Setenv("MUSE_SESSION_ID", "")
			if got := run(c.child, map[string]any{}); !slices.Equal(got, []string{"OBS-RULES"}) {
				t.Errorf("agent without a session ID: %v", got)
			}
		})
	}
}
