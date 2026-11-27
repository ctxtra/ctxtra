package config

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// NewFile is the content of a new config file.
const NewFile = "schema: " + string(yamlx.ConfigKind) + "\n"

// Section is a part of the config that holds named entries: a map keyed by
// name, or the bindings list.
type Section string

const (
	Sources     Section = "sources"
	MCPServers  Section = "mcp-servers"
	Collections Section = "collections"
	RepoSets    Section = "repo-sets"
	Bindings    Section = "bindings"
)

// Kind is what the user calls one entry of the section.
func (s Section) Kind() string {
	switch s {
	case Sources:
		return "source"
	case MCPServers:
		return "MCP server"
	case Collections:
		return "collection"
	case RepoSets:
		return "repo set"
	case Bindings:
		return "binding"
	}
	return string(s)
}

func (s Section) list() bool { return s == Bindings }

// topOrder is the order of top-level keys that a new key is written in:
// before the first key of the file that comes later in it.
var topOrder = topKeys

// Place is where Add puts a new binding: last, or before or after another
// binding.
type Place struct {
	Before string
	After  string
}

// ExistsError is an Add of a name that the section has with other content.
type ExistsError struct {
	Section Section
	Name    string
}

func (e *ExistsError) Error() string {
	return fmt.Sprintf("a %s named %s already exists", e.Section.Kind(), e.Name)
}

// NotFoundError is a change of an entry that the section doesn't have.
type NotFoundError struct {
	Section Section
	Name    string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no %s named %s in the config", e.Section.Kind(), e.Name)
}

// Editor changes the config's text one entry at a time. Comments, key
// order and every line it doesn't change stay as the user wrote them.
// Each change is read back with the strict YAML layer and compared with the
// intended result before it is kept; a change that fails leaves the text as
// it was.
type Editor struct {
	start   []byte
	data    []byte
	doc     *yamlx.Doc
	changed []yamlx.Part
}

// NewEditor is an editor of the config data; empty data is a new config. A
// config that can't be read as a whole is an error: ctxtra doesn't write
// over it.
func NewEditor(data []byte) (*Editor, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte(NewFile)
	}
	d, err := yamlx.Parse(data, yamlx.ConfigKind)
	if err != nil {
		return nil, fmt.Errorf("the config can't be read: %v; fix it first", err)
	}
	// YAML reads a lone \r as a line break, and the edits count lines by \n.
	if bytes.Contains(bytes.ReplaceAll(data, []byte("\r\n"), nil), []byte("\r")) {
		return nil, errors.New("the config has line breaks written as a lone carriage return (\\r); save it with \\n or \\r\\n line breaks first")
	}
	return &Editor{start: data, data: data, doc: d}, nil
}

// Bytes is the config's text with the changes made so far.
func (e *Editor) Bytes() []byte { return e.data }

// Changed reports whether the text changed.
func (e *Editor) Changed() bool { return !bytes.Equal(e.start, e.data) }

// Entry is the entry name of section s as written, nil when there is
// none. A binding's entry includes its name.
func (e *Editor) Entry(s Section, name string) (*yaml.Node, error) {
	c, i, err := e.find(s, name)
	if err != nil || i < 0 {
		return nil, err
	}
	return entryNode(c, s, i), nil
}

// Add adds entry name to section s: a binding where at says, a map entry
// last. entry is the entry's content as in the config, for a binding
// without its name. A scalar without a tag is text. With the same entry
// already there, Add changes nothing and reports false; with another entry
// of that name, it returns an *ExistsError.
func (e *Editor) Add(s Section, name string, entry *yaml.Node, at Place) (bool, error) {
	n, err := e.newEntry(s, name, entry)
	if err != nil {
		return false, err
	}
	c, i, err := e.find(s, name)
	switch {
	case err != nil:
		return false, err
	case i >= 0 && yamlx.Same(entryNode(c, s, i), n):
		return false, nil
	case i >= 0:
		return false, &ExistsError{s, name}
	}
	ref := -1
	switch {
	case at != Place{} && !s.list():
		return false, fmt.Errorf("a %s has no place in the config's order", s.Kind())
	case at.Before != "" && at.After != "":
		return false, errors.New("a binding goes either before or after another, not both")
	case at.Before != "" || at.After != "":
		other := at.Before + at.After
		if _, ref, err = e.find(s, other); err != nil {
			return false, err
		} else if ref < 0 {
			return false, &NotFoundError{s, other}
		}
	}
	err = e.change(s, func(c *yaml.Node) {
		switch {
		case s.list() && ref >= 0 && at.After != "":
			c.Content = slices.Insert(c.Content, ref+1, n)
		case s.list() && ref >= 0:
			c.Content = slices.Insert(c.Content, ref, n)
		case s.list():
			c.Content = append(c.Content, n)
		default:
			c.Content = append(c.Content, yamlx.Text(name), n)
		}
	}, func(sp []span, indent int) (int, int, string, error) {
		text, err := render(s, name, n, indent, "")
		sep := gap(e.lines(), sp)
		switch {
		case ref >= 0 && at.Before != "":
			return sp[ref].head, sp[ref].head, text + sep, err
		case ref >= 0:
			return sp[ref].end, sp[ref].end, sep + text, err
		}
		last := sp[len(sp)-1]
		return last.end, last.end, sep + text, err
	})
	if err != nil {
		return false, err
	}
	e.changed = append(e.changed, yamlx.Part{Kind: s.Kind(), Name: name})
	return true, nil
}

// Replace changes the entry name of section s to entry, in its place.
func (e *Editor) Replace(s Section, name string, entry *yaml.Node) error {
	return e.replace(s, name, entry, yamlx.Same)
}

func (e *Editor) replace(s Section, name string, entry *yaml.Node, same func(*yaml.Node, *yaml.Node) bool) error {
	n, err := e.newEntry(s, name, entry)
	if err != nil {
		return err
	}
	c, i, err := e.find(s, name)
	switch {
	case err != nil:
		return err
	case i < 0:
		return &NotFoundError{s, name}
	case same(entryNode(c, s, i), n):
		return nil
	}
	err = e.change(s, func(c *yaml.Node) {
		if s.list() {
			c.Content[i] = n
		} else {
			c.Content[2*i+1] = n
		}
	}, func(sp []span, indent int) (int, int, string, error) {
		text, err := render(s, name, n, indent, lineComment(e.lines()[sp[i].start]))
		return sp[i].start, sp[i].end, text, err
	})
	if err != nil {
		return err
	}
	e.changed = append(e.changed, yamlx.Part{Kind: s.Kind(), Name: name})
	return nil
}

// AppendTo adds value at the end of the list key of the entry name of
// section s, or adds key with that list at the end of an entry without it.
// It edits the text: every other byte stays as it is. A layout it can't edit
// that way, such as a flow map or a flow list over several lines, is an
// error, and the text stays as it was. With value in the list already, it
// changes nothing and reports false.
func (e *Editor) AppendTo(s Section, name, key, value string) (bool, error) {
	c, i, err := e.find(s, name)
	switch {
	case err != nil:
		return false, err
	case i < 0:
		return false, &NotFoundError{s, name}
	}
	entry := entryNode(c, s, i)
	if entry.Kind != yaml.MappingNode || entry.Style&yaml.FlowStyle != 0 || len(entry.Content) == 0 {
		return false, fmt.Errorf("the %s %s isn't written as a block map, one key per line", s.Kind(), name)
	}
	keyAt := -1
	for j := 0; j+1 < len(entry.Content); j += 2 {
		if entry.Content[j].Value == key {
			keyAt = j
		}
	}
	var list *yaml.Node
	if keyAt >= 0 {
		list = entry.Content[keyAt+1]
		if list.Kind != yaml.SequenceNode {
			return false, fmt.Errorf("%s of the %s %s isn't a list", key, s.Kind(), name)
		}
		if slices.ContainsFunc(list.Content, func(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Value == value }) {
			return false, nil
		}
	}
	flow := list != nil && list.Style&yaml.FlowStyle != 0
	v := yamlx.Text(value)
	if err := yamlx.Prepare(v, flow); err != nil {
		return false, err
	}
	err = e.change(s, func(c *yaml.Node) {
		n := entryNode(c, s, i)
		if keyAt < 0 {
			n.Content = append(n.Content, yamlx.Text(key), &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{v}})
			return
		}
		l := n.Content[keyAt+1]
		l.Content = append(l.Content, v)
	}, func(sp []span, _ int) (int, int, string, error) {
		lines := e.lines()
		switch {
		case list == nil:
			text, err := renderPair(yamlx.Text(key), &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{v}},
				entry.Content[0].Column-1, "")
			return sp[i].end, sp[i].end, text, err
		case flow:
			return appendFlow(lines, list, v)
		}
		hi := sp[i].end
		if keyAt+2 < len(entry.Content) {
			hi = entry.Content[keyAt+2].Line - 1
		}
		items, ok := e.entrySpans(lines, list, true, entry.Content[keyAt].Line, hi)
		if !ok {
			return 0, 0, "", fmt.Errorf("the entries of %s can't be told apart; start each on a line of its own", key)
		}
		last := items[len(items)-1]
		text, err := renderNode(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{v}}, items[0].indent, "")
		return last.end, last.end, text, err
	})
	if err != nil {
		return false, err
	}
	e.changed = append(e.changed, yamlx.Part{Kind: s.Kind(), Name: name})
	return true, nil
}

// appendFlow adds v at the end of list, a flow list on one line.
func appendFlow(lines []string, list, v *yaml.Node) (int, int, string, error) {
	at := list.Line - 1
	for _, n := range list.Content {
		if n.Line != list.Line {
			return 0, 0, "", errors.New("it is a flow list over several lines")
		}
	}
	line := lines[at]
	content := strings.TrimRight(line, "\r\n")
	body := strings.TrimRight(strings.TrimSuffix(content, lineComment(content)), " \t")
	if !strings.HasSuffix(body, "]") {
		return 0, 0, "", errors.New("it is a flow list over several lines")
	}
	out, err := yamlx.Encode(v)
	if err != nil {
		return 0, 0, "", err
	}
	val := strings.TrimSuffix(string(out), "\n")
	inner := strings.TrimRight(body[:len(body)-1], " \t")
	if len(list.Content) > 0 {
		val = ", " + val
	}
	text := inner + val + body[len(inner):] + content[len(body):] + line[len(content):]
	return at, at + 1, text, nil
}

// PlaceError is a place given for an entry that Put replaces: a replaced
// entry keeps its place.
type PlaceError struct {
	Section Section
	Name    string
}

func (e *PlaceError) Error() string {
	return fmt.Sprintf("the %s %s exists, and a replaced %s keeps its place", e.Section.Kind(), e.Name, e.Section.Kind())
}

// Put changes the entry name of section s to entry in its place, or adds
// it where at says when the section has no such entry. A place for an
// entry that is there is a *PlaceError. changed is false when the same
// entry is there already; replaced is true when an entry was there.
func (e *Editor) Put(s Section, name string, entry *yaml.Node, at Place) (changed, replaced bool, err error) {
	_, i, err := e.find(s, name)
	switch {
	case err != nil:
		return false, false, err
	case i < 0:
		changed, err = e.Add(s, name, entry, at)
		return changed, false, err
	case at != Place{}:
		return false, true, &PlaceError{s, name}
	}
	before := e.data
	if err := e.Replace(s, name, entry); err != nil {
		return false, true, err
	}
	return !bytes.Equal(before, e.data), true, nil
}

// Remove removes the entry name of section s, and the section's key when it
// was the last entry.
func (e *Editor) Remove(s Section, name string) error {
	c, i, err := e.find(s, name)
	switch {
	case err != nil:
		return err
	case i < 0:
		return &NotFoundError{s, name}
	}
	if entries(c, s) == 1 {
		return e.change(s, nil, nil)
	}
	return e.change(s, func(c *yaml.Node) {
		if s.list() {
			c.Content = slices.Delete(c.Content, i, i+1)
		} else {
			c.Content = slices.Delete(c.Content, 2*i, 2*i+2)
		}
	}, func(sp []span, _ int) (int, int, string, error) {
		from, to := cut(e.lines(), sp, i)
		return from, to, "", nil
	})
}

// Check reports the errors that the config has in the entries this editor
// added or replaced: a write must not add a mistake. Errors elsewhere are
// the user's, and don't stop a write.
func (e *Editor) Check(opts Options) error {
	var errs []error
	for _, err := range Parse(e.data, opts).Errors {
		if err.Out() == yamlx.OutFile || slices.Contains(e.changed, err.Part) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// newEntry is entry, ready to write as the entry name of section s: a
// copy with every text scalar quoted as it needs, and for a binding, its
// name first.
func (e *Editor) newEntry(s Section, name string, entry *yaml.Node) (*yaml.Node, error) {
	if entry == nil {
		return nil, fmt.Errorf("the %s %s has no content", s.Kind(), name)
	}
	if entry.Kind == yaml.DocumentNode && len(entry.Content) == 1 {
		entry = entry.Content[0]
	}
	n := clone(entry)
	n.Style &^= yaml.FlowStyle
	_, c, err := e.section(e.doc.Root, s)
	if err != nil {
		return nil, err
	}
	flow := e.doc.Root.Style&yaml.FlowStyle != 0 || c != nil && c.Style&yaml.FlowStyle != 0
	if err := yamlx.Prepare(n, flow); err != nil {
		return nil, fmt.Errorf("%s %s: %v", s.Kind(), name, err)
	}
	// Positions of n are in the text it was parsed from, not in the
	// config's: they would make the check of the result read the wrong
	// lines.
	clearPositions(n)
	if !s.list() {
		return n, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("a binding must be a map")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "name" {
			return nil, fmt.Errorf("binding %s: the name is given apart, not as a name key", name)
		}
	}
	n.Content = append([]*yaml.Node{yamlx.Text("name"), yamlx.Text(name)}, n.Content...)
	return n, nil
}

// find is section s's node and the index of its entry name: the pair's
// index in a map, the item's in a list; -1 when it has none. A section that
// can't hold entries, or a name that is there twice, is an error.
func (e *Editor) find(s Section, name string) (*yaml.Node, int, error) {
	_, c, err := e.section(e.doc.Root, s)
	if err != nil || c == nil {
		return nil, -1, err
	}
	found := -1
	for i := range entries(c, s) {
		if entryName(c, s, i) != name {
			continue
		}
		if found >= 0 {
			return nil, -1, fmt.Errorf("the config has %s %s twice; fix it first", s.Kind(), name)
		}
		found = i
	}
	return c, found, nil
}

// section is the index of s's key among root's pairs and its value; -1 and
// nil when root has no such key, and nil when the value is empty.
func (e *Editor) section(root *yaml.Node, s Section) (int, *yaml.Node, error) {
	at := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != string(s) {
			continue
		}
		if at >= 0 {
			return -1, nil, fmt.Errorf("the config has %s twice; fix it first", s)
		}
		at = i / 2
	}
	if at < 0 {
		return -1, nil, nil
	}
	v := root.Content[2*at+1]
	want, what := yaml.MappingNode, "a map"
	if s.list() {
		want, what = yaml.SequenceNode, "a list"
	}
	switch {
	case yamlx.IsNull(v):
		return at, nil, nil
	case v.Kind != want || e.doc.CheckNode(yamlx.Part{}, v) != nil:
		return -1, nil, fmt.Errorf("%s in the config must be %s; fix it first", s, what)
	}
	return at, v, nil
}

func entries(c *yaml.Node, s Section) int {
	if s.list() {
		return len(c.Content)
	}
	return len(c.Content) / 2
}

func entryNode(c *yaml.Node, s Section, i int) *yaml.Node {
	if s.list() {
		return c.Content[i]
	}
	return c.Content[2*i+1]
}

func entryName(c *yaml.Node, s Section, i int) string {
	if !s.list() {
		return c.Content[2*i].Value
	}
	if n := c.Content[i]; n.Kind == yaml.MappingNode {
		for j := 0; j+1 < len(n.Content); j += 2 {
			if n.Content[j].Value == "name" && n.Content[j+1].Kind == yaml.ScalarNode {
				return n.Content[j+1].Value
			}
		}
	}
	return ""
}

// editFunc makes a change on the text of a block section with entries,
// whose spans are sp: it returns the lines [from, to) to replace and the
// text to put there.
type editFunc func(sp []span, indent int) (from, to int, text string, err error)

// change applies a change to section s. apply makes it on the section's
// node, which then has at least one entry; nil removes the section. edit
// makes it on the text. The change is made on the text only: every line it
// doesn't change stays as it is. A layout that can't be changed that way,
// or a result that doesn't read back as intended, is an error, and the
// text stays as it was.
func (e *Editor) change(s Section, apply func(c *yaml.Node), edit editFunc) error {
	want := clone(e.doc.Root)
	at, c, err := e.section(want, s)
	if err != nil {
		return err
	}
	switch {
	case apply == nil && at >= 0:
		want.Content = slices.Delete(want.Content, 2*at, 2*at+2)
	case apply == nil:
	case c == nil || len(c.Content) == 0:
		kind := yaml.MappingNode
		if s.list() {
			kind = yaml.SequenceNode
		}
		c = &yaml.Node{Kind: kind}
		if at >= 0 {
			want.Content[2*at+1] = c
		} else {
			i := topIndex(want, string(s))
			want.Content = slices.Insert(want.Content, 2*i, yamlx.Text(string(s)), c)
		}
		apply(c)
	default:
		apply(c)
	}
	out, err := e.editText(s, want, edit)
	if err != nil {
		return fmt.Errorf("can't change %s in the config: %w; nothing changed", s, err)
	}
	return e.keep(s, out, want)
}

// keep makes out the editor's text when it reads back as want, ! tags
// included.
func (e *Editor) keep(s Section, out []byte, want *yaml.Node) error {
	// Sessions read nothing from a config over the limit.
	if len(out) > MaxFileBytes {
		return fmt.Errorf("can't change %s in the config: the result would have %d bytes, more than the %d that ctxtra reads; nothing changed", s, len(out), MaxFileBytes)
	}
	d, perr := yamlx.Parse(out, yamlx.ConfigKind)
	switch {
	case perr != nil:
		return fmt.Errorf("can't change %s in the config: the result doesn't read (%v); nothing changed; make the change by hand", s, perr)
	case !yamlx.IdenticalIn(e.doc, want, d, d.Root):
		return fmt.Errorf("can't change %s in the config: the result doesn't read back as intended; nothing changed; make the change by hand", s)
	}
	e.data, e.doc = out, d
	return nil
}

// editText makes the change on the text: it removes or adds the key of
// section s, writes the first entry of an empty section on its key's line,
// or runs edit on the entries of a block section.
func (e *Editor) editText(s Section, want *yaml.Node, edit editFunc) ([]byte, error) {
	root := e.doc.Root
	if root.Style&yaml.FlowStyle != 0 {
		return nil, errors.New("the config is written as one flow map ({...}); write its top-level keys in block style, one per line")
	}
	lines := e.lines()
	top, ok := e.entrySpans(lines, root, false, 0, len(lines))
	if !ok {
		return nil, errors.New("its top-level keys can't be told apart; start each on a line of its own")
	}
	at, c, err := e.section(root, s)
	if err != nil {
		return nil, err
	}
	wantAt := -1
	for i := 0; i+1 < len(want.Content); i += 2 {
		if want.Content[i].Value == string(s) {
			wantAt = i
		}
	}
	block := fmt.Sprintf("write %s in block style, one entry per line, or make the change by hand", s)
	switch {
	case wantAt < 0:
		from, to := cut(lines, top, at)
		return splice(lines, from, to, ""), nil
	case at < 0:
		text, err := renderPair(yamlx.Text(string(s)), want.Content[wantAt+1], indentOf(top), "")
		if err != nil {
			return nil, err
		}
		sep := gap(lines, top)
		if i := topIndex(root, string(s)); i < len(top) {
			return splice(lines, top[i].head, top[i].head, text+sep), nil
		}
		end := len(lines)
		if len(top) > 0 {
			end = top[len(top)-1].end
		}
		return splice(lines, end, end, sep+text), nil
	case c == nil || len(c.Content) == 0:
		if top[at].end != top[at].start+1 {
			return nil, fmt.Errorf("it is empty over several lines; %s", block)
		}
		text, err := renderPair(root.Content[2*at], want.Content[wantAt+1], top[at].indent, lineComment(lines[top[at].start]))
		if err != nil {
			return nil, err
		}
		return splice(lines, top[at].start, top[at].end, text), nil
	case c.Style&yaml.FlowStyle != 0:
		return nil, fmt.Errorf("it is written in flow style; %s", block)
	}
	end := len(lines)
	if at+1 < len(top) {
		end = top[at+1].head
	}
	sp, ok := e.entrySpans(lines, c, s.list(), top[at].start+1, end)
	if !ok {
		return nil, fmt.Errorf("its entries can't be told apart; start each entry on a line of its own%s, or make the change by hand",
			map[bool]string{true: " (- name: ...)"}[s.list()])
	}
	from, to, text, err := edit(sp, sp[0].indent)
	if err != nil {
		return nil, err
	}
	return splice(lines, from, to, text), nil
}

func indentOf(top []span) int {
	if len(top) == 0 {
		return 0
	}
	return top[0].indent
}

// topIndex is the place among root's pairs where the top-level key k goes.
func topIndex(root *yaml.Node, k string) int {
	rank := slices.Index(topOrder, k)
	for i := 0; i+1 < len(root.Content); i += 2 {
		if r := slices.Index(topOrder, root.Content[i].Value); r > rank {
			return i / 2
		}
	}
	return len(root.Content) / 2
}

// lines are the text's lines, each with its line break; the last may have
// none.
func (e *Editor) lines() []string {
	lines := strings.SplitAfter(string(e.data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// span is where an entry of a block map or list is in the file, as line
// indexes from 0.
type span struct {
	// head is the entry's first line, with the comments right above it at
	// its indentation.
	head int
	// start is the line of its key, or of its - in a list.
	start int
	// end is the line after its last one, without the blank lines and
	// comments that follow it: they belong to no entry, so the editor
	// neither moves nor removes them.
	end    int
	indent int
}

// entrySpans are the spans of the entries of the block map or list c,
// whose lines are within [lo, hi). It fails when an entry doesn't start
// its line: a key after other text, or a list item whose - is on a line of
// its own.
func (e *Editor) entrySpans(lines []string, c *yaml.Node, list bool, lo, hi int) ([]span, bool) {
	var out []span
	step := 2
	if list {
		step = 1
	}
	for i := 0; i < len(c.Content); i += step {
		n := c.Content[i]
		line := n.Line - 1
		if line < lo || line >= hi {
			return nil, false
		}
		prefix := []rune(lines[line])
		if n.Column-1 > len(prefix) {
			return nil, false
		}
		prefix = prefix[:n.Column-1]
		indent := len(prefix)
		if list {
			trimmed := strings.TrimRight(string(prefix), " ")
			if !strings.HasSuffix(trimmed, "-") || len(trimmed) == len(prefix) {
				return nil, false
			}
			indent = len(trimmed) - 1
			prefix = []rune(trimmed[:indent])
		}
		if strings.Trim(string(prefix), " ") != "" {
			return nil, false
		}
		head := line
		for head-1 >= lo && commentIndent(lines[head-1]) == indent {
			head--
		}
		out = append(out, span{head: head, start: line, indent: indent})
	}
	for i := range out {
		end := hi
		if i+1 < len(out) {
			end = out[i+1].head
		}
		for end-1 > out[i].start && (strings.TrimSpace(lines[end-1]) == "" || commentIndent(lines[end-1]) >= 0) {
			end--
		}
		out[i].end = end
	}
	return out, true
}

// commentIndent is the indentation of a comment line, -1 for other lines.
func commentIndent(l string) int {
	t := strings.TrimLeft(l, " ")
	if !strings.HasPrefix(t, "#") {
		return -1
	}
	return len(l) - len(t)
}

// gap is "\n" when the entries of sp are apart by blank lines, as the new
// one then should be.
func gap(lines []string, sp []span) string {
	for i := 0; i+1 < len(sp); i++ {
		for _, l := range lines[sp[i].end:sp[i+1].head] {
			if strings.TrimSpace(l) == "" {
				return "\n"
			}
		}
	}
	return ""
}

// cut is the lines to delete to remove entry i of sp: the entry, and the
// blank lines that set it apart from the next entry, or for the last, from
// the one before.
func cut(lines []string, sp []span, i int) (int, int) {
	from, to := sp[i].head, sp[i].end
	if i+1 < len(sp) {
		for to < sp[i+1].head && strings.TrimSpace(lines[to]) == "" {
			to++
		}
	} else if i > 0 {
		for from > sp[i-1].end && strings.TrimSpace(lines[from-1]) == "" {
			from--
		}
	}
	return from, to
}

// splice replaces lines [from, to) with text.
func splice(lines []string, from, to int, text string) []byte {
	var b strings.Builder
	for _, l := range lines[:from] {
		b.WriteString(l)
	}
	if text != "" && from > 0 && !strings.HasSuffix(lines[from-1], "\n") {
		b.WriteString("\n")
	}
	b.WriteString(text)
	for _, l := range lines[to:] {
		b.WriteString(l)
	}
	return []byte(b.String())
}

// render is the entry name of section s as lines of YAML at indent, with
// comment, when there is one, on its first line.
func render(s Section, name string, n *yaml.Node, indent int, comment string) (string, error) {
	if s.list() {
		return renderNode(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{n}}, indent, comment)
	}
	return renderPair(yamlx.Text(name), n, indent, comment)
}

// renderPair is the map entry of key k and value v as lines of YAML.
func renderPair(k, v *yaml.Node, indent int, comment string) (string, error) {
	k = clone(k)
	k.HeadComment, k.LineComment, k.FootComment = "", "", ""
	return renderNode(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{k, v}}, indent, comment)
}

func renderNode(n *yaml.Node, indent int, comment string) (string, error) {
	out, err := yamlx.Encode(n)
	if err != nil {
		return "", err
	}
	pad := strings.Repeat(" ", indent)
	var b strings.Builder
	for l := range strings.Lines(string(out)) {
		// YAML writes a long key, or one with a line break, as an explicit
		// key (? KEY), which the edits can't find again.
		if t := strings.TrimLeft(l, " -"); t == "?\n" || strings.HasPrefix(t, "? ") {
			return "", errors.New("a name is too long or has a line break")
		}
		if l != "\n" {
			b.WriteString(pad)
		}
		if comment != "" {
			l = strings.TrimSuffix(l, "\n") + " " + comment + "\n"
			comment = ""
		}
		b.WriteString(l)
	}
	return b.String(), nil
}

// lineComment is the comment at the end of line l, "" when it has none.
func lineComment(l string) string {
	l = strings.TrimRight(l, "\r\n")
	var quote byte
	for i := 0; i < len(l); i++ {
		c := l[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote == '\'' && c == '\'' && i+1 < len(l) && l[i+1] == '\'':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case (c == '\'' || c == '"') && (i == 0 || strings.IndexByte(" \t[{,:-", l[i-1]) >= 0):
			quote = c
		case c == '#' && (i == 0 || l[i-1] == ' ' || l[i-1] == '\t'):
			return l[i:]
		}
	}
	return ""
}

func clearPositions(n *yaml.Node) {
	n.Line, n.Column = 0, 0
	for _, c := range n.Content {
		clearPositions(c)
	}
}

func clone(n *yaml.Node) *yaml.Node {
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, x := range n.Content {
		c.Content[i] = clone(x)
	}
	return &c
}
