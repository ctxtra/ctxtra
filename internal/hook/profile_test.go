package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// profileSession binds a file per profile: IAM-RULES for iam, OBS-RULES
// for observability. It returns the session folder.
func profileSession(t *testing.T) (home, dir string) {
	h := testenv.Home(t)
	dir = filepath.Join(h, "svc")
	testenv.Write(t, filepath.Join(dir, "main.go"), "package main\n")
	testenv.Write(t, filepath.Join(h, "iam.md"), "IAM-RULES")
	testenv.Write(t, filepath.Join(h, "obs.md"), "OBS-RULES")
	testenv.Config(t, h, `schema: ctxtra/config/v1
bindings:
  - {name: iam, profiles: [iam], instructions: [~/iam.md]}
  - {name: obs, profiles: [observability], instructions: [~/obs.md]}
`)
	return h, dir
}

// codexRun runs Codex's hook for event, with the session ID and source
// given, and returns its text and the user's message.
func codexRun(t *testing.T, dir, event, session, source string) (text, msg string) {
	t.Helper()
	in := map[string]string{"cwd": dir, "source": source}
	if session != "" {
		in["session_id"] = session
	}
	b, _ := json.Marshal(in)
	var out bytes.Buffer
	Run("codex", event, bytes.NewReader(b), &out, io.Discard)
	var m struct {
		HookSpecificOutput struct{ AdditionalContext string }
		SystemMessage      string
	}
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("%q", out.String())
	}
	return m.HookSpecificOutput.AdditionalContext, m.SystemMessage
}

// rules are the profiles' files that text has.
func rules(text string) []string {
	var out []string
	for _, r := range []string{"IAM-RULES", "OBS-RULES"} {
		if strings.Contains(text, r) {
			out = append(out, r)
		}
	}
	return out
}

func setSelection(t *testing.T, names ...string) {
	t.Helper()
	if err := profile.Store(names); err != nil {
		t.Fatal(err)
	}
}

func lastEntry(t *testing.T, h string) Entry {
	t.Helper()
	e := logEntries(t, h)
	return e[len(e)-1]
}

func TestHookASessionKeepsItsProfiles(t *testing.T) {
	t.Run("switching while a session runs", func(t *testing.T) {
		h, dir := profileSession(t)
		setSelection(t, "iam")
		if text, _ := codexRun(t, dir, "SessionStart", "s1", "startup"); !slices.Equal(rules(text), []string{"IAM-RULES"}) {
			t.Fatalf("start: %q", text)
		}
		setSelection(t, "observability")
		if text, _ := codexRun(t, dir, "SubagentStart", "s1", ""); !slices.Equal(rules(text), []string{"IAM-RULES"}) {
			t.Errorf("subagent: %q", text)
		}
		// Kept profiles in the log.
		if e := lastEntry(t, h); !slices.Equal(e.Profiles, []string{"iam"}) || e.ProfilesFrom != "stored selection" || !e.ProfilesKept {
			t.Errorf("log: %+v", e)
		}
	})
	t.Run("compaction", func(t *testing.T) {
		_, dir := profileSession(t)
		setSelection(t, "iam")
		codexRun(t, dir, "SessionStart", "s1", "startup")
		setSelection(t, "observability")
		if text, _ := codexRun(t, dir, "SessionStart", "s1", "compact"); !slices.Equal(rules(text), []string{"IAM-RULES"}) {
			t.Errorf("compact: %q", text)
		}
	})
	t.Run("clear starts over", func(t *testing.T) {
		h, dir := profileSession(t)
		setSelection(t, "iam")
		codexRun(t, dir, "SessionStart", "s1", "startup")
		setSelection(t, "observability")
		if text, _ := codexRun(t, dir, "SessionStart", "s1", "clear"); !slices.Equal(rules(text), []string{"OBS-RULES"}) {
			t.Errorf("clear: %q", text)
		}
		if e := lastEntry(t, h); e.ProfilesKept {
			t.Errorf("log: %+v", e)
		}
		if text, _ := codexRun(t, dir, "SubagentStart", "s1", ""); !slices.Equal(rules(text), []string{"OBS-RULES"}) {
			t.Errorf("subagent: %q", text)
		}
	})
}

func TestHookKeepingProfilesNeverBreaksASession(t *testing.T) {
	t.Run("agent without a session ID", func(t *testing.T) {
		_, dir := profileSession(t)
		setSelection(t, "iam")
		codexRun(t, dir, "SessionStart", "", "startup")
		setSelection(t, "observability")
		if text, _ := codexRun(t, dir, "SubagentStart", "", ""); !slices.Equal(rules(text), []string{"OBS-RULES"}) {
			t.Errorf("subagent: %q", text)
		}
	})
	t.Run("cannot record", func(t *testing.T) {
		h, dir := profileSession(t)
		setSelection(t, "iam")
		testenv.Write(t, filepath.Join(h, ".local/state/ctxtra/sessions"), "a file, not a folder")
		text, msg := codexRun(t, dir, "SessionStart", "s1", "startup")
		if !slices.Equal(rules(text), []string{"IAM-RULES"}) || strings.Contains(msg, "error") {
			t.Errorf("start: %q %q", text, msg)
		}
		e := lastEntry(t, h)
		if !slices.ContainsFunc(e.Errors, func(s string) bool { return strings.HasPrefix(s, "can't keep the session's profiles: ") }) {
			t.Errorf("log: %+v", e)
		}
	})
}

func TestHookShowingTheActiveProfiles(t *testing.T) {
	t.Run("session message", func(t *testing.T) {
		h, dir := profileSession(t)
		t.Setenv(profile.Env, "iam")
		_, msg := codexRun(t, dir, "SessionStart", "s1", "startup")
		if msg != "✓ ctxtra added 1 instruction\nProfiles: iam (from CTXTRA_PROFILES)\nInstructions:\n  local\n  - ~/iam.md" {
			t.Errorf("%q", msg)
		}
		if e := lastEntry(t, h); !slices.Equal(e.Profiles, []string{"iam"}) || e.ProfilesFrom != "CTXTRA_PROFILES" || e.ProfilesKept {
			t.Errorf("log: %+v", e)
		}
	})
	t.Run("no profile and no error", func(t *testing.T) {
		_, dir := profileSession(t)
		if _, msg := codexRun(t, dir, "SessionStart", "s1", "startup"); msg != "" {
			t.Errorf("%q", msg)
		}
	})
	t.Run("one invalid name", func(t *testing.T) {
		h, dir := profileSession(t)
		t.Setenv(profile.Env, "iam,on call")
		text, msg := codexRun(t, dir, "SessionStart", "s1", "startup")
		if !slices.Equal(rules(text), []string{"IAM-RULES"}) {
			t.Errorf("text: %q", text)
		}
		want := `CTXTRA_PROFILES: "on call" is not a valid profile name`
		if !strings.Contains(msg, "; 1 error (run ctxtra doctor)\nProfiles: iam (from CTXTRA_PROFILES)\n") || !strings.Contains(msg, "\n⚠ "+want) {
			t.Errorf("message: %q", msg)
		}
		if e := lastEntry(t, h); len(e.Errors) != 1 || !strings.HasPrefix(e.Errors[0], want) {
			t.Errorf("log: %+v", e)
		}
	})
	t.Run("typo in the variable", func(t *testing.T) {
		_, dir := profileSession(t)
		t.Setenv(profile.Env, "obsrvability,iam")
		text, msg := codexRun(t, dir, "SessionStart", "s1", "startup")
		if !slices.Equal(rules(text), []string{"IAM-RULES"}) || !strings.Contains(msg, "\n⚠ profile obsrvability: no binding uses it; check the name for a typo") {
			t.Errorf("%q %q", text, msg)
		}
	})
	t.Run("unreadable selection in a session", func(t *testing.T) {
		h, dir := profileSession(t)
		testenv.Write(t, filepath.Join(h, ".local/state/ctxtra/profiles.json"), "iam")
		text, msg := codexRun(t, dir, "SessionStart", "s1", "startup")
		if len(rules(text)) != 0 || !strings.HasPrefix(msg, "ctxtra added nothing; 1 error (run ctxtra doctor)\nProfiles: none\n\n⚠ ~/.local/state/ctxtra/profiles.json is not a valid profile selection: ") {
			t.Errorf("%q %q", text, msg)
		}
	})
}

// The slots of one session start share the profiles that the slot that
// resolves chose; a subagent's slots keep them.
func TestSlotsKeepTheSessionsProfiles(t *testing.T) {
	h, dir := profileSession(t)
	orig := endLock
	endLock = func(unlock func()) { unlock() }
	t.Cleanup(func() { endLock = orig })
	setSelection(t, "iam")
	in := `{"cwd": "` + dir + `", "session_id": "s1", "source": "startup"}`
	check := func(event string, kept bool) {
		t.Helper()
		var text []string
		for _, a := range runSlotsOf(t, event, in, 4) {
			text = append(text, a.text)
		}
		if got := rules(strings.Join(text, "\n")); !slices.Equal(got, []string{"IAM-RULES"}) {
			t.Errorf("%s: %v", event, got)
		}
		var logged []Entry
		for _, e := range logEntries(t, h) {
			if e.Event == event {
				logged = append(logged, e)
			}
		}
		if len(logged) != 1 || !slices.Equal(logged[0].Profiles, []string{"iam"}) || logged[0].ProfilesKept != kept {
			t.Errorf("%s: log %+v", event, logged)
		}
	}
	check("SessionStart", false)
	setSelection(t, "observability")
	check("SubagentStart", true)
	if r, _ := filepath.Glob(filepath.Join(h, ".local/state/ctxtra/sessions/*.json")); len(r) != 1 {
		t.Errorf("records: %v", r)
	}
	if _, err := os.Stat(filepath.Join(h, ".local/state/ctxtra/slots")); err != nil {
		t.Error(err)
	}
}
