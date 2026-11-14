package claude

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/extfile"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
)

var _ agent.PluginEvents = Agent{}

// The plugin is a Claude Code plugin named ctxtra with a mod. Claude loads
// a plugin folder in its skills folder in place, as ctxtra@skills-dir:
// setup writes the files, and nothing is installed or copied.
//
// The mod asks the binary for the session's skills with PluginStart, and
// registers a slash command for each.
const (
	pluginEvent = "PluginStart"
	// protocol is the version of the mod, which it sends as ctxtra_plugin.
	protocol = 1
)

func pluginDir() string { return filepath.Join(dir(), "skills", "ctxtra") }

// PluginPath is the mod, the file that says it is ctxtra's.
func (Agent) PluginPath() string { return filepath.Join(pluginDir(), "hooks", "ctxtra.js") }

func (Agent) PluginEvent(event string) bool { return event == pluginEvent }

// pluginFile is a file of the plugin, by its path in the plugin folder.
type pluginFile struct{ rel, content string }

// pluginFiles are the plugin's files for bin, the mod last: a folder with
// the mod and without the rest is one that a setup didn't finish.
func pluginFiles(bin string) []pluginFile {
	return []pluginFile{
		{".claude-plugin/plugin.json", manifest},
		{"hooks/hooks.json", modules},
		{"hooks/ctxtra.js", Mod(bin)},
	}
}

const manifest = `{
  "name": "ctxtra",
  "description": "Written by ctxtra setup: offers the skills that ctxtra binds to the session's folder as slash commands."
}
`

const modules = `{
  "modules": ["./ctxtra.js"]
}
`

// foreign is why the plugin folder isn't ctxtra's, "" when it is or there
// is none. A folder with ctxtra's mod is ctxtra's, also with other files in
// it, so that setup still updates and removes the mod. Without the mod, a
// file there that the plugin doesn't have makes it someone else's, as does
// a mod that ctxtra didn't write, or a folder that is a symlink.
func foreign() (string, error) {
	root := pluginDir()
	st, err := os.Lstat(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", err
	case !st.IsDir():
		return fmt.Sprintf("%s is not a folder of ctxtra's", paths.Tilde(root)), nil
	}
	mod := Agent{}.PluginPath()
	if _, err := os.Lstat(mod); err == nil {
		if extfile.Owned(mod) != nil {
			return fmt.Sprintf("%s is not ctxtra's", paths.Tilde(mod)), nil
		}
		return "", nil
	}
	ours := []string{".claude-plugin", "hooks"}
	for _, f := range pluginFiles("") {
		ours = append(ours, f.rel)
	}
	why := ""
	n := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// A user's folder can be large: it isn't ctxtra's either way.
		if n++; n > 100 {
			why = fmt.Sprintf("%s has files that ctxtra didn't write", paths.Tilde(root))
			return fs.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." || d.Name() == ".DS_Store" {
			return nil
		}
		if !slices.Contains(ours, filepath.ToSlash(rel)) {
			why = fmt.Sprintf("%s has %s, which ctxtra didn't write", paths.Tilde(root), filepath.ToSlash(rel))
			return fs.SkipAll
		}
		return nil
	})
	return why, err
}

// setupPlugin writes the plugin's files, or removes them with o.Remove. A
// folder that isn't ctxtra's is left alone with a warning: the hooks add the
// skills without it.
func setupPlugin(o agent.Options, r *agent.Report) error {
	why, err := foreign()
	if err != nil {
		return err
	}
	if why != "" {
		if !o.Remove {
			r.Warnings = append(r.Warnings, why+"; left alone, so Claude Code doesn't offer the skills as slash commands")
		}
		return nil
	}
	files := pluginFiles(o.Binary)
	if !o.Remove {
		for _, f := range files {
			written, changed, err := fsutil.WriteAtomic(filepath.Join(pluginDir(), f.rel), []byte(f.content), paths.BackupDir(), o.DryRun)
			if changed {
				r.Changes = append(r.Changes, written)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	// The mod first, so that a removal that stops halfway leaves no mod.
	for _, f := range slices.Backward(files) {
		path := filepath.Join(pluginDir(), f.rel)
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		r.Changes = append(r.Changes, path)
		if o.DryRun {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if !o.DryRun {
		// Only empty folders: one with a file of the user's stays.
		for _, d := range []string{"hooks", ".claude-plugin", "."} {
			_ = os.Remove(filepath.Join(pluginDir(), d))
		}
	}
	return nil
}

// checkPlugin is what the user should do about the plugin, when the hooks
// are set up: a plugin that is missing, incomplete, or runs another binary.
// A folder that isn't ctxtra's is no notice: setup said so, and the hooks
// work without it.
func checkPlugin(bin string) []string {
	repair := agent.Repair("claude")
	if why, err := foreign(); err != nil {
		return []string{agent.UnreadableNotice(pluginDir(), err)}
	} else if why != "" {
		return nil
	}
	notices, ours := extfile.CheckSetup(extfile.Ext{Path: Agent{}.PluginPath(), Agent: "claude", Noun: "plugin"}, bin, false)
	if !ours {
		if len(notices) > 0 {
			return notices
		}
		return []string{fmt.Sprintf("ctxtra's plugin, which offers the skills as slash commands, is missing from %s; %s",
			paths.Tilde(pluginDir()), repair)}
	}
	for _, f := range pluginFiles("")[:2] {
		b, err := fsutil.ReadBounded(filepath.Join(pluginDir(), f.rel), agent.MaxConfig)
		if err != nil || string(b) != f.content {
			notices = append(notices, fmt.Sprintf("ctxtra's plugin in %s is incomplete; %s", paths.Tilde(pluginDir()), repair))
			break
		}
	}
	return notices
}

// pluginAnswer is the skills of the session for the mod.
func pluginAnswer(res *resolve.Result) any {
	type skill struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Path        string `json:"path"`
	}
	skills := []skill{}
	if res != nil {
		for _, h := range res.Skills {
			skills = append(skills, skill{h.Name, h.Description, h.Path})
		}
	}
	return map[string]any{"skills": skills}
}

// Mod is the mod that runs bin.
func Mod(bin string) string { return extfile.Content(extfile.Mod, bin, protocol, modBody) }

// modBody registers the commands at session start, which Claude waits for
// before the first prompt. Claude refuses a prompt that a command.run hook
// submits, because the hook holds the turn: the hook gives the model the
// skill as hidden context, and a timer submits the prompt once it returned.
//
// A session without a surface (claude -p, the Agent SDK) gets no commands:
// there a command that is the whole prompt ends the run before the timer,
// while an unknown /name reaches the model as text, with the skill list.
//
// A command that is there already wins: a built-in, the user's own skill or
// command, another plugin's. Without an answer from ctxtra (no binary, a
// timeout) the mod registers nothing; it never throws.
var modBody = `// Written by ctxtra setup. Offers the skills that ctxtra binds to the
// session's folder as slash commands. Safe to delete; ` + "`ctxtra setup --remove`" + ` removes it.
const bin = %s;

const skills = new Map();

async function binary($) {
	if (typeof bin === "string") return bin;
	const home = await $.env.get("HOME");
	return home ? home + "/" + bin.home : undefined;
}

async function answer($) {
	const path = await binary($);
	if (!path) return {};
	const stdin = JSON.stringify({ cwd: await $.session.cwd(), session_id: await $.session.id(), ctxtra_plugin: ` + strconv.Itoa(protocol) + ` });
	const r = await $.process.run([path, "hook", "claude", "` + pluginEvent + `"], { stdin, timeoutMs: 10000 });
	if (r.exitCode !== 0 || r.isStdoutTruncated) return {};
	return JSON.parse(r.stdout) ?? {};
}

async function start($) {
	if ((await $.session.surfaces()).length === 0) return;
	const a = await answer($);
	if (!Array.isArray(a.skills)) return;
	const taken = new Set();
	for (const c of await $.command.list()) {
		if (c.plugin !== $.plugin.name) taken.add(c.name);
	}
	for (const s of a.skills) {
		if (typeof s?.name !== "string" || typeof s.path !== "string" || taken.has(s.name) || skills.has(s.name)) continue;
		const description = String(s.description ?? "").replace(/\s+/g, " ").trim();
		try {
			await $.command.register({ name: s.name, description: (description ? description + " " : "") + "(ctxtra)" });
			skills.set(s.name, s);
		} catch {}
	}
}

// The text of SKILL.md after its frontmatter.
function body(text) {
	const m = /^---\r?\n[\s\S]*?\r?\n---[ \t]*(\r?\n|$)/.exec(text);
	return (m ? text.slice(m[0].length) : text).trim();
}

function replace(text, from, to) {
	return text.split(from).join(to);
}

export function register(on) {
	on("session.start", async ($, e, next) => {
		try {
			await start($);
		} catch {}
		return next(e);
	});
	on("command.run", async ($, e, next) => {
		const s = skills.get(e.command);
		if (!s) return next(e);
		let text;
		try {
			text = await $.fs.read(s.path);
		} catch (err) {
			return { text: "can't read " + s.path + ": " + err };
		}
		const dir = s.path.slice(0, s.path.lastIndexOf("/"));
		let instructions = replace(body(text), "${CLAUDE_SKILL_DIR}", dir);
		if (instructions.includes("$ARGUMENTS")) instructions = replace(instructions, "$ARGUMENTS", e.args);
		else if (e.args) instructions += "\n\nARGUMENTS: " + e.args;
		const prompt = "Run the " + s.name + " skill" + (e.args ? ": " + e.args : ".");
		$.clock.after(0, () => {
			$.prompt.submit({ text: prompt, asUser: true }).catch(() => {});
		});
		return {
			text: "loaded the " + s.name + " skill",
			context: ["The user ran /" + s.name + ", a skill that ctxtra added, from " + s.path +
				". Follow its instructions below. Resolve relative paths in them against " + dir + ".\n\n" + instructions],
		};
	});
}
`
