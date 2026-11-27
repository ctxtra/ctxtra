package muse

import (
	"os"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/profile"
)

func (Agent) SessionEvent(event string, raw []byte) profile.Event {
	e := agent.HookSession(event, raw)
	if e.Kind == profile.Subagent {
		e.Parent = os.Getenv("MUSE_SESSION_ID")
		if e.Parent == e.Session {
			e.Parent = ""
		}
	}
	return e
}
