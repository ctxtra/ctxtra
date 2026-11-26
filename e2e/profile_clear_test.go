package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/e2e/driver"
	"github.com/ctxtra/ctxtra/e2e/mock"
)

func TestProfileClearStartsOver(t *testing.T) {
	forEachAgent(t, nil, func(t *testing.T, a Agent) {
		if a.Name() != "opencode" {
			t.Skip("OpenCode's /clear")
		}
		s, repo := profileSession(t, a)
		s.Ctxtra("profile", "set", "iam")
		m := mock.New(t)
		script := tuiScript(t, s, a, repo, m.URL())
		x := startTUI(t, tmuxBin(t), script)
		t.Cleanup(func() {
			if t.Failed() {
				screen := x.screen()
				t.Logf("screen:\n%s", screen)
				cmd := exec.Command("/bin/sh", script)
				cmd.Dir, cmd.Env = repo, s.Env()
				s.saveEvidence(1, cmd, &Result{t: t, Requests: m.Requests(), Stdout: screen})
			}
		})
		waitFor := func(what string, ready func(string) bool) {
			t.Helper()
			for end := time.Now().Add(30 * time.Second); time.Now().Before(end); {
				if ready(x.screen()) {
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			t.Fatalf("%s didn't appear:\n%s", what, x.screen())
		}
		waitFor("prompt", func(sc string) bool { return strings.Contains(sc, "Ask anything") })
		x.prompt(t, m, 0)
		Text{t, "before clear", firstMain(t, m, 0)}.Has("IAM").Lacks("OBS")
		waitFor("reply", func(sc string) bool { return strings.Contains(sc, mock.Reply) })
		before, _ := filepath.Glob(s.Path(".local/state/ctxtra/sessions/*.json"))
		s.Ctxtra("profile", "set", "observability")
		seen := len(m.Requests())
		x.keys("-l", "/clear")
		waitFor("clear command menu", func(sc string) bool {
			return strings.Contains(sc, "/clear") && strings.Contains(sc, "Clear session")
		})
		x.keys("Enter")
		waitFor("cleared prompt", func(sc string) bool {
			return strings.Contains(sc, "Ask anything") && !strings.Contains(sc, mock.Prompt)
		})
		x.prompt(t, m, seen)
		Text{t, "after clear", firstMain(t, m, seen)}.Has("OBS").Lacks("IAM")
		after, _ := filepath.Glob(s.Path(".local/state/ctxtra/sessions/*.json"))
		if len(before) != 1 || len(after) != 2 {
			t.Errorf("session records before %d, after %d: /clear should create a new native ID", len(before), len(after))
		}
		t.Logf("%s: /clear made a new session record and selected observability", driver.Version(a.Bin))
	})
}
