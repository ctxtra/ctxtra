package catalog

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/lock"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// A SOURCE:PATH entry finds only what is inside its source, for a live
// folder and for a commit in ctxtra's store, whatever the path.
func FuzzSourceReference(f *testing.F) {
	for _, s := range []string{"rules.md", "*.md", "**", "../rules.md", "../../outside", "/etc/passwd", "~/rules.md",
		"other:rules.md", "nested/../../outside", "..x/rules.md", "nested/..", ".", "{..,x}/rules.md", "[.][.]/outside.md"} {
		f.Add(s)
	}
	h := testenv.Home(f)
	for _, p := range []string{"live/rules.md", "live/nested/x.md", "live-other/rules.md", "outside.md", "rules.md"} {
		testenv.Write(f, filepath.Join(h, p), "x")
	}
	commit := strings.Repeat("a", 40)
	gen := source.GenDir("https://example.com/team.git", "", commit)
	for _, p := range []string{"rules.md", "nested/x.md"} {
		testenv.Write(f, filepath.Join(gen, p), "x")
	}
	testenv.Write(f, filepath.Join(filepath.Dir(gen), "rules.md"), "x")
	const sources = "sources:\n  live: {dir: ~/live}\n  pinned: {repo: 'https://example.com/team.git', branch: main}\n"
	conf := "schema: ctxtra/config/v1\n" + sources
	path := testenv.Config(f, h, conf)
	l := lock.Parse([]byte("schema: ctxtra/lock/v1\nsources:\n  pinned: {repo: 'https://example.com/team.git', branch: main, commit: '" + commit + "'}\n"))
	f.Fuzz(func(t *testing.T, ref string) {
		for _, name := range []string{"live", "pinned"} {
			text, _ := json.Marshal(name + ":" + ref)
			c := config.Parse([]byte(conf+"bindings:\n  - {name: b, instructions: ["+string(text)+"], skills: ["+string(text)+"]}\n"), config.Options{})
			if len(c.Bindings) != 1 || len(c.Bindings[0].Instructions) != 1 {
				continue
			}
			cat := New(c, l, path)
			top := cat.Source(name).Top
			if top == "" {
				t.Fatalf("source %s: %s", name, cat.Source(name).Why)
			}
			for kind, e := range map[Kind]config.Entry{Files: c.Bindings[0].Instructions[0], Skills: c.Bindings[0].Skills[0]} {
				if e.Err != nil {
					continue
				}
				items, _ := cat.Paths(e, kind)
				for _, it := range items {
					if rel, ok := paths.Rel(top, it.Path); !ok || rel == "." {
						t.Fatalf("%s escaped its source: %s", e.Text, it.Path)
					}
				}
			}
		}
	})
}
