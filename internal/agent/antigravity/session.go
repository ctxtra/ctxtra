package antigravity

import (
	"encoding/json"

	"github.com/ctxtra/ctxtra/internal/profile"
)

func (Agent) SessionEvent(event string, raw []byte) profile.Event {
	var in struct {
		Session string `json:"conversationId"`
		Source  string `json:"source"`
	}
	_ = json.Unmarshal(raw, &in)
	e := profile.Event{Kind: profile.Continue, Session: in.Session}
	if event == "SessionStart" && (in.Source == "" || in.Source == "startup" || in.Source == "clear") {
		e.Kind = profile.Start
	}
	return e
}
