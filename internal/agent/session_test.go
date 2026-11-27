package agent_test

import (
	"slices"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestAgentsIdentifyTheirSessions(t *testing.T) {
	testenv.Home(t)
	for _, c := range []struct {
		agent, scenario, event, input string
		kind                          profile.Kind
		session, parent               string
	}{
		{"github-copilot", "stored selection", "sessionStart", `{"sessionId":"p","source":"new"}`, profile.Start, "p", ""},
		{"github-copilot", "switching while a session runs", "subagentStart", `{"sessionId":"p","agentId":"c","parentAgentId":"wrong"}`, profile.Subagent, "p", "p"},
		{"github-copilot", "resume", "sessionStart", `{"sessionId":"p","source":"resume"}`, profile.Continue, "p", ""},
		{"github-copilot", "clear starts over", "sessionStart", `{"sessionId":"p","source":"clear"}`, profile.Start, "p", ""},
		{"antigravity", "stored selection", "SessionStart", `{"conversationId":"p"}`, profile.Start, "p", ""},
		{"antigravity", "a subagent without a parent", "SessionStart", `{"conversationId":"c","session_id":"wrong"}`, profile.Start, "c", ""},
		{"antigravity", "compaction", "SessionStart", `{"conversationId":"p","source":"compact"}`, profile.Continue, "p", ""},
		{"muse", "stored selection", "SessionStart", `{"session_id":"p","source":"startup"}`, profile.Start, "p", ""},
		{"muse", "compaction", "SessionStart", `{"session_id":"p","source":"compact"}`, profile.Continue, "p", ""},
		{"muse", "resume", "SessionStart", `{"session_id":"p","source":"resume"}`, profile.Continue, "p", ""},
		{"muse", "clear starts over", "SessionStart", `{"session_id":"p","source":"clear"}`, profile.Start, "p", ""},
		{"opencode", "stored selection", "context", `{"session_id":"p"}`, profile.Continue, "p", ""},
		{"opencode", "switching while a session runs", "subagent", `{"session_id":"c","ctxtra_parent_session":"p"}`, profile.Continue, "c", "p"},
		{"hermes-agent", "stored selection", "pre_llm_call", `{"session_id":"p","ctxtra_parent_session":""}`, profile.Start, "p", ""},
		{"hermes-agent", "switching while a session runs", "subagent", `{"session_id":"c","ctxtra_parent_session":"p"}`, profile.Subagent, "c", "p"},
		{"dsh", "stored selection", "startup", `{"session_id":"p"}`, profile.Start, "p", ""},
		{"dsh", "switching while a session runs", "subagent", `{"session_id":"c","ctxtra_parent_session":"p"}`, profile.Subagent, "c", "p"},
		{"dsh", "compaction", "compact", `{"session_id":"c","ctxtra_parent_session":"p"}`, profile.Continue, "c", ""},
	} {
		t.Run(c.agent+"/"+c.scenario, func(t *testing.T) {
			a, _ := registry.Get(c.agent)
			s, ok := a.(agent.Sessions)
			if !ok {
				t.Fatal("agent doesn't identify its sessions")
			}
			if got := s.SessionEvent(c.event, []byte(c.input)); got.Kind != c.kind || got.Session != c.session || got.Parent != c.parent {
				t.Errorf("%+v, want kind %d session %q parent %q", got, c.kind, c.session, c.parent)
			}
			if got := s.SessionEvent(c.event, []byte(`{}`)); got.Session != "" || got.Parent != "" {
				t.Errorf("agent without a session ID: %+v", got)
			}
		})
	}
}

func TestMuseUsesTheNativeParent(t *testing.T) {
	testenv.Home(t)
	a, _ := registry.Get("muse")
	s := a.(agent.Sessions)
	for _, parent := range []string{"p", "", "c"} {
		t.Run("parent="+parent, func(t *testing.T) {
			t.Setenv("MUSE_SESSION_ID", parent)
			e := s.SessionEvent("SubagentStart", []byte(`{"session_id":"c","child_session_id":"c","subagent_id":"other"}`))
			want := parent
			if want == "c" {
				want = ""
			}
			if e.Kind != profile.Subagent || e.Session != "c" || e.Parent != want {
				t.Errorf("%+v, want child c parent %q", e, want)
			}
		})
	}
}

func TestCompactionWithoutARecordUsesTheCurrentSelection(t *testing.T) {
	testenv.Home(t)
	if err := profile.Store([]string{"iam"}); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.ForEvent(profile.Event{Agent: "dsh", Kind: profile.Start, Session: "p"}); err != nil {
		t.Fatal(err)
	}
	if err := profile.Store([]string{"observability"}); err != nil {
		t.Fatal(err)
	}
	a, _ := registry.Get("dsh")
	e := a.(agent.Sessions).SessionEvent("compact", []byte(`{"session_id":"c","ctxtra_parent_session":"p"}`))
	e.Agent = a.Name()
	active, err := profile.ForEvent(e)
	if err != nil || !slices.Equal(active.Names, []string{"observability"}) || active.Kept {
		t.Errorf("%+v %v", active, err)
	}
}
