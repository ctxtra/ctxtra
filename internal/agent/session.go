package agent

import (
	"encoding/json"

	"github.com/ctxtra/ctxtra/internal/profile"
)

// Sessions is an agent whose hook input tells which session an event
// belongs to, so that the session keeps the profiles it started with. The
// hook gives an agent without it the current profiles at every event.
type Sessions interface {
	// SessionEvent is event's place in its session, from the hook input
	// raw (which may be empty). Agent is set by the hook.
	SessionEvent(event string, raw []byte) profile.Event
}

// HookSession is SessionEvent for the hook input that Claude Code and Codex
// send: session_id, and source at SessionStart. A subagent's session_id is
// its parent's, so a SubagentStart takes and keeps the parent's record.
// Events of ctxtra's plugin take the session's record.
func HookSession(event string, raw []byte) profile.Event {
	var in struct {
		SessionID string `json:"session_id"`
		Source    string `json:"source"`
	}
	_ = json.Unmarshal(raw, &in)
	e := profile.Event{Kind: profile.Continue, Session: in.SessionID}
	switch {
	case event == "SubagentStart":
		e.Kind, e.Parent = profile.Subagent, in.SessionID
	case event == "SessionStart" && (in.Source == "startup" || in.Source == "clear"):
		e.Kind = profile.Start
	}
	return e
}

// PluginSession reads the native session identities forwarded by ctxtra's
// OpenCode, Hermes and DSH plugins.
func PluginSession(event string, raw []byte) profile.Event {
	var in struct {
		Session string `json:"session_id"`
		Parent  string `json:"ctxtra_parent_session"`
	}
	_ = json.Unmarshal(raw, &in)
	e := profile.Event{Kind: profile.Continue, Session: in.Session}
	switch event {
	case "startup", "clear", "pre_llm_call":
		e.Kind = profile.Start
	case "subagent":
		e.Kind, e.Parent = profile.Subagent, in.Parent
	}
	return e
}
