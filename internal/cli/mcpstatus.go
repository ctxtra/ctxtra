package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/ctxtra/ctxtra/internal/mcp/netguard"
	sessionstatus "github.com/ctxtra/ctxtra/internal/mcp/status"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
)

func (a *app) mcpStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [DIR]",
		Short: "Show the MCP servers of a folder, and why a session has no tools of one",
		Long: `Show the MCP servers bound to DIR (the current folder by default) with their
sign-in state, and what the last session started there got from each: its
tools in time, later, or none, and why. Reads local files only.`,
		Example: `  ctxtra mcp status
  ctxtra mcp status ~/dev/app`,
		Args: atMost(1),
		RunE: a.mcpRun(func(_ *cobra.Command, args []string) error {
			dir, _ := os.Getwd()
			if len(args) == 1 {
				dir = paths.Resolve(dir, args[0])
			}
			if abs, err := filepath.Abs(dir); err == nil {
				dir = abs
			}
			return code(a.mcpStatus(dir))
		}),
	}
}

func (a *app) mcpStatus(dir string) int {
	res, cfg := resolve.Session(dir, paths.ConfigPath())
	for i, binding := range cfg.Bindings {
		if !res.Trace[i].Matched {
			continue
		}
		for _, ref := range binding.MCPListed {
			id := ref
			if binding.MCPScope != "" {
				id = binding.MCPScope + "/" + ref
			}
			if !slices.Contains(binding.MCPServers, id) {
				fmt.Fprintf(a.Out, "unavailable MCP reference %q in binding %s; see ctxtra doctor\n", ref, binding.ID())
			}
		}
	}
	servers := res.MCPServers
	for _, conflict := range res.MCPConflicts {
		fmt.Fprintf(a.Out, "  %s shadowed by %s (tool prefix %s, binding %s); no fallback\n", conflict.Loser, conflict.Winner, conflict.Prefix, conflict.Origin)
	}
	if len(servers) == 0 {
		fmt.Fprintf(a.Out, "no MCP servers are bound to %s\n", paths.Tilde(dir))
		return 0
	}
	rec, ok, err := sessionstatus.Read(dir)
	if err != nil {
		fmt.Fprintf(a.Err, "warning: can't read the last session's record: %v\n", err)
	}
	fmt.Fprintf(a.Out, "MCP servers of %s:\n", paths.Tilde(dir))
	for _, s := range servers {
		fmt.Fprintf(a.Out, "  %s  %s, %s\n    binding: %s; tool prefix: %s; %s\n", s.ID, shortServer(s.Server), signIn(s.Server), s.Origin, s.Name, approvalState(s.Server))
	}
	if !ok {
		fmt.Fprintln(a.Out, "\nno session has started here with ctxtra's MCP server yet")
		return 0
	}
	fmt.Fprintf(a.Out, "\nthe last session here (%s, %s) waited %s for the first tool list:\n",
		rec.Agent, rec.Started.Local().Format(time.DateTime), seconds(rec.DeadlineMs))
	for _, s := range rec.Servers {
		switch s.State {
		case sessionstatus.Ready:
			fmt.Fprintf(a.Out, "  %s  ready after %s\n", s.Name, seconds(s.Ms))
		case sessionstatus.Late:
			fmt.Fprintf(a.Out, "  %s  later: %s, after %s; Codex sessions didn't get its tools\n", s.Name, s.Reason, seconds(s.Ms))
		default:
			fmt.Fprintf(a.Out, "  %s  missing: %s\n", s.Name, s.Reason)
		}
	}
	return 0
}

func seconds(ms int64) string {
	return fmt.Sprintf("%.1f s", float64(ms)/1000)
}

// shortServer is s without what can hold a secret, a URL's path and query
// or a command's arguments: an agent runs this command, and the model
// reads its output.
func shortServer(s upstream.Server) string {
	if s.Local() {
		return "local, " + filepath.Base(s.Command[0])
	}
	if u, err := url.Parse(s.URL); err == nil && u.Host != "" {
		return "remote, " + netguard.Origin(u)
	}
	return "remote"
}
