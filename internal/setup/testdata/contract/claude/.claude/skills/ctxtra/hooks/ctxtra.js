// @ctxtra-managed
// binary: $HOME/bin/ctxtra
// protocol: 1
// Written by ctxtra setup. Offers the skills that ctxtra binds to the
// session's folder as slash commands. Safe to delete; `ctxtra setup --remove` removes it.
const bin = { home: "bin/ctxtra" };

const skills = new Map();

async function binary($) {
	if (typeof bin === "string") return bin;
	const home = await $.env.get("HOME");
	return home ? home + "/" + bin.home : undefined;
}

async function answer($) {
	const path = await binary($);
	if (!path) return {};
	const stdin = JSON.stringify({ cwd: await $.session.cwd(), session_id: await $.session.id(), ctxtra_plugin: 1 });
	const r = await $.process.run([path, "hook", "claude", "PluginStart"], { stdin, timeoutMs: 10000 });
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
