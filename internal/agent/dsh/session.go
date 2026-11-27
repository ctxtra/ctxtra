package dsh

import (
	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/profile"
)

func (Agent) SessionEvent(event string, raw []byte) profile.Event {
	return agent.PluginSession(event, raw)
}
