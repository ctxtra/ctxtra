package yamlx

import (
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Reader decodes one part of a file and collects its errors.
type Reader struct {
	Doc  *Doc
	Part Part
	// Mistake is the kind of a broken value in this part, such as
	// BrokenSource for a source.
	Mistake Mistake
	Errs    []*Error
}

// NewReader is a reader of part p of d, with the tags and duplicate keys of
// nodes already checked.
func NewReader(d *Doc, p Part, m Mistake, nodes ...*yaml.Node) *Reader {
	return &Reader{Doc: d, Part: p, Mistake: m, Errs: d.Check(p, nodes...)}
}

// Broken reports whether an error leaves out the part.
func (r *Reader) Broken() bool {
	return slices.ContainsFunc(r.Errs, func(e *Error) bool { return e.Out() >= OutPart })
}

// Fail adds an error of the reader's mistake at n.
func (r *Reader) Fail(n *yaml.Node, format string, args ...any) {
	r.Add(n, r.Mistake, format, args...)
}

// Add adds an error of mistake m at n.
func (r *Reader) Add(n *yaml.Node, m Mistake, format string, args ...any) {
	r.Errs = append(r.Errs, Errorf(n, m, r.Part, format, args...))
}

// EntryError adds an error of mistake m at n, and returns it for the entry
// that keeps it.
func (r *Reader) EntryError(n *yaml.Node, m Mistake, format string, args ...any) *Error {
	r.Add(n, m, format, args...)
	return r.Errs[len(r.Errs)-1]
}

// Map returns the values of map n by key. A key that isn't in keys is an
// error; what names the map in it.
func (r *Reader) Map(n *yaml.Node, what string, keys ...string) (map[string]*yaml.Node, bool) {
	if n.Kind != yaml.MappingNode {
		r.Fail(n, "%s must be a map", what)
		return nil, false
	}
	out := map[string]*yaml.Node{}
	ok := true
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if !slices.Contains(keys, k.Value) {
			r.Fail(k, "unknown key %q in %s (known: %s)", k.Value, what, strings.Join(keys, ", "))
			ok = false
			continue
		}
		out[k.Value] = v
	}
	return out, ok
}

// Text is the text of scalar n; key names the value in errors. A number,
// true/false or date that YAML reads is an error that says to quote it:
// ctxtra doesn't turn it back into text.
func (r *Reader) Text(n *yaml.Node, key string) (string, bool) {
	switch {
	case n.Kind != yaml.ScalarNode:
		r.Fail(n, "%s must be text", key)
	case n.ShortTag() == "!!null":
		r.Fail(n, "%s has no value", key)
	case n.ShortTag() != "!!str":
		r.Add(n, NotText, "YAML reads %s as %s, not text; quote it: %s", n.Value, typeName(n), quoteAs(key, n.Value))
	case n.Value == "":
		r.Fail(n, "%s is empty", key)
	default:
		return n.Value, true
	}
	return "", false
}

// Bool is the value of a true/false scalar.
func (r *Reader) Bool(n *yaml.Node, key string) (bool, bool) {
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!bool" {
		b, err := strconv.ParseBool(n.Value)
		if err == nil {
			return b, true
		}
	}
	r.Fail(n, "%s must be true or false", key)
	return false, false
}

// Count is the value of a whole number of 0 or more.
func (r *Reader) Count(n *yaml.Node, key string) (int, bool) {
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!int" {
		v, err := strconv.ParseInt(n.Value, 10, 0)
		if err == nil && v >= 0 {
			return int(v), true
		}
	}
	r.Fail(n, "%s must be a whole number of 0 or more", key)
	return 0, false
}

// List is the items of sequence n. Null is an empty list.
func (r *Reader) List(n *yaml.Node, key string) ([]*yaml.Node, bool) {
	switch {
	case n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null":
		return nil, true
	case n.Kind != yaml.SequenceNode:
		r.Fail(n, "%s must be a list", key)
		return nil, false
	}
	return n.Content, true
}

// IsNull reports whether n is null, as an empty value is.
func IsNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null" }

// IsEmpty reports whether n is null, an empty list or an empty map.
func IsEmpty(n *yaml.Node) bool {
	return IsNull(n) || (n.Kind == yaml.SequenceNode || n.Kind == yaml.MappingNode) && len(n.Content) == 0
}

func typeName(n *yaml.Node) string {
	switch n.ShortTag() {
	case "!!int", "!!float":
		return "a number"
	case "!!bool":
		return "true or false"
	case "!!timestamp":
		return "a date"
	}
	return strings.TrimPrefix(n.ShortTag(), "!!")
}

// quoteAs is the line to write instead: key: 'value', or 'value' for a list
// entry.
func quoteAs(key, value string) string {
	if strings.HasSuffix(key, " entry") {
		return Quote(value)
	}
	return key + ": " + Quote(value)
}
