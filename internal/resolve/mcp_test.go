package resolve

import (
	"fmt"
	"testing"
)

// Of two servers with one name, the session gets the first in binding
// order; the other is a conflict, a notice and not an error, and is never
// served instead.
func TestMCPPrefixConflictKeepsFirstIdentity(t *testing.T) {
	h := newHome(t)
	h.write("context/ctxtra-source.yaml", "schema: ctxtra/source/v1\nmcp-servers:\n  linear: {url: https://package.example/mcp}\n  other: {command: [other-mcp]}\n")
	env := h.load(`sources:
  work: {dir: ~/context}
mcp-servers:
  linear: {url: https://config.example/mcp}
bindings:
  - {name: root, mcp-servers: [linear]}
  - {name: repeat, mcp-servers: [linear]}
  - {name: package, mcp-servers: ['work:linear', 'work:other']}
  - {name: repeat_package, mcp-servers: ['work:linear']}
`)
	res := start(env, h.dir, "")
	noErrors(t, res)
	if len(res.MCPServers) != 2 || res.MCPServers[0].ID != "linear" || res.MCPServers[1].ID != "work:other" {
		t.Fatalf("selected servers: %+v", res.MCPServers)
	}
	if len(res.MCPConflicts) != 1 || res.MCPConflicts[0].Winner != "linear" || res.MCPConflicts[0].Loser != "work:linear" {
		t.Fatalf("conflict: %+v", res.MCPConflicts)
	}
	if len(res.Notices) != 1 || res.Notices[0] != "MCP servers linear and work:linear are both named linear; the session gets linear, the first in binding order; "+
		"to get both, give work:linear a new name in the config's mcp-servers: linear_work: {use: 'work:linear'}" {
		t.Fatalf("notices: %q", res.Notices)
	}
}

// The notice about two servers with one name suggests a new name for a
// source's server, the one the session doesn't get if it can.
func TestMCPConflictNotice(t *testing.T) {
	const first = "MCP servers %s and %s are both named jira; the session gets %s, the first in binding order; to get both, "
	for _, c := range []struct {
		name string
		c    MCPConflict
		want string
	}{
		{"two servers named jira", MCPConflict{"jira", "iam-team:jira", "jira", "b"},
			"give iam-team:jira a new name in the config's mcp-servers: jira_iam_team: {use: 'iam-team:jira'}"},
		{"the config's server second", MCPConflict{"iam-team:jira", "jira", "jira", "b"},
			"give iam-team:jira a new name in the config's mcp-servers: jira_iam_team: {use: 'iam-team:jira'}"},
		{"already renamed", MCPConflict{"jira", "obs-team:tracker", "jira", "b"},
			"change the config's name jira for obs-team:tracker in mcp-servers"},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := fmt.Sprintf(first, c.c.Winner, c.c.Loser, c.c.Winner) + c.want
			if got := c.c.Notice(); got != want {
				t.Errorf("notice:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}
