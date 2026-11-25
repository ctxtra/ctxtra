package yamlx

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		data string
		kind Kind
		line int
		want []string // parts of the error; none for a file that parses
	}{
		{name: "a config", data: "schema: ctxtra/config/v1\nbindings: []\n", kind: ConfigKind},
		{name: "schema anywhere among the top-level keys", data: "bindings: []\nschema: ctxtra/config/v1\n", kind: ConfigKind},
		{name: "a manifest saved as the config", data: "schema: ctxtra/source/v1\n", kind: ConfigKind, line: 1,
			want: []string{"this is a source manifest, not a config"}},
		{name: "a config saved as the lock", data: "schema: ctxtra/config/v1\n", kind: LockKind, line: 1,
			want: []string{"this is a config, not a lock file"}},
		{name: "an unknown schema", data: "schema: ctxtra/v1\n", kind: ConfigKind, line: 1,
			want: []string{"schema ctxtra/v1 is not a kind ctxtra reads", "schema: ctxtra/config/v1"}},
		{name: "no schema", data: "bindings: []\n", kind: ConfigKind, want: []string{"schema is missing", "schema: ctxtra/config/v1"}},
		{name: "schema twice", data: "schema: ctxtra/config/v1\nschema: ctxtra/config/v1\n", kind: ConfigKind, line: 2,
			want: []string{"schema is there twice, lines 1 and 2"}},
		{name: "schema as a number", data: "schema: 1\n", kind: ConfigKind, line: 1, want: []string{"schema must be ctxtra/config/v1"}},
		{name: "an empty file", data: "", kind: ConfigKind, want: []string{"the file is empty"}},
		{name: "only a comment", data: "# nothing yet\n", kind: ConfigKind, want: []string{"the file is empty"}},
		{name: "a list at the top", data: "- a\n", kind: ConfigKind, line: 1, want: []string{"must be a map"}},
		{name: "more than one document", data: "schema: ctxtra/config/v1\n---\nbindings: []\n", kind: ConfigKind, line: 2,
			want: []string{"more than one YAML document"}},
		{name: "an alias in a manifest",
			data: "schema: ctxtra/source/v1\ncollections:\n  base: &b {instructions: [docs/*.md]}\n  essentials: *b\n",
			kind: ManifestKind, line: 3, want: []string{"the anchor &b", "doesn't read anchors, aliases or <<"}},
		{name: "an alias without its anchor first",
			data: "schema: ctxtra/source/v1\ncollections:\n  essentials: *b\n", kind: ManifestKind, want: []string{"YAML syntax", "doesn't read anchors, aliases or <<"}},
		{name: "a merge key", data: "schema: ctxtra/config/v1\nsession-message:\n  <<: {max-skills: 0}\n", kind: ConfigKind, line: 3,
			want: []string{"the merge key <<"}},
		{name: "a tag on the whole file", data: "--- !foo\nschema: ctxtra/config/v1\n", kind: ConfigKind, line: 1,
			want: []string{"starts with a YAML tag"}},
		{name: "a config YAML can't read", data: "schema: ctxtra/config/v1\nbindings:\n  - name: me\n   skills: [a]\n", kind: ConfigKind, line: 3,
			want: []string{"YAML syntax"}},
		{name: "an unclosed list gives its own line",
			data: "schema: ctxtra/config/v1\nmax-ctx-chars: 1\nbindings: [a, b\n", kind: ConfigKind, line: 3,
			want: []string{"YAML syntax"}},
		{name: "a syntax error on the first line", data: "schema: [\n", kind: ConfigKind, line: 2, want: []string{"YAML syntax"}},
		// YAML can't read some unquoted ! entries in a flow list at all: the
		// whole file is left out, and the error says to quote the entry.
		{name: "unquoted in a flow list of a manifest",
			data: "schema: ctxtra/source/v1\ncollections:\n  essentials:\n    instructions: [docs/*.md, !docs/draft-*.md]\n",
			kind: ManifestKind, line: 4, want: []string{"YAML syntax", "quote the entry that starts with !: '!docs/draft-*.md'"}},
		{name: "unquoted in a flow list of a config",
			data: "schema: ctxtra/config/v1\nbindings:\n  - name: obs\n    location: {repos: [github.com/acme/**, !github.com/acme/legacy]}\n    skills: [a]\n",
			kind: ConfigKind, line: 4, want: []string{"YAML syntax", "quote the entry that starts with !: '!github.com/acme/legacy'"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Parse([]byte(tt.data), tt.kind)
			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				if d.Root == nil {
					t.Fatal("no root")
				}
				return
			}
			if err == nil {
				t.Fatalf("Parse: no error, want %q", tt.want)
			}
			if err.Mistake != Unreadable || err.Out() != OutFile {
				t.Errorf("mistake %d leaves out %d, want the whole file", err.Mistake, err.Out())
			}
			if err.Pos.Line != tt.line {
				t.Errorf("line %d, want %d: %v", err.Pos.Line, tt.line, err)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q doesn't say %q", err, w)
				}
			}
			if !strings.Contains(err.Error(), "nothing in the file is used") {
				t.Errorf("error %q doesn't say the file is left out", err)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	part := Part{Kind: "binding", Name: "x"}
	tests := []struct {
		name  string
		data  string
		lines []int
		want  []string
	}{
		{name: "unquoted on its own line", data: "agents:\n  - !cursor\n  - codex\n", lines: []int{3},
			want: []string{"YAML reads !cursor as a tag", "'!cursor'"}},
		{name: "unquoted pattern on its own line", data: "dirs:\n  - !~/work/clients/**\n", lines: []int{3},
			want: []string{"'!~/work/clients/**'"}},
		{name: "unquoted alone in a flow list", data: "agents: [ !cursor ]\n", lines: []int{2}, want: []string{"'!cursor'"}},
		{name: "unquoted before another entry in a flow list", data: "agents: [!cursor, codex]\n", lines: []int{2},
			want: []string{"'!cursor'"}},
		{name: "a ! and a space", data: "agents:\n  - ! cursor\n", lines: []int{3}, want: []string{"'!cursor'"}},
		{name: "a lone !", data: "agents: !\n", lines: []int{2}, want: []string{"tag"}},
		{name: "a standard tag", data: "max-ctx-chars: !!str 1\n", lines: []int{2}, want: []string{"!!str 1"}},
		{name: "a tag on a map", data: "location: !where {dirs: [/x]}\n", lines: []int{2}, want: []string{"!where"}},
		{name: "a key twice", data: "a:\n  branch: main\n  branch: next\n", lines: []int{4},
			want: []string{"branch is there twice, lines 3 and 4"}},
		{name: "quoted entries", data: "agents: ['!cursor', \"!codex\"]\nx: |\n  !text\n"},
		{name: "a ! inside an entry", data: "agents: [a!b]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, perr := Parse([]byte("schema: ctxtra/config/v1\n"+tt.data), ConfigKind)
			if perr != nil {
				t.Fatal(perr)
			}
			errs := d.Check(part, d.Root)
			if len(errs) != len(tt.lines) {
				t.Fatalf("errors %v, want %d", errs, len(tt.lines))
			}
			for i, err := range errs {
				if err.Pos.Line != tt.lines[i] {
					t.Errorf("line %d, want %d", err.Pos.Line, tt.lines[i])
				}
				if err.Out() != OutPart || err.Part != part {
					t.Errorf("%v leaves out %d of %v, want the binding", err, err.Out(), err.Part)
				}
				if !strings.HasSuffix(err.Error(), "; binding x is left out") {
					t.Errorf("error %q doesn't say what is left out", err)
				}
			}
			for _, w := range tt.want {
				if !strings.Contains(errs[0].Error(), w) {
					t.Errorf("error %q doesn't say %q", errs[0], w)
				}
			}
		})
	}
}

func TestPairs(t *testing.T) {
	d, perr := Parse([]byte("schema: ctxtra/config/v1\nsources:\n  a: 1\n  b: 2\n  a: 3\n  1: 4\n  c: 5\n"), ConfigKind)
	if perr != nil {
		t.Fatal(perr)
	}
	pairs, errs := d.Pairs(d.Root.Content[3], func(k string) Part { return Part{Kind: "source", Name: k} })
	var keys []string
	for _, p := range pairs {
		keys = append(keys, p.Key)
	}
	if strings.Join(keys, ",") != "b,c" {
		t.Errorf("keys %v, want b,c: a name twice leaves out both, a key that isn't text leaves out itself", keys)
	}
	if len(errs) != 2 || errs[0].Part.Name != "a" || errs[0].Mistake != DuplicateKey || errs[1].Mistake != NotText {
		t.Errorf("errors %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "lines 3 and 5") {
		t.Errorf("error %q doesn't name both lines", errs[0])
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		value string
		want  string // the text; "" for an error
		err   []string
	}{
		{value: "main", want: "main"},
		{value: "'1.10'", want: "1.10"},
		// YAML 1.2: these are text.
		{value: "on", want: "on"},
		{value: "no", want: "no"},
		{value: "y", want: "y"},
		{value: "off", want: "off"},
		{value: "1.10", err: []string{"YAML reads 1.10 as a number, not text", "quote it: tag: '1.10'"}},
		{value: "10", err: []string{"a number", "tag: '10'"}},
		{value: "true", err: []string{"true or false", "tag: 'true'"}},
		{value: "2024-01-02", err: []string{"a date", "tag: '2024-01-02'"}},
		{value: "", err: []string{"tag has no value"}},
		{value: "''", err: []string{"tag is empty"}},
		{value: "[a]", err: []string{"tag must be text"}},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			d, perr := Parse([]byte("schema: ctxtra/config/v1\ntag: "+tt.value+"\n"), ConfigKind)
			if perr != nil {
				t.Fatal(perr)
			}
			r := NewReader(d, Part{Kind: "source", Name: "s"}, BrokenSource)
			got, ok := r.Text(d.Root.Content[3], "tag")
			if tt.err == nil {
				if !ok || got != tt.want || len(r.Errs) != 0 {
					t.Fatalf("Text = %q, %v, %v; want %q", got, ok, r.Errs, tt.want)
				}
				return
			}
			if ok || len(r.Errs) != 1 || !r.Broken() {
				t.Fatalf("Text = %q, %v, %v; want an error that leaves out the source", got, ok, r.Errs)
			}
			for _, w := range tt.err {
				if !strings.Contains(r.Errs[0].Error(), w) {
					t.Errorf("error %q doesn't say %q", r.Errs[0], w)
				}
			}
		})
	}
}

func TestEveryMistakeLeavesOutSomething(t *testing.T) {
	for m := Unreadable; m <= BadProfileName; m++ {
		if _, ok := leftOut[m]; !ok {
			t.Errorf("mistake %d has no row in the table", m)
		}
	}
}
