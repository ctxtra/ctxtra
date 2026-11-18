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
	"github.com/ctxtra/ctxtra/internal/term"
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
		RunE: a.run(func(_ *cobra.Command, args []string) error {
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
	out := term.Writer{W: a.Out}
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
				fmt.Fprintf(out, "unavailable MCP reference %q in binding %s; see ctxtra doctor\n", ref, binding.ID())
			}
		}
	}
	servers := res.MCPServers
	for _, conflict := range res.MCPConflicts {
		fmt.Fprintf(out, "  %s shadowed by %s (tool prefix %s, binding %s); no fallback\n", conflict.Loser, conflict.Winner, conflict.Prefix, conflict.Origin)
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
