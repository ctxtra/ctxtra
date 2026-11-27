package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ctxtra/ctxtra/internal/paths"
)

// commandTemplate is the hook command: the binary path, the agent, the
// event with the hook's slot, and what it prints when the binary is gone, a
// shell word from missingWord: {}, or a message for the user. It reads stdin
// then, so the agent never gets a broken pipe; /bin/cat by its path, as PATH
// may be anything. Codex trusts a hook by a hash of the command, so the text
// must stay the same across versions.
const commandTemplate = `/bin/sh -c 'f="%s"; if [ -x "$f" ]; then exec "$f" hook %s %s ` + SetupVersionArg + `; else /bin/cat >/dev/null 2>&1; echo %s; fi'`

// wordRe is a shell word that missingWord writes: a double-quoted string
// whose only escapes are \".
const wordRe = `"((?:[^"\\$` + "`" + `'\n]|\\")*)"`

var commandWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// commandRe matches commandTemplate with any binary path, so setup
// recognizes its own entries also after the binary moved. The event can
// have a slot (SlotCommand).
var commandRe = func() *regexp.Regexp {
	p := strings.Split(commandTemplate, "%s")
	return regexp.MustCompile("^" + regexp.QuoteMeta(p[0]) + `([^"]+)` + regexp.QuoteMeta(p[1]) + `(\S+)` + regexp.QuoteMeta(p[2]) +
		`(\S+)(?: --slot (\d+) --of (\d+))?(?: (` + regexp.QuoteMeta(SetupVersionArg) + `))?` + regexp.QuoteMeta(strings.TrimPrefix(p[3], " "+SetupVersionArg)) + wordRe + regexp.QuoteMeta(p[4]) + "$")
}()

// MissingMessage is what the hook of agent tells the user when the binary
// it runs is gone. It has no path: the hook command can't hold every path
// in a JSON string.
func MissingMessage(agent string) string {
	return "ctxtra: the ctxtra binary that this hook runs is missing; reinstall ctxtra, or run ctxtra setup --agent " +
		agent + " with the new binary"
}

// missingAnswer is what the hook of agent, event and slot prints when the
// binary is gone: m's message for slot 0, or for a hook without slots, and
// {} for the other slots and for an agent without a message.
func missingAnswer(m Missing, agent, event string, slot int) string {
	if m == nil || slot > 0 {
		return "{}"
	}
	if s := m.MissingAnswer(event, MissingMessage(agent)); s != "" && !strings.ContainsAny(s, "\\$`'\n") {
		return s
	}
	return "{}"
}

// missingWord is answer as a shell word inside commandTemplate's single
// quotes.
func missingWord(answer string) string { return `"` + strings.ReplaceAll(answer, `"`, `\"`) + `"` }

// Command is the hook command for one agent and event. A binary under the
// home folder is written as $HOME/..., so the same agent config works on
// another machine with a different user name. m is the agent, when its hook
// answer can show the user that the binary is gone.
func Command(bin, agent, event string, m Missing) string {
	return fmt.Sprintf(commandTemplate, ShellPath(bin), agent, event, missingWord(missingAnswer(m, agent, event, -1)))
}

// SlotCommand is the command of hook slot of the slots of a Slotted agent.
func SlotCommand(bin, agent, event string, slot, slots int, m Missing) string {
	return fmt.Sprintf(commandTemplate, ShellPath(bin), agent, fmt.Sprintf("%s --slot %d --of %d", event, slot, slots),
		missingWord(missingAnswer(m, agent, event, slot)))
}

// Hook is what one of our hook commands runs.
type Hook struct {
	Binary, Agent, Event string
	Slot                 int // --slot, or -1 without one
	Slots                int // --of, or 0 without one
	Env                  map[string]string
	Current              bool // carries the current setup contract marker
}

// WithXDG records folder variables for agents that clear the hook environment.
func WithXDG(cmd string) string {
	var prefix strings.Builder
	for _, key := range paths.XDGVars {
		fmt.Fprintf(&prefix, "%s=\"%s\" ", key, shellEscape(os.Getenv(key)))
	}
	return prefix.String() + cmd
}

func shellEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(s)
}

var envWord = regexp.MustCompile("^([A-Z_]+)=\"((?:[^\"\\\\$`]|\\\\[\\\\\"$`])*)\" ")

func hookEnv(cmd string) (string, map[string]string) {
	var env map[string]string
	for _, key := range paths.XDGVars {
		g := envWord.FindStringSubmatch(cmd)
		if g == nil || g[1] != key {
			return cmd, env
		}
		if env == nil {
			env = map[string]string{}
		}
		env[key] = strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\$`, `$`, "\\`", "`").Replace(g[2])
		cmd = cmd[len(g[0]):]
	}
	return cmd, env
}

// XDGNotices reports setup-time folder values that differ from this process.
func XDGNotices(what, name string, env map[string]string) []string {
	var out []string
	for _, key := range paths.XDGVars {
		if saved, current := env[key], os.Getenv(key); saved != current {
			out = append(out, fmt.Sprintf("%s use %s=%q, current value is %q; %s", what, key, saved, current, Repair(name)))
		}
	}
	return out
}

// ParseCommand recognizes managed shell commands and plain commands from
// earlier setups. It checks the fallback for a missing binary, so setup
// leaves commands edited by hand alone.
func ParseCommand(cmd string, m Missing) (Hook, bool) {
	cmd, env := hookEnv(cmd)
	g := commandRe.FindStringSubmatch(cmd)
	if g == nil {
		words := strings.Fields(cmd)
		if len(words) == 4 && filepath.Base(words[0]) == "ctxtra" && words[1] == "hook" &&
			commandWord.MatchString(words[2]) && commandWord.MatchString(words[3]) {
			return Hook{Binary: ExpandShellPath(words[0]), Agent: words[2], Event: words[3], Slot: -1}, true
		}
		return Hook{}, false
	}
	h := Hook{Binary: ExpandShellPath(g[1]), Agent: g[2], Event: g[3], Slot: -1, Env: env, Current: g[6] != ""}
	if env != nil && (h.Agent != "muse" || len(env) != len(paths.XDGVars)) {
		return Hook{}, false
	}
	if g[4] != "" {
		slot, err1 := strconv.Atoi(g[4])
		slots, err2 := strconv.Atoi(g[5])
		if err1 != nil || err2 != nil {
			return Hook{}, false
		}
		h.Slot, h.Slots = slot, slots
	}
	answer := strings.ReplaceAll(g[7], `\"`, `"`)
	if answer != missingAnswer(m, h.Agent, h.Event, h.Slot) && (answer != "{}" || h.Current) {
		return Hook{}, false
	}
	return h, true
}

// ShellPath is bin as the hook command writes it.
func ShellPath(bin string) string {
	if t := paths.Tilde(bin); strings.HasPrefix(t, "~/") {
		return "$HOME/" + t[2:]
	}
	return bin
}

// ExpandShellPath turns a path written by ShellPath back into a file path.
func ExpandShellPath(p string) string {
	if rest, ok := strings.CutPrefix(p, "$HOME/"); ok {
		return filepath.Join(paths.Home(), rest)
	}
	return p
}

// CheckBinary is the state of the binary a hook runs, for Status: "ok" or
// "MISSING".
func CheckBinary(bin string) (string, bool) {
	if st, err := os.Stat(bin); err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return "MISSING", false
	}
	return "ok", true
}

// ValidBinaryPath reports an error for a path the hook command can't hold.
func ValidBinaryPath(bin string) error {
	if !filepath.IsAbs(bin) {
		return fmt.Errorf("binary path %q is not absolute", bin)
	}
	if strings.ContainsAny(bin, "\"'$`\\\n") {
		return fmt.Errorf("unsupported characters in the binary path %q", bin)
	}
	return nil
}
