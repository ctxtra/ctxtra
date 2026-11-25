package yamlx

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// Text is a scalar of text v, in a style that every YAML reader reads back
// as the same text in a block map or list.
func Text(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v, Style: textStyle(v, false)}
}

// textStyle is how ctxtra writes text v: plain when it can't be read as
// anything else, in single quotes otherwise, and in double quotes when it
// has characters that only an escape can write. flow is true inside a flow
// list or map.
func textStyle(v string, flow bool) yaml.Style {
	switch {
	case strings.ContainsFunc(v, func(r rune) bool { return !unicode.IsPrint(r) || r == unicode.ReplacementChar }):
		return yaml.DoubleQuotedStyle
	case needsQuotes(v, flow):
		return yaml.SingleQuotedStyle
	}
	return 0
}

// plainWords are the plain values that YAML 1.1 reads as true/false or
// null. ctxtra reads YAML 1.2, where they are text, but other tools may not.
var plainWords = []string{"y", "yes", "n", "no", "on", "off", "true", "false", "null", "~"}

// reNumber matches what YAML 1.1 or 1.2 may read as a number.
var reNumber = regexp.MustCompile(`^[-+]?(0b[01_]+|0x[0-9a-f_]+|0o[0-7_]+|[0-9][0-9_]*(\.[0-9_]*)?(e[-+]?[0-9]+)?|\.[0-9_]+(e[-+]?[0-9]+)?|\.(inf|nan))$`)

// needsQuotes reports whether plain v could be read as something other
// than that text: a number, true/false, null, a date, an indicator such as
// ! or *, or a value whose spaces, : or # change how it reads. Only
// letters, digits and . _ / ~ @ + - stay plain, @ never first, and : only
// between other characters outside flow lists and maps, as in a URL.
func needsQuotes(v string, flow bool) bool {
	if v == "" || slices.Contains(plainWords, strings.ToLower(v)) || reNumber.MatchString(strings.ToLower(v)) {
		return true
	}
	for i, r := range v {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._/~+-", r):
		case r == '@' && i > 0:
		case r == ':' && !flow && i > 0 && i < len(v)-1:
		default:
			return true
		}
	}
	if v[0] == '-' {
		return true
	}
	var n yaml.Node
	return yaml.Unmarshal([]byte(v), &n) != nil || len(n.Content) != 1 || n.Content[0].ShortTag() != "!!str" || n.Content[0].Value != v
}

// Prepare readies n, a part that ctxtra will write, so that it reads back as
// it is: each text scalar is quoted as it needs, and a scalar without a tag
// is text. flow is true when n goes inside a flow list or map. A YAML tag,
// an anchor or an alias is an error: ctxtra writes none.
func Prepare(n *yaml.Node, flow bool) error {
	switch {
	case n.Kind == yaml.AliasNode || n.Anchor != "":
		return fmt.Errorf("line %d: ctxtra doesn't write anchors or aliases", n.Line)
	case n.Kind == yaml.ScalarNode && n.Tag == "":
		n.Tag = "!!str"
	}
	flow = flow || n.Style&yaml.FlowStyle != 0
	switch tag := n.ShortTag(); tag {
	case "!!str":
		if n.Kind == yaml.ScalarNode {
			n.Style = textStyle(n.Value, flow)
		}
	case "!!int", "!!float", "!!bool", "!!null", "!!timestamp", "!!map", "!!seq":
		n.Style &^= yaml.TaggedStyle | yaml.LiteralStyle | yaml.FoldedStyle | yaml.SingleQuotedStyle | yaml.DoubleQuotedStyle
	default:
		entry := strings.TrimSpace(tag + " " + n.Value)
		return fmt.Errorf("YAML reads %s as a tag; quote an entry that starts with !: %s", entry, Quote(entry))
	}
	for _, c := range n.Content {
		if err := Prepare(c, flow); err != nil {
			return err
		}
	}
	return nil
}

// Encode writes n as YAML, two spaces per level.
func Encode(n *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Same reports whether a and b are the same after reading the YAML:
// formatting, comments and the order of map keys don't count, the order of
// list items does.
func Same(a, b *yaml.Node) bool { return equal(a, b, false) }

// Identical is Same where the order of map keys counts too.
func Identical(a, b *yaml.Node) bool { return equal(a, b, true) }

func equal(a, b *yaml.Node, ordered bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind == yaml.DocumentNode && len(a.Content) == 1 {
		a = a.Content[0]
	}
	if b.Kind == yaml.DocumentNode && len(b.Content) == 1 {
		b = b.Content[0]
	}
	if a.Kind != b.Kind || a.ShortTag() != b.ShortTag() || len(a.Content) != len(b.Content) {
		return false
	}
	switch a.Kind {
	case yaml.ScalarNode:
		return a.Value == b.Value
	case yaml.MappingNode:
		if ordered {
			break
		}
		for i := 0; i+1 < len(a.Content); i += 2 {
			v := lookup(b, a.Content[i])
			if v == nil || !equal(a.Content[i+1], v, false) || lookup(a, a.Content[i]) != a.Content[i+1] {
				return false
			}
		}
		return true
	case yaml.AliasNode:
		return false
	}
	for i := range a.Content {
		if !equal(a.Content[i], b.Content[i], ordered) {
			return false
		}
	}
	return true
}

// lookup is the value of key k in map m, nil when there is none. With a key
// that is there twice, it is the first.
func lookup(m, k *yaml.Node) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if equal(m.Content[i], k, true) {
			return m.Content[i+1]
		}
	}
	return nil
}
