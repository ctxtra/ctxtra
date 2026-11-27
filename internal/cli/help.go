package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/edit"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/skill"
	"github.com/ctxtra/ctxtra/internal/term"
	"github.com/ctxtra/ctxtra/internal/yamlx"
	"go.yaml.in/yaml/v3"
)

// topic is a docs page that ctxtra help shows: a file, or one section of it.
type topic struct {
	name, file, section, about string
}

var topics = []topic{
	{"config", "docs/spec.md", "", "the config format and its rules: conditions, sources, trust, updates, limits"},
	{"scenarios", "docs/scenarios.md", "", "use cases step by step, and what ctxtra protects you from"},
	{"agents", "docs/guide.md", "## Limits", "what differs between agents: size limits, resumed sessions, subagents"},
}

func findTopic(name string) (topic, bool) {
	i := slices.IndexFunc(topics, func(t topic) bool { return t.name == name })
	if i < 0 {
		return topic{}, false
	}
	return topics[i], true
}

// read returns the topic's text: the file, or its section up to the next
// heading of the same level.
func (t topic) read() (string, error) {
	b, err := ctxtra.Doc(t.file)
	if err != nil {
		return "", err
	}
	text := string(b)
	if t.section == "" {
		return text, nil
	}
	_, rest, ok := strings.Cut(text, "\n"+t.section+"\n")
	if !ok {
		return "", fmt.Errorf("%s has no section %q", t.file, t.section)
	}
	level := t.section[:strings.Index(t.section, " ")+1]
	if i := strings.Index(rest, "\n"+level); i >= 0 {
		rest = rest[:i+1]
	}
	return t.section + "\n" + rest, nil
}

// checkSkill fails unless name is one of ctxtra's skills.
func checkSkill(stdio IO, cmd, name string) bool {
	if ctxtra.Skill(name) == nil {
		fmt.Fprintf(stdio.Err, "ctxtra %s: no skill %q; skills: %s\n", cmd, name, skillNames())
		return false
	}
	return true
}

// skillInfo is a skill in the output of skill list.
type skillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	Binding     string `json:"binding,omitempty"` // the binding that adds it to sessions
}

// skillList is skill list.
func skillList(stdio IO, asJSON bool) int {
	problems := errorTexts(loadEnv().Errors)
	out := []skillInfo{}
	for _, name := range ctxtra.SkillNames() {
		i := skillInfo{Name: name, Description: description(name)}
		_, err := os.Stat(filepath.Join(paths.SkillDir(name), "SKILL.md"))
		i.Installed = err == nil
		i.Binding = skillBound(name)
		out = append(out, i)
	}
	if asJSON {
		writeJSON(stdio.Out, out, diagnostics("error", problems))
		if len(problems) > 0 {
			return 1
		}
		return 0
	}
	for _, i := range out {
		state := "not installed"
		switch {
		case i.Binding != "":
			state = "installed; binding " + i.Binding + " adds it to sessions"
		case i.Installed:
			state = "installed, but no binding adds it to sessions"
		}
		fmt.Fprintf(stdio.Out, "%s: %s\n  %s\n", i.Name, state, i.Description)
	}
	for _, p := range problems {
		fmt.Fprintf(stdio.Err, "error: %s\n", p)
	}
	if len(problems) > 0 {
		return 1
	}
	return 0
}

// description is the description in the front matter of a skill's
// SKILL.md.
func description(name string) string {
	b, err := fs.ReadFile(ctxtra.Skill(name), "SKILL.md")
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if d, ok := strings.CutPrefix(sc.Text(), "description:"); ok {
			return strings.Trim(strings.TrimSpace(d), `"'`)
		}
	}
	return ""
}

// skillShow is skill show: SKILL.md of the skill, then each other file of
// it after a line that names it, so that the output pasted into an agent is
// the whole skill.
func skillShow(stdio IO, name string) int {
	if !checkSkill(stdio, "skill show", name) {
		return 1
	}
	if err := printSkill(stdio.Out, name); err != nil {
		fmt.Fprintf(stdio.Err, "ctxtra skill show: %v\n", err)
		return 1
	}
	return 0
}

func printSkill(w io.Writer, name string) error {
	src := ctxtra.Skill(name)
	b, err := fs.ReadFile(src, "SKILL.md")
	if err != nil {
		return err
	}
	w.Write(b)
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "SKILL.md" {
			return err
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "\n<!-- ctxtra skill file: %s -->\n\n", p)
		w.Write(b)
		return nil
	})
}

// skillInstall is skill install: it writes the skill name to
// paths.SkillDir, and adds it to the skills of the binding skill.Binding,
// unless a binding has it already. It adds that binding, with a comment,
// when the config has none, and otherwise keeps its conditions.
func skillInstall(stdio IO, name string, dryRun bool) int {
	if !checkSkill(stdio, "skill install", name) {
		return 1
	}
	dir := paths.Tilde(paths.SkillDir(name))
	b := skillBound(name)
	s := newSources(stdio)
	fail := func(err error) int {
		fmt.Fprintf(stdio.Err, "ctxtra skill install: %v\n", err)
		return 1
	}
	var exists bool
	if b == "" {
		old := loadEnv().Config.Binding(skill.Binding)
		if old != nil && old.Broken() {
			return fail(fmt.Errorf("the binding %s has errors, so it can't add the skill; fix it first (see ctxtra doctor); nothing changed", skill.Binding))
		}
		exists = old != nil
		data, err := readConfig(s.file)
		if err == nil {
			var ed *config.Editor
			if ed, err = config.NewEditor(data); err == nil {
				err = putSkill(ed, dir)
			}
		}
		if err != nil {
			return fail(err)
		}
	}
	if dryRun {
		fmt.Fprintf(stdio.Out, "would write the %s skill to %s\n", name, dir)
		switch {
		case b == "" && exists:
			fmt.Fprintf(stdio.Out, "would add it to the skills of the binding %s\n", skill.Binding)
		case b == "":
			fmt.Fprintf(stdio.Out, "would add the binding %s, which adds it to every session\n", skill.Binding)
		}
		fmt.Fprintln(stdio.Out, "nothing changed (--dry-run)")
		return 0
	}
	changed, err := skill.Write(name)
	if err != nil {
		return fail(err)
	}
	if changed {
		term.Fprintf(stdio.Out, term.Green, "Installed to %s\n", dir)
	} else {
		fmt.Fprintf(stdio.Out, "The %s skill in %s is up to date.\n", name, dir)
	}
	if b != "" {
		fmt.Fprintf(stdio.Out, "Binding %s adds the skill to sessions.\n", b)
		return 0
	}
	created := s.files.Created()
	if err := s.write(func(tx *edit.Tx) error {
		if err := putSkill(tx.Config, dir); err != nil {
			return err
		}
		_, res, err := checkBinding(s.file, tx.Config.Bytes(), skill.Binding)
		if err == nil && !slices.ContainsFunc(res.Skills, func(h resolve.Hit) bool { return filepath.Dir(h.Path) == paths.Real(paths.SkillDir(name)) }) {
			err = fmt.Errorf("the binding %s leaves out %s; add it to that binding by hand", skill.Binding, dir)
		}
		return err
	}); err != nil {
		return fail(err)
	}
	switch {
	case exists:
		term.Fprintf(stdio.Out, term.Green, "Added to the binding %s in %s\n", skill.Binding, paths.Tilde(s.file))
	case !created:
		term.Fprintf(stdio.Out, term.Green, "Added the binding %s to %s\n", skill.Binding, paths.Tilde(s.file))
	}
	ready := "The skill is ready for new agent sessions in any folder."
	if exists {
		ready = "The skill is ready for new agent sessions that the binding " + skill.Binding + " applies to."
	}
	if name == skill.Base {
		ready += " Ask your agent to help you get started with ctxtra."
	}
	fmt.Fprintln(stdio.Out, ready)
	return 0
}

// putSkill adds dir to the skills of the binding skill.Binding in ed, and
// adds that binding, with skill.Comment above it, when ed has none.
func putSkill(ed *config.Editor, dir string) error {
	old, err := ed.Entry(config.Bindings, skill.Binding)
	if err != nil {
		return err
	}
	if old == nil {
		n := newBinding{Skills: []string{dir}}.node()
		n.HeadComment = skill.Comment
		_, err := ed.Add(config.Bindings, skill.Binding, n, config.Place{})
		return err
	}
	// The editor keeps the comments above the binding and on its first
	// line; written from old, they would be there twice.
	n := yamlx.Plain(old)
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "name" {
			n.Content = slices.Delete(n.Content, i, i+2)
			break
		}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value != "skills" {
			continue
		}
		list := n.Content[i+1]
		if list.Kind != yaml.SequenceNode {
			return fmt.Errorf("the binding %s has skills that aren't a list; nothing changed", skill.Binding)
		}
		list.Content = append(list.Content, yamlx.Text(dir))
		return ed.Replace(config.Bindings, skill.Binding, n)
	}
	n.Content = append(n.Content, yamlx.Text("skills"), textListNode([]string{dir}))
	return ed.Replace(config.Bindings, skill.Binding, n)
}

// skillBound is the name of the first usable binding of the config that
// adds the installed skill name, or "".
func skillBound(name string) string {
	env := loadEnv()
	dir := paths.Real(paths.SkillDir(name))
	for _, b := range env.Config.Bindings {
		if b.Broken() {
			continue
		}
		for _, e := range b.Skills {
			if e.Err == nil && !e.Not && !e.InSource && paths.Real(paths.Expand(e.Value)) == dir {
				return b.Name
			}
		}
	}
	return ""
}
