// Package claude supports Claude Code: hooks in ~/.claude/settings.json (or
// $CLAUDE_CONFIG_DIR) for session start and subagent start, and a plugin in
// its skills folder that offers the skills as slash commands.
package claude

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/cursor"
	"github.com/ctxtra/ctxtra/internal/agent/devin"
	"github.com/ctxtra/ctxtra/internal/agent/hookjson"
	"github.com/ctxtra/ctxtra/internal/chunk"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
)

// Agent is Claude Code.
type Agent struct{}

var (
	_ agent.Agent    = Agent{}
	_ agent.Slotted  = Agent{}
	_ agent.Missing  = Agent{}
	_ agent.Native   = Agent{}
	_ agent.Launcher = Agent{}
)

// events are not registered for resume: the resumed transcript already
// contains the injected text.
var events = []hookjson.Event{{Name: "SessionStart", Matcher: "startup|clear|compact"}, {Name: "SubagentStart"}}

func (Agent) Name() string { return "claude" }

func dir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(paths.Home(), ".claude")
}

func (Agent) ConfigPath() string { return filepath.Join(dir(), "settings.json") }

func (Agent) Detect() bool {
	if st, err := os.Stat(dir()); err == nil && st.IsDir() {
		return true
	}
	_, err := exec.LookPath("claude")
	return err == nil
}

func (a Agent) hooks() hookjson.Config {
	return hookjson.Config{Agent: a.Name(), Path: a.ConfigPath(), Events: events, Slots: a.Delivery().Slots, Missing: a}
}

// Setup sets up the hooks, then the plugin, which needs them: the hooks add
// the files, and the skill list that the model reads.
func (a Agent) Setup(o agent.Options, r *agent.Report) error {
	if err := a.hooks().Apply(o, r); err != nil {
		return err
	}
	return setupPlugin(o, r)
}

func (a Agent) Status(bin string, known bool) agent.Status {
	st := a.hooks().Status(bin, known)
	if len(st.Details) == 0 && a.hooksSetUp() {
		if n := checkPlugin(bin); len(n) > 0 {
			st.OK, st.Details = false, n
		}
	}
	return st
}

// Check: the hooks' notices first; one setup repairs both, so the plugin's
// come only when the hooks need nothing.
func (a Agent) Check(bin string, known bool) []string {
	if n := a.hooks().Check(bin, known); len(n) > 0 || !a.hooksSetUp() {
		return n
	}
	return checkPlugin(bin)
}

func (a Agent) hooksSetUp() bool {
	cmds, err := a.hooks().Commands()
	return err == nil && len(cmds) > 0
}

// Skip: Devin and the Cursor desktop app also run the hooks in
// ~/.claude/settings.json, and when ctxtra's own hook of that agent is set
// up, the session would get the files twice. In Cursor, all of Claude's
// slots would also slow down the session start, whose hooks Cursor doesn't
// wait for.
func (Agent) Skip(in agent.Input) string {
	switch {
	case devin.UnderDevin():
		return "run by Devin, whose own ctxtra hook adds the files"
	case in.CursorVersion != "" && cursor.HookSetUp():
		return "run by Cursor, whose own ctxtra hook adds the files"
	}
	return ""
}

// Answer is the whole text in one hook. The hooks that setup writes have
// slots, and get SlotAnswer.
func (Agent) Answer(event string, res *resolve.Result) any {
	if event == pluginEvent {
		return pluginAnswer(res)
	}
	return agent.AdditionalContext(event, res)
}

func (Agent) SlotAnswer(event, text, summary string) any {
	return agent.ContextAnswer(event, text, summary)
}

func (Agent) MissingAnswer(event, msg string) string { return agent.SystemMessage(event, msg) }

// NativeFiles: CLAUDE.md, then the user-level rules: the .md files in rules/
// and its subfolders, which Claude finds also through symlinks.
func (Agent) NativeFiles() []agent.NativeFile {
	var out []agent.NativeFile
	if f, ok := agent.ReadNative(filepath.Join(dir(), "CLAUDE.md")); ok {
		out = append(out, f)
	}
	return append(out, agent.WalkNative(filepath.Join(dir(), "rules"), ".md")...)
}

func (Agent) Launch(prompt string) []string { return []string{"claude", prompt} }

// Delivery: Claude shows the model only a preview of a hook's text over
// 10,000 UTF-16 units, but gets the text of every hook of an event: 40 hooks
// of up to 9,000 each (measured with Claude Code 2.1.300).
func (Agent) Delivery() agent.Delivery {
	return agent.Delivery{Title: "Claude Code", Capacity: 360_000, Unit: chunk.UTF16, Chunk: 9000, Slots: 40}
}
