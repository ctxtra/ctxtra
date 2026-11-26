package e2e

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ctxtra/ctxtra/e2e/driver"
	"github.com/ctxtra/ctxtra/e2e/mock"
)

// profileConfig binds IAM to the profile iam and OBS to observability.
const profileConfig = `schema: ctxtra/config/v1
bindings:
  - {name: iam, profiles: [iam], instructions: [~/ctx/iam.md]}
  - {name: obs, profiles: [observability], instructions: [~/ctx/obs.md]}
`

// profileSession is a session in a repository with profileConfig, and
// observability as the stored selection.
func profileSession(t *testing.T, a Agent) (*Session, string) {
	s := newSession(t, a)
	repo := s.Repo("dev/svc", "", nil)
	s.Marked("ctx/iam.md", "IAM")
	s.Marked("ctx/obs.md", "OBS")
	s.Config(profileConfig)
	s.Write(".local/state/ctxtra/profiles.json", `{"profiles": ["observability"]}`)
	s.Setup()
	return s, repo
}

func TestProfileSwitchingWhileASessionRuns(t *testing.T) {
	forEachAgent(t, []driver.Cap{driver.Subagent}, func(t *testing.T, a Agent) {
		s, repo := profileSession(t, a)
		s.Ctxtra("profile", "set", "iam")
		var switched atomic.Bool
		s.BeforeReply = func(q mock.Request) {
			if q.Role != mock.Main || !switched.CompareAndSwap(false, true) {
				return
			}
			cmd := exec.Command(s.Bin, "profile", "set", "observability")
			cmd.Dir, cmd.Env = s.Home, s.Env()
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("profile set: %v\n%s", err, b)
			}
		}
		r := s.Run(repo, driver.Options{}, mock.StartSubagent())
		r.First(mock.Main).Has("IAM").Lacks("OBS")
		if a.Name() == "antigravity" {
			// Antigravity gives its child's ID, but no parent ID.
			r.First(mock.Subagent).Has("OBS").Lacks("IAM")
		} else {
			r.First(mock.Subagent).Has("IAM").Lacks("OBS")
		}
		if b, err := os.ReadFile(s.Path(".local/state/ctxtra/profiles.json")); err != nil || !strings.Contains(string(b), `"observability"`) {
			t.Errorf("selection wasn't switched: %s %v", b, err)
		}
	})
}

// The stored selection decides what a session gets.
func TestProfileStored(t *testing.T) {
	forEachAgent(t, nil, func(t *testing.T, a Agent) {
		s, repo := profileSession(t, a)
		r := s.Run(repo, driver.Options{})
		r.First(mock.Main).Has("OBS").Lacks("IAM")
	})
}

// A session started with CTXTRA_PROFILES gets that profile instead of the
// stored selection, and so does its subagent; the session message names it.
func TestProfileVariable(t *testing.T) {
	forEachAgent(t, []driver.Cap{driver.Subagent}, func(t *testing.T, a Agent) {
		if a.Name() == "muse" {
			t.Skip("Muse runs hooks without the shell's environment: it sees only the stored selection")
		}
		s, repo := profileSession(t, a)
		s.Extra = append(s.Extra, "CTXTRA_PROFILES=iam")
		r := s.Run(repo, driver.Options{}, mock.StartSubagent())
		r.First(mock.Main).Has("IAM").Lacks("OBS")
		r.First(mock.Subagent).Has("IAM").Lacks("OBS")
		if driver.Has(a, driver.UserMessage) {
			msgs := a.UserMessages(s.Home, r.Stdout)
			if !slices.ContainsFunc(msgs, func(m string) bool { return strings.Contains(m, "\nProfiles: iam (from CTXTRA_PROFILES)") }) {
				t.Errorf("user messages %q", msgs)
			}
		}
	})
}

func TestProfileVariableReachesTheMCPProxy(t *testing.T) {
	forEachAgent(t, nil, func(t *testing.T, a Agent) {
		if a.Name() == "muse" {
			t.Skip("Muse clears the environment of hooks and MCP servers; tested with the stored selection")
		}
		for _, value := range []string{"iam", ""} {
			t.Run("CTXTRA_PROFILES="+value, func(t *testing.T) { profileProxy(t, a, value, value) })
		}
	})
}

func TestProfileMuseHooksWithoutTheEnvironment(t *testing.T) {
	forEachAgent(t, nil, func(t *testing.T, a Agent) {
		if a.Name() != "muse" {
			t.Skip("Muse clears the shell environment")
		}
		profileProxy(t, a, "iam", "observability")
	})
}

func profileProxy(t *testing.T, a Agent, value, want string) {
	t.Helper()
	probe := probeBinary(t)
	s := mcpSession(t, a, func(s *Session) string {
		s.Marked("ctx/iam.md", "IAM")
		s.Marked("ctx/obs.md", "OBS")
		return `schema: ctxtra/config/v1
mcp-servers:
  iamdb: {command: [` + probe + `, mcp, -log, ` + s.Path("iam.jsonl") + `]}
  obsdb: {command: [` + probe + `, mcp, -log, ` + s.Path("obs.jsonl") + `]}
bindings:
  - {name: iam, profiles: [iam], instructions: [~/ctx/iam.md], mcp-servers: [iamdb]}
  - {name: obs, profiles: [observability], instructions: [~/ctx/obs.md], mcp-servers: [obsdb]}
`
	})
	repo := s.Repo("dev/svc", "", nil)
	s.Ctxtra("profile", "set", "observability")
	s.Extra = append(s.Extra, "CTXTRA_PROFILES="+value)
	var steps []mock.Step
	switch want {
	case "iam":
		steps = mcpScript(a.Name(), "iamdb__probe_ping")
	case "observability":
		steps = mcpScript(a.Name(), "obsdb__probe_ping")
	}
	r := s.Run(repo, driver.Options{}, steps...)
	switch want {
	case "":
		r.First(mock.Main).Lacks("IAM", "OBS")
	case "iam":
		r.First(mock.Main).Has("IAM").Lacks("OBS")
	default:
		r.First(mock.Main).Has("OBS").Lacks("IAM")
	}
	for name, selected := range map[string]bool{"iam": want == "iam", "obs": want == "observability"} {
		calls, starts := probeCalls(t, s.Path(name+".jsonl"))
		if selected && !slices.Contains(calls, "probe_ping") || !selected && starts != 0 {
			t.Errorf("%s: calls %v, starts %d; selected %t", name, calls, starts, selected)
		}
	}
}
