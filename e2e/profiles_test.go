package e2e

import (
	"slices"
	"strings"
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
