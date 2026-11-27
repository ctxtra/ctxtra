package cli

import (
	"context"
	"io"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	agentpkg "github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/logfile"
	"github.com/ctxtra/ctxtra/internal/mcp/proxy"
	sessionstatus "github.com/ctxtra/ctxtra/internal/mcp/status"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/resolve"
)

// started is when this process started: the proxy's deadline for the
// first tool list counts from it.
var started = time.Now()

// runProxy is ctxtra mcp proxy AGENT [ARG]..., the MCP server that setup
// adds to each agent. It ignores arguments it doesn't know, so an entry
// that a later version wrote still runs, and it never fails: the agent
// gets the tools that can be served, or none.
func runProxy(args []string, stdio IO, version string) int {
	agent := ""
	if len(args) > 0 {
		agent = args[0]
	}
	if _, ok := registry.Get(agent); !ok || !slices.Contains(args, agentpkg.SetupVersionArg) {
		return 0
	}
	dir, err := os.Getwd()
	log := func(r proxy.Record) {
		logfile.Append(proxyRecord{Ts: time.Now().UTC().Format(time.RFC3339Nano), Kind: "mcp", Harness: agent, Record: r})
	}
	// The keychain can wait for a prompt that no one sees in a session.
	store := openStore()
	store.KeyTimeout = 3 * time.Second
	o := proxy.Options{Agent: agent, Version: version, Dir: dir, Start: started, Log: log,
		Deadline: mcpDeadline(agent), Secrets: store, Tokens: store, SignInState: store.SignInState,
		KeepsFirstList: keepsFirstList(agent),
		NeedsSignIn: func(s upstream.Server) bool {
			st, err := store.State(s)
			return err == nil && st.NeedsLogin
		},
		Report: func(r sessionstatus.Record) {
			if err := sessionstatus.Write(r); err != nil {
				log(proxy.Record{Event: "error", Error: "can't write the session's status record"})
			}
		}}
	var notices []string
	o.Servers = func(dir string) ([]upstream.Server, []string) {
		res := loadEnv().Resolve(resolve.Input{Dir: dir, Agent: agent, Profiles: profile.Current()})
		for _, c := range res.MCPConflicts {
			notices = append(notices, c.Notice())
		}
		out := make([]upstream.Server, len(res.MCPServers))
		for i, s := range res.MCPServers {
			out[i] = s.Server
		}
		return out, errorTexts(res.Errors)
	}
	o.Notices = func() []string { return notices }
	if err != nil {
		o.Log(proxy.Record{Event: "error", Error: "can't read the working directory"})
		o.Servers = func(string) ([]upstream.Server, []string) { return nil, nil }
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer stop()
	// Go ends the process on SIGPIPE when stdout's reader is gone: a write
	// error instead ends the session and stops the servers. Notify, not
	// Ignore, so that the servers start with the default.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	t := &mcp.IOTransport{Reader: io.NopCloser(stdio.In), Writer: nopWriteCloser{stdio.Out}}
	if err := proxy.Run(ctx, o, t); err != nil {
		o.Log(proxy.Record{Event: "error", Error: "agent connection: " + err.Error()})
	}
	return 0
}

// proxyRecord is a proxy line in ctxtra's log.
type proxyRecord struct {
	Ts      string `json:"ts"`
	Kind    string `json:"kind"`
	Harness string `json:"harness"`
	proxy.Record
}

// keepsFirstList reports whether agent ignores tool list changes: tools
// that come after its first list reach a new session only.
func keepsFirstList(agent string) bool { return agent == "codex" || agent == "muse" }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// mcpDeadline is how long the proxy for agent waits for slow servers; the
// longest wait for an agent this version doesn't know.
func mcpDeadline(name string) time.Duration {
	if a, ok := registry.Get(name); ok {
		if m, ok := a.(agentpkg.MCPAgent); ok {
			return m.MCPDeadline()
		}
	}
	return agentpkg.MCPDeadline
}
