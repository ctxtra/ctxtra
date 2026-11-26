package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

func (a *app) removeMCPLeftover(ref string, dryRun bool) error {
	if err := checkServerRef(ref); err != nil {
		return err
	}
	return withLock(func() error {
		env := loadEnv()
		scope, server, _ := strings.Cut(ref, ":")
		src := env.Catalog.Source(scope)
		if src != nil {
			m, errs := env.Catalog.Manifest(src)
			switch {
			case src.State != catalog.Active || len(errs) > 0:
				return fmt.Errorf("cannot establish that %s is undeclared while source %s can't be read or its manifest has errors; fix it before cleanup", ref, scope)
			case m != nil && m.MCPServer(server) != nil:
				return fmt.Errorf("%s is declared by the source %s; change its bindings or remove the source instead", ref, scope)
			}
		}
		if env.Fatal() != nil {
			return fmt.Errorf("cannot establish that %s is undeclared while the config has errors; fix it before cleanup", ref)
		}
		if dryRun {
			fmt.Fprintf(a.Out, "would delete leftover credentials of %s; nothing changed (--dry-run)\n", ref)
			return nil
		}
		if err := creds.Open().Delete(upstream.Server{ID: ref, Name: ref}); err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "deleted leftover credentials of %s\n", ref)
		return nil
	})
}

// mcpConsent reads only its own answer, leaving later secret input on the
// original terminal so it can still be read without echo.
func mcpConsent(stdio IO, question string) (bool, error) {
	fmt.Fprint(stdio.Err, question)
	var answer strings.Builder
	var b [1]byte
	for answer.Len() < 1024 {
		if _, err := io.ReadFull(stdio.In, b[:]); err != nil {
			return false, errors.New("no complete answer; nothing changed")
		}
		if b[0] == '\n' {
			a := strings.ToLower(strings.TrimSpace(answer.String()))
			return a == "y" || a == "yes", nil
		}
		answer.WriteByte(b[0])
	}
	return false, errors.New("answer too long; nothing changed")
}

func approvalState(s upstream.Server) string {
	if !s.Pinned {
		return "approval not required"
	}
	a, err := creds.Open().Approval(s)
	switch {
	case err != nil:
		return "approval unknown: " + err.Error()
	case a.Allowed:
		return "approved"
	case a.Version != 0:
		return "approval declined"
	default:
		return "approval needed"
	}
}

func (a *app) approveMCP(s upstream.Server) error {
	if !a.prompts() {
		return fmt.Errorf("approving %s needs your terminal; run ctxtra mcp approve %s there", s.ID, s.ID)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Err, "Server definition (no connection or command started):\n%s\n", data)
	accepted, err := mcpConsent(a.IO, "Allow this server? [y/N] ")
	if err != nil {
		return err
	}
	if err := withLock(func() error {
		cur, ok := findServer(s.ID)
		if !ok || creds.Fingerprint(cur) != creds.Fingerprint(s) {
			return fmt.Errorf("%s changed while you considered approval; nothing saved", s.ID)
		}
		return creds.Open().DecideApproval(s, accepted)
	}); err != nil {
		return err
	}
	if !accepted {
		return fmt.Errorf("approval of %s declined; no connection or command started", s.ID)
	}
	return nil
}

func (a *app) requireMCPApproval(s upstream.Server) error {
	err := creds.Open().RequireApproval(s)
	var needed *creds.ApprovalError
	if errors.As(err, &needed) {
		return a.approveMCP(s)
	}
	return err
}

func (a *app) mcpApprovalCmd(revoke bool) *cobra.Command {
	verb, short := "approve", "Approve a repo source's MCP server definition without connecting"
	if revoke {
		verb, short = "revoke", "Withdraw approval without deleting credentials or provider grants"
	}
	return &cobra.Command{
		Use: verb + " REF", Short: short, Args: exactly("REF"),
		Example: "  ctxtra mcp " + verb + " team:build",
		RunE: a.run(func(_ *cobra.Command, args []string) (result error) {
			defer func() {
				if result != nil {
					result = exitCode(newSources(a.IO).fail("mcp "+verb, result))
				}
			}()
			s, ok := findServer(args[0])
			if !ok {
				return fmt.Errorf("no MCP server %q", args[0])
			}
			if !s.Pinned {
				return fmt.Errorf("%s is a server of the config or of a dir source: it needs no approval", s.ID)
			}
			if !revoke {
				if err := a.approveMCP(s); err != nil {
					return err
				}
				fmt.Fprintf(a.Out, "approved %s on this machine; no connection or command started\n", s.ID)
				return nil
			}
			if err := creds.Open().DecideApproval(s, false); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "revoked approval of %s; new connections need approval again. Credentials and provider grants remain. Close existing sessions to stop their connections.\n", s.ID)
			return nil
		}),
	}
}
