package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/mcp/netguard"
	sessionstatus "github.com/ctxtra/ctxtra/internal/mcp/status"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/term"
)

func (a *app) mcpStatusCmd() *cobra.Command {
	var agentName string
	c := &cobra.Command{
		Use:   "status [DIR]",
		Short: "Show the MCP servers of a folder, and why a session has no tools of one",
		Long: `Show the MCP servers bound to DIR (the current folder by default) with their
sign-in state, and what the last session started there got from each: its
tools in time, later, or none, and why. With --agent, for a session of that
agent: without it, bindings with agents don't apply. Reads local files only.`,
		Example: `  ctxtra mcp status
  ctxtra mcp status ~/dev/app --agent claude-code`,
		Args: atMost(1),
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			dir, _ := os.Getwd()
			if len(args) == 1 {
				dir = paths.Resolve(dir, args[0])
			}
			if abs, err := filepath.Abs(dir); err == nil {
				dir = abs
			}
			if agentName != "" {
				if _, ok := registry.Get(agentName); !ok {
					return fmt.Errorf("unknown agent %q (one of: %s)", agentName, strings.Join(registry.Names(), ", "))
				}
			}
			return code(a.mcpStatus(dir, agentName))
		}),
	}
	c.Flags().StringVar(&agentName, "agent", "", "for a session of `AGENT`, as its MCP proxy resolves it")
	return c
}

func (a *app) mcpStatus(dir, agentName string) int {
	out := term.Writer{W: a.Out}
	env := loadEnv()
	res := env.Resolve(resolve.Input{Dir: dir, Agent: agentName})
	for _, e := range res.Errors {
		if e.Err.Part.Kind == "MCP server" {
			fmt.Fprintf(out, "error: %s; see ctxtra doctor\n", e)
		}
	}
	for _, t := range res.Trace {
		if !t.Matched {
			continue
		}
		for _, e := range t.Errors {
			fmt.Fprintf(out, "error in binding %s: %s; see ctxtra doctor\n", t.Origin, e.Err)
		}
		for _, u := range t.Unavailable {
			fmt.Fprintf(out, "binding %s uses source %s, which can't be read; see ctxtra doctor\n", t.Origin, u)
		}
	}
	servers := res.MCPServers
	for _, c := range res.MCPConflicts {
		fmt.Fprintf(out, "  %s and %s are both named %s; the session gets %s, the first in binding order (binding %s)\n", c.Winner, c.Loser, c.Prefix, c.Winner, c.Origin)
	}
	if len(servers) == 0 {
		fmt.Fprintf(out, "no MCP servers are bound to %s\n", paths.Tilde(dir))
		return 0
	}
	rec, ok, err := sessionstatus.Read(dir)
	if err != nil {
		fmt.Fprintf(a.Err, "warning: can't read the last session's record: %v\n", err)
	}
	fmt.Fprintf(out, "MCP servers of %s:\n", paths.Tilde(dir))
	for _, s := range servers {
		fmt.Fprintf(out, "  %s  %s, %s\n    binding: %s; tool prefix: %s; %s\n", s.ID, shortServer(s.Server), signIn(s.Server), s.Origin, s.Name, approvalState(s.Server))
	}
	if !ok {
		fmt.Fprintln(out, "\nno session has started here with ctxtra's MCP proxy yet")
		return 0
	}
	fmt.Fprintf(out, "\nthe last session here (%s, %s):\n", rec.Agent, rec.Started.Local().Format(time.DateTime))
	own := fmt.Sprintf("; ctxtra's own start took %.2f s", float64(rec.ResolveMs)/1000)
	switch {
	case rec.ListMs > 0:
		fmt.Fprintf(out, "  first tool list after %s (waits up to %s)%s\n", seconds(rec.ListMs), seconds(rec.DeadlineMs), own)
	case rec.ResolveMs > 0:
		fmt.Fprintf(out, "  no tool list answered yet (waits up to %s)%s\n", seconds(rec.DeadlineMs), own)
	default:
		// A record of a version without the times.
		fmt.Fprintf(out, "  first tool list waits up to %s\n", seconds(rec.DeadlineMs))
	}
	late := "added with a tool list change"
	if keepsFirstList(rec.Agent) {
		late = "this session didn't get its tools"
	}
	for _, s := range rec.Servers {
		switch s.State {
		case sessionstatus.Ready:
			fmt.Fprintf(out, "  %s  ready after %s%s\n", s.Name, seconds(s.Ms), credTime(s))
		case sessionstatus.Late:
			fmt.Fprintf(out, "  %s  later: %s, after %s%s; %s\n", s.Name, s.Reason, seconds(s.Ms), credTime(s), late)
		default:
			fmt.Fprintf(out, "  %s  missing: %s%s\n", s.Name, s.Reason, credTime(s))
		}
	}
	return 0
}

// credTime is the part of a server's start that ctxtra spent reading its
// credentials, when it is worth showing.
func credTime(s sessionstatus.Server) string {
	if s.CredMs < 10 {
		return ""
	}
	return fmt.Sprintf(" (reading its credentials: %.2f s)", float64(s.CredMs)/1000)
}

func seconds(ms int64) string {
	return fmt.Sprintf("%.1f s", float64(ms)/1000)
}

// shortServer is s without what can hold a secret, a URL's path and query
// or a command's arguments: an agent runs this command, and the model
// reads its output.
func shortServer(s upstream.Server) string {
	if s.Local() {
		return "local, " + term.Sanitize(filepath.Base(s.Command[0]))
	}
	if u, err := url.Parse(s.URL); err == nil && u.Host != "" {
		return "remote, " + term.Sanitize(netguard.Origin(u))
	}
	return "remote"
}
