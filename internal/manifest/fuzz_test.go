package manifest

import (
	"testing"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// A manifest comes from someone else's repository: no input may crash the
// reader, and every error says what it leaves out.
func FuzzManifest(f *testing.F) {
	for _, seed := range []string{
		"", "schema: ctxtra/source/v1\n",
		"schema: ctxtra/source/v1\ncollections: {a: {collections: [b], instructions: [docs/*.md, '!docs/x.md']}}\n",
		"schema: ctxtra/source/v1\ncollections:\n  a:\n    skills:\n      - !x\n",
		"schema: ctxtra/source/v1\nrepo-sets: {s: [github.com/a/*, {repo: github.com/a/b, subdirs: ['!x/**']}]}\n",
		"schema: ctxtra/source/v1\nmcp-servers: {jira: {url: 'https://x.example', headers: {A: b}}}\n",
		"schema: ctxtra/source/v1\nx: &a [1]\ny: *a\n",
		"schema: ctxtra/source/v1\nbindings: []\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		m := Parse(data)
		for _, e := range m.Errors {
			if e.Mistake == 0 || e.Error() == "" {
				t.Fatalf("an error without a mistake: %#v", e)
			}
			if e.Out() == yamlx.OutFile && len(m.Collections)+len(m.MCPServers)+len(m.RepoSets) > 0 {
				t.Fatal("an unreadable manifest has parts")
			}
		}
	})
}
