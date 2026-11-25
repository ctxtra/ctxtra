package yamlx

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// quoted are values that YAML reads as something else, or not at all,
// unless they are quoted.
var quoted = []string{
	"!cursor", "!docs/draft-*.md", "!",
	"1.10", "1", "-1", "+1", "0x1F", "0o17", "0b101", "1e3", ".5", ".inf", "-.Inf", ".NaN", "1_000",
	"on", "off", "On", "yes", "no", "y", "N", "true", "False", "null", "Null", "~",
	"2001-01-01",
	"a: b", "a:", ":a", "a #b", "#a",
	"*x", "&x", "@x", "%x", "`x", "?x", "|", ">", "-", "- x", "-x", "[x]", "{x}", ",x", "a,b",
	" lead", "trail ", "two  spaces",
	"'single'", `"double"`, `back\slash`,
	"tab\tx", "new\nline", "bell\a", " ",
	"",
}

// plain are values that read back as the same text without quotes.
var plain = []string{
	"main", "v1.4.0", "iam-team", "teams/billing", "~/dev/iam-ctx", "docs/conventions",
	"git@github.com:acme/iam-ctx.git", "https://mcp.linear.app/mcp",
	"9f8e7d6c5b4a39281706f5e4d3c2b1a098765432", "0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c",
	"café", "a+b", "user@host", "x_y",
}

// readBack writes v the way ctxtra writes it, in a block map and in a flow
// list, and returns what the strict layer reads in each.
func readBack(t *testing.T, v string) (block, flow string) {
	t.Helper()
	entry := &yaml.Node{Kind: yaml.ScalarNode, Value: v}
	list := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle, Content: []*yaml.Node{entry}}
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		Text("schema"), Text(string(ConfigKind)), Text("block"), Text(v), Text("flow"), list,
	}}
	if err := Prepare(root, false); err != nil {
		t.Fatalf("%q: %v", v, err)
	}
	data, err := Encode(root)
	if err != nil {
		t.Fatalf("%q: %v", v, err)
	}
	d, perr := Parse(data, ConfigKind)
	if perr != nil {
		t.Fatalf("%q: the strict layer refuses\n%s\n%v", v, data, perr)
	}
	r := NewReader(d, Part{Kind: "key", Name: "test"}, BadValue, d.Root)
	got := map[string]string{}
	for i := 0; i+1 < len(d.Root.Content); i += 2 {
		k, n := d.Root.Content[i].Value, d.Root.Content[i+1]
		if k == "flow" {
			n = n.Content[0]
		}
		if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
			t.Fatalf("%q: %s reads as %s\n%s", v, k, n.ShortTag(), data)
		}
		got[k] = n.Value
	}
	if len(r.Errs) > 0 {
		t.Fatalf("%q: %v\n%s", v, r.Errs, data)
	}
	return got["block"], got["flow"]
}

func TestTextReadsBack(t *testing.T) {
	for _, v := range append(append([]string{}, quoted...), plain...) {
		block, flow := readBack(t, v)
		if block != v || flow != v {
			t.Errorf("%q reads back as %q in a block map and %q in a flow list", v, block, flow)
		}
	}
}

func TestTextQuotesWhatNeedsIt(t *testing.T) {
	for _, v := range quoted {
		if Text(v).Style == 0 {
			t.Errorf("%q is written plain", v)
		}
	}
	for _, v := range plain {
		if s := Text(v).Style; s != 0 {
			t.Errorf("%q is written quoted (%v)", v, s)
		}
	}
	// In a flow list, a colon is quoted, as in a reference into a source:
	// ['iam-team:essentials'].
	n := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: "iam-team:essentials"}}}
	if err := Prepare(n, false); err != nil || n.Content[0].Style != yaml.SingleQuotedStyle {
		t.Errorf("a colon in a flow list: style %v, %v", n.Content[0].Style, err)
	}
}

func TestPrepare(t *testing.T) {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("agents: [!cursor x, codex]\n"), &n); err != nil {
		t.Fatal(err)
	}
	err := Prepare(n.Content[0], false)
	if err == nil || !strings.Contains(err.Error(), "quote an entry that starts with !") {
		t.Errorf("a tag: %v", err)
	}
	if err := yaml.Unmarshal([]byte("a: &x [1]\nb: *x\n"), &n); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(n.Content[0], false); err == nil || !strings.Contains(err.Error(), "anchors or aliases") {
		t.Errorf("an anchor: %v", err)
	}
	if err := yaml.Unmarshal([]byte("check-updates: false\ncount: 3\n"), &n); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(n.Content[0], false); err != nil {
		t.Errorf("true/false and numbers stay as they are: %v", err)
	}
	if n.Content[0].Content[1].ShortTag() != "!!bool" || n.Content[0].Content[3].ShortTag() != "!!int" {
		t.Errorf("tags changed: %s, %s", n.Content[0].Content[1].ShortTag(), n.Content[0].Content[3].ShortTag())
	}
}

func TestSame(t *testing.T) {
	node := func(s string) *yaml.Node {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(s), &n); err != nil {
			t.Fatal(err)
		}
		return &n
	}
	tests := []struct {
		a, b string
		same bool
	}{
		{"{profiles: [iam], collections: ['iam-team:essentials']}", "collections:\n  - iam-team:essentials\nprofiles: [\"iam\"]\n", true},
		{"{tag: '1.10'}", "{tag: 1.10}", false},
		{"{agents: [codex, cursor]}", "{agents: [cursor, codex]}", false},
		{"{a: 1}", "{a: 1, b: 2}", false},
		{"{a: 1, a: 1}", "{a: 1, b: 1}", false},
		{"[a, b] # one", "- a # two\n- b\n", true},
	}
	for _, tt := range tests {
		if got := Same(node(tt.a), node(tt.b)); got != tt.same {
			t.Errorf("Same(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
	if Identical(node("{a: 1, b: 2}"), node("{b: 2, a: 1}")) {
		t.Error("Identical ignores key order")
	}
}
