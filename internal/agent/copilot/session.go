package copilot

import (
	"encoding/json"

	"github.com/ctxtra/ctxtra/internal/profile"
)

func (Agent) SessionEvent(event string, raw []byte) profile.Event {
	var in struct {
		Session string `json:"sessionId"`
		Source  string `json:"source"`
	}
	_ = json.Unmarshal(raw, &in)
	e := profile.Event{Kind: profile.Continue, Session: in.Session}
	switch {
	case event == "subagentStart":
		e.Kind, e.Parent = profile.Subagent, in.Session
	case event == "sessionStart" && (in.Source == "new" || in.Source == "startup" || in.Source == "clear"):
		e.Kind = profile.Start
	}
	return e
}
