package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/e2e/driver"
	"github.com/ctxtra/ctxtra/e2e/mock"
)

const commandConfig = `schema: ctxtra/v1
bindings:
  - name: svc
    match: {repos: [{remote: github.com/acme/svc}]}
    skills: [~/skills/deploy, ~/skills/review]
  - name: other
    match: {repos: [{remote: github.com/acme/other}]}
    skills: [~/skills/other]
`

// In claude -p, ctxtra adds no commands: a command that is the whole prompt
// would end the run without a turn. /deploy reaches the model as text, with
// the skill list that says where its SKILL.md is.
func TestSkillCommandHeadless(t *testing.T) {
	forEachAgent(t, []driver.Cap{driver.SkillCommand}, func(t *testing.T, a Agent) {
		s := newSession(t, a)
		repo := s.Repo("dev/svc", "https://github.com/acme/svc", nil)
		deploy := s.Skill("skills/deploy", "deploy", "DEPLOYDESC", "DEPLOYBODY")
		s.Skill("skills/review", "review", "REVIEWDESC", "REVIEWBODY")
		s.Skill("skills/other", "other", "OTHERDESC", "OTHERBODY")
		s.Config(commandConfig)
		s.Setup()
		r := s.Run(repo, driver.Options{Prompt: "/deploy ARGMARK"})
		r.First(mock.Main).Has("DEPLOYDESC").Lacks("DEPLOYBODY").Contains("/deploy ARGMARK").Contains(deploy)
	})
}

// In the interactive app, the / menu lists the session's skills with their
// descriptions, not another repository's, and a skill of the user's own
// wins over a bound one with its name. Running a skill's command gives the
// model its body, with the arguments in place of $ARGUMENTS, and starts a
// turn.
func TestSkillCommand(t *testing.T) {
	forEachAgent(t, []driver.Cap{driver.SkillCommand}, func(t *testing.T, a Agent) {
		tmux := tmuxBin(t)
		s := newSession(t, a)
		// First-run onboarding checks Anthropic's servers even with a mock model.
		s.Write(".claude/.claude.json", `{"hasCompletedOnboarding":true,"customApiKeyResponses":{"approved":["sk-e2e-mock"],"rejected":[]}}`)
		repo := s.Repo("dev/svc", "https://github.com/acme/svc", nil)
		s.Skill("skills/deploy", "deploy", "DEPLOYDESC", "DEPLOYBODY $ARGUMENTS")
		s.Skill("skills/review", "review", "CTXREVIEWDESC", "CTXREVIEWBODY")
		s.Skill("skills/other", "other", "OTHERDESC", "OTHERBODY")
		s.Skill(".claude/skills/review", "review", "OWNREVIEWDESC", "OWNREVIEWBODY")
		s.Config(commandConfig)
		s.Setup()
		m := mock.New(t)
		x := startTUI(t, tmux, tuiScript(t, s, a, repo, m.URL()))
		t.Cleanup(func() {
			if t.Failed() {
				t.Logf("screen:\n%s", x.screen())
			}
		})
		ui := a.Driver.(driver.TUI)
		// typed types text into the prompt and returns the screen once it
		// shows want, answering the app's startup questions on the way.
		typed := func(text, want string) string {
			t.Helper()
			x.keys("C-u")
			deadline := time.Now().Add(60 * time.Second)
			for time.Now().Before(deadline) {
				sc := x.screen()
				switch keys := ui.Answer(sc); {
				case keys != nil:
					x.keys(keys...)
				case prompted(sc, text) && strings.Contains(sc, want):
					return sc
				case !prompted(sc, text):
					x.keys("C-u")
					x.keys("-l", text)
				}
				time.Sleep(500 * time.Millisecond)
			}
			t.Fatalf("no %q after typing %q", want, text)
			return ""
		}
		if sc := typed("/dep", "/deploy"); !strings.Contains(sc, "Use for deploy tasks. CTXMARK-DEPLOYDESC (ctxtra)") {
			t.Errorf("the menu lacks the skill's description:\n%s", sc)
		}
		if sc := typed("/review", "CTXMARK-OWNREVIEWDESC"); strings.Contains(sc, "CTXMARK-CTXREVIEWDESC") {
			t.Errorf("the menu has the bound review skill next to the user's own:\n%s", sc)
		}
		typed("/other", "")
		time.Sleep(2 * time.Second)
		if sc := x.screen(); strings.Contains(sc, "CTXMARK-OTHERDESC") {
			t.Errorf("the menu has another repository's skill:\n%s", sc)
		}

		typed("/deploy ARGMARK", "")
		x.keys("Enter")
		var text string
		for end := time.Now().Add(60 * time.Second); text == "" && time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
			for _, q := range m.Requests() {
				if q.Role == mock.Main && strings.Contains(q.Text, "Run the deploy skill: ARGMARK") {
					text = q.Text
				}
			}
		}
		if text == "" {
			t.Fatal("no model request after /deploy ARGMARK")
		}
		Text{t, "main", text}.Has("DEPLOYBODY").Lacks("OTHERBODY").Contains("CTXMARK-DEPLOYBODY ARGMARK")
		s.hookRan("PluginStart")
		x.stop()
	})
}

// prompted reports whether the prompt line of the screen sc holds text.
// Claude's prompt marker can be followed by a no-break space.
func prompted(sc, text string) bool {
	for _, l := range strings.Split(sc, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "❯"); ok && strings.TrimSpace(rest) == text {
			return true
		}
	}
	return false
}

// tuiScript writes a script that starts the agent's interactive app in
// repo, with the fixture's environment and the mock as its model.
func tuiScript(t *testing.T, s *Session, a Agent, repo, mockURL string) string {
	t.Helper()
	app, err := a.Driver.(driver.TUI).Interactive(a.Bin, s.Home, mockURL, s.Env())
	if err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	var b strings.Builder
	b.WriteString("#!/bin/sh\ncd " + q(repo) + " || exit 1\nexec env -i")
	for _, e := range app.Env {
		if strings.HasPrefix(e, "TERM=") {
			e = "TERM=xterm-256color"
		}
		b.WriteString(" " + q(e))
	}
	b.WriteString(" " + q(a.Bin))
	for _, arg := range append(app.Pre, app.Post...) {
		b.WriteString(" " + q(arg))
	}
	b.WriteString("\n")
	path := s.Write("tui.sh", b.String())
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
