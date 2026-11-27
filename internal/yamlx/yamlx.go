// Package yamlx reads ctxtra's YAML files strictly, so that a file means one
// thing: one document with a schema line, no anchors, aliases or merge keys,
// no tags, no duplicate keys, and text wherever text is expected.
package yamlx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Kind is a file's kind and version, the value of its schema key.
type Kind string

const (
	ConfigKind   Kind = "ctxtra/config/v1"
	LockKind     Kind = "ctxtra/lock/v1"
	ManifestKind Kind = "ctxtra/source/v1"
)

func (k Kind) noun() string {
	switch k {
	case ConfigKind:
		return "a config"
	case LockKind:
		return "a lock file"
	case ManifestKind:
		return "a source manifest"
	}
	return string(k)
}

// Doc is a file that passed the checks of the whole file.
type Doc struct {
	// Root is the top-level map, schema included.
	Root  *yaml.Node
	lines [][]rune
}

// Parse reads data as a file of kind. An error leaves out the whole file.
func Parse(data []byte, kind Kind) (*Doc, *Error) {
	d, top, err := decode(data, func() string { return fmt.Sprintf("the file is empty; it needs schema: %s", kind) })
	if err != nil {
		return nil, err
	}
	if top.Kind != yaml.MappingNode {
		return nil, unreadable(top.Line, "the file must be a map of keys, starting with schema: %s", kind)
	}
	d.Root = top
	if err := d.checkSchema(kind); err != nil {
		return nil, err
	}
	return d, nil
}

// ParseEntry reads data as one entry of a file, as a command takes it from
// a file or stdin: one document, without anchors, aliases, merge keys, YAML
// tags or duplicate keys. Root is the entry.
func ParseEntry(data []byte) (*Doc, *Error) {
	d, top, err := decode(data, func() string { return "the entry is empty" })
	if err != nil {
		return nil, err
	}
	d.Root = top
	if errs := d.Check(Part{}, top); len(errs) > 0 {
		return nil, errs[0]
	}
	return d, nil
}

func unreadable(line int, format string, args ...any) *Error {
	return &Error{Pos: Pos{Line: line}, Mistake: Unreadable, Msg: fmt.Sprintf(format, args...)}
}

// decode reads the one document of data, and checks what a file can't have
// anywhere: a syntax error, more documents, an anchor, alias or merge key,
// and a tag on the whole. empty is the message for a document without
// content.
func decode(data []byte, empty func() string) (*Doc, *yaml.Node, *Error) {
	fail := func(line int, format string, args ...any) (*Doc, *yaml.Node, *Error) {
		return nil, nil, unreadable(line, format, args...)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return fail(0, "%s", empty())
		}
		line, msg := syntaxError(err)
		switch at, entry := unquotedExclusion(data); {
		case entry == "":
		case at == line:
			msg += "; quote the entry that starts with !: " + Quote(entry)
		default:
			msg += fmt.Sprintf("; line %d has an entry that starts with !; quote it: %s", at, Quote(entry))
		}
		return fail(line, "%s", msg)
	}
	var next yaml.Node
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		line := 0
		if err == nil {
			line = next.Line
		}
		return fail(line, "the file has more than one YAML document; ctxtra reads only one")
	}
	d := &Doc{lines: splitLines(data)}
	if n := findAnchor(&root); n != nil {
		what := "an alias"
		switch {
		case n.Anchor != "":
			what = "the anchor &" + n.Anchor
		case n.Kind == yaml.AliasNode:
			what = "the alias *" + n.Value
		case n.Value == "<<":
			what = "the merge key <<"
		}
		return fail(n.Line, "%s: ctxtra doesn't read anchors, aliases or <<; write the values out", what)
	}
	if len(root.Content) == 0 || root.Content[0].ShortTag() == "!!null" {
		return fail(0, "%s", empty())
	}
	top := root.Content[0]
	if d.tagged(top) {
		return fail(top.Line, "the file starts with a YAML tag; remove it")
	}
	return d, top, nil
}

// checkSchema checks the schema key, wherever it is among the top-level keys.
func (d *Doc) checkSchema(kind Kind) *Error {
	var found []*yaml.Node
	for i := 0; i+1 < len(d.Root.Content); i += 2 {
		if d.Root.Content[i].Value == "schema" {
			found = append(found, d.Root.Content[i+1])
		}
	}
	switch {
	case len(found) == 0:
		return &Error{Mistake: Unreadable, Msg: fmt.Sprintf("schema is missing; %s starts with schema: %s", kind.noun(), kind)}
	case len(found) > 1:
		return &Error{Pos: At(found[1]), Mistake: Unreadable, Msg: "schema is there twice, lines " + lines(found)}
	}
	n := found[0]
	if d.tagged(n) || n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return &Error{Pos: At(n), Mistake: Unreadable, Msg: fmt.Sprintf("schema must be %s", kind)}
	}
	switch got := Kind(n.Value); got {
	case kind:
		return nil
	case ConfigKind, LockKind, ManifestKind:
		return &Error{Pos: At(n), Mistake: Unreadable, Msg: fmt.Sprintf("schema %s: this is %s, not %s", got, got.noun(), kind.noun())}
	default:
		return &Error{Pos: At(n), Mistake: Unreadable, Msg: fmt.Sprintf("schema %s is not a kind ctxtra reads; %s has schema: %s", got, kind.noun(), kind)}
	}
}

// findAnchor is the first node with an anchor or alias, or a merge key.
func findAnchor(n *yaml.Node) *yaml.Node {
	if n.Anchor != "" || n.Kind == yaml.AliasNode || n.Kind == yaml.ScalarNode && n.Tag == "!!merge" {
		return n
	}
	for _, c := range n.Content {
		if f := findAnchor(c); f != nil {
			return f
		}
	}
	return nil
}

// tagged reports whether n has a YAML tag. A non-specific tag (a lone !
// followed by a space) isn't kept in the node, so the file's text at the
// node's position shows it: a value without a tag never starts with !.
func (d *Doc) tagged(n *yaml.Node) bool {
	if n.Style&yaml.TaggedStyle != 0 {
		return true
	}
	if n.Line < 1 || n.Line > len(d.lines) {
		return false
	}
	line := d.lines[n.Line-1]
	return n.Column >= 1 && n.Column <= len(line) && line[n.Column-1] == '!'
}

// Check reports the YAML tags and duplicate keys in the trees of nodes. Each
// error leaves out p.
func (d *Doc) Check(p Part, nodes ...*yaml.Node) []*Error {
	var errs []*Error
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if d.tagged(n) {
			errs = append(errs, d.tagError(n, p))
			return
		}
		if n.Kind == yaml.MappingNode {
			for _, keys := range duplicates(n) {
				errs = append(errs, &Error{Pos: At(keys[len(keys)-1]), Mistake: DuplicateKey, Part: p,
					Msg: fmt.Sprintf("%s is there twice, lines %s", keys[0].Value, lines(keys))})
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, n := range nodes {
		if n != nil {
			walk(n)
		}
	}
	return errs
}

// CheckNode reports a YAML tag on n itself, not on its children.
func (d *Doc) CheckNode(p Part, n *yaml.Node) *Error {
	if d.tagged(n) {
		return d.tagError(n, p)
	}
	return nil
}

func (d *Doc) tagError(n *yaml.Node, p Part) *Error {
	var entry string
	switch {
	case n.Style&yaml.TaggedStyle == 0: // a non-specific tag: ! and a space
		entry = "!" + n.Value
	case strings.HasSuffix(n.Tag, ","): // in a flow list, the tag ran into the next entry
		entry = strings.TrimSuffix(n.Tag, ",")
	case n.Kind == yaml.ScalarNode && n.Value != "":
		entry = n.Tag + " " + n.Value
	default:
		entry = n.Tag
	}
	return &Error{Pos: At(n), Mistake: Tag, Part: p,
		Msg: fmt.Sprintf("YAML reads %s as a tag; quote an entry that starts with !: %s", entry, Quote(entry))}
}

// Pair is a key and its value in a map, as written.
type Pair struct {
	Key   string
	K, V  *yaml.Node
	Index int // the pair's place in the map
}

// Pairs returns the pairs of map n in file order. A key that isn't text,
// and a key that is there more than once, is left out with an error; part
// says which part each key is.
func (d *Doc) Pairs(n *yaml.Node, part func(key string) Part) ([]Pair, []*Error) {
	var errs []*Error
	dup := map[string]bool{}
	for _, keys := range duplicates(n) {
		dup[keys[0].Value] = true
		errs = append(errs, &Error{Pos: At(keys[len(keys)-1]), Mistake: DuplicateKey, Part: part(keys[0].Value),
			Msg: fmt.Sprintf("%s is there twice, lines %s", keys[0].Value, lines(keys))})
	}
	var out []Pair
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		switch {
		case d.tagged(k):
			errs = append(errs, d.tagError(k, part(k.Value)))
		case k.Kind != yaml.ScalarNode || k.ShortTag() != "!!str" || k.Value == "":
			errs = append(errs, &Error{Pos: At(k), Mistake: NotText, Part: part(k.Value), Msg: "a key must be text; quote it"})
		case !dup[k.Value]:
			out = append(out, Pair{Key: k.Value, K: k, V: v, Index: i / 2})
		}
	}
	return out, errs
}

// duplicates are the keys of map n that are there more than once, each with
// every node that has it, in file order.
func duplicates(n *yaml.Node) [][]*yaml.Node {
	byKey := map[string][]*yaml.Node{}
	var order []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		if len(byKey[k.Value]) == 0 {
			order = append(order, k.Value)
		}
		byKey[k.Value] = append(byKey[k.Value], k)
	}
	var out [][]*yaml.Node
	for _, k := range order {
		if len(byKey[k]) > 1 {
			out = append(out, byKey[k])
		}
	}
	return out
}

func lines(nodes []*yaml.Node) string {
	s := make([]int, len(nodes))
	for i, n := range nodes {
		s[i] = n.Line
	}
	return Lines(s)
}

// Lines lists line numbers: "3 and 4", "3, 4 and 9".
func Lines(lines []int) string {
	s := make([]string, len(lines))
	for i, l := range lines {
		s[i] = strconv.Itoa(l)
	}
	if len(s) == 1 {
		return s[0]
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

// Quote writes s as a single-quoted YAML scalar.
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func splitLines(data []byte) [][]rune {
	var out [][]rune
	for l := range bytes.Lines(data) {
		out = append(out, []rune(strings.TrimRight(string(l), "\r\n")))
	}
	return out
}

// parserProblems are the messages of the YAML parser, which, unlike its
// scanner, reports lines counted from 0.
var parserProblems = []string{
	"did not find expected <stream-start>",
	"did not find expected <document start>",
	"did not find expected node content",
	"did not find expected key",
	"did not find expected '-' indicator",
	"did not find expected ',' or ']'",
	"did not find expected ',' or '}'",
	"found duplicate %YAML directive",
	"found duplicate %TAG directive",
	"found incompatible YAML document",
	"found undefined tag handle",
}

var reYAMLLine = regexp.MustCompile(`^yaml: (?:line (\d+): )?`)

// syntaxError is the line and message of a YAML syntax error. The parser
// leaves out line 1, and counts some lines from 0.
func syntaxError(err error) (int, string) {
	msg := err.Error()
	m := reYAMLLine.FindStringSubmatch(msg)
	if m == nil {
		return 0, msg
	}
	msg = msg[len(m[0]):]
	line, _ := strconv.Atoi(m[1])
	switch {
	case strings.HasPrefix(msg, "unknown anchor"):
		return 0, "YAML syntax: " + msg + "; ctxtra doesn't read anchors, aliases or <<"
	case slices.Contains(parserProblems, msg) || line == 0:
		line++
	}
	return line, "YAML syntax: " + msg
}

// reUnquoted finds an entry that starts with an unquoted !, in a flow list or
// map or as a block list item or map value.
var reUnquoted = regexp.MustCompile(`(?:^\s*-|[\[{,]|:)\s+(![^!\s,\[\]{}#'"][^\s,\[\]{}#]*)|[\[{,](![^!\s,\[\]{}#'"][^\s,\[\]{}#]*)`)

// unquotedExclusion is the line and text of the first entry of data that
// starts with an unquoted !, which YAML can't read in some places.
func unquotedExclusion(data []byte) (int, string) {
	for i, l := range splitLines(data) {
		line := string(l)
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		m := reUnquoted.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		return i + 1, m[1] + m[2]
	}
	return 0, ""
}
