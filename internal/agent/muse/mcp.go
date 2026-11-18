package muse

import (
	"time"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/mcpentry"
)

var _ agent.MCPAgent = Agent{}

// MCP shares the hook settings file. An optional server cannot prevent
// Muse from starting when its connection fails.
func (a Agent) MCP() agent.MCPHost {
	return mcpentry.Config{Agent: a.Name(), Path: a.ConfigPath(), Servers: []string{"mcpServers"},
		Extra:    []mcpentry.KV{{Key: "type", Value: "stdio"}, {Key: "mode", Value: "optional"}},
		Env:      []string{"CTXTRA_LOG", "CTXTRA_NO_UPDATE_CHECK"},
		EnvPaths: []string{"CTXTRA_LOG"},
		Defaults: []mcpentry.KV{{Key: "schema_version", Value: 1}}}
}

func (Agent) MCPDeadline() time.Duration { return agent.MCPDeadline }
