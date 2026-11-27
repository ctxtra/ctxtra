package opencode

import (
	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/profile"
)

func (Agent) SessionEvent(event string, raw []byte) profile.Event {
	e := agent.PluginSession(event, raw)
	// Context callbacks don't distinguish a child's start from its resume
	// or compaction. Its own record takes priority when it already exists.
	if e.Kind == profile.Subagent {
		e.Kind = profile.Continue
	}
	return e
}
