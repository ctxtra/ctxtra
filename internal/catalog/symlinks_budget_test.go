package catalog

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestDeepSymlinkBudget(t *testing.T) {
	h := testenv.Home(t)
	testenv.Write(t, filepath.Join(h, "a.md"), "REGULAR")
	n := source.MaxEntries - 1
	if testing.Short() {
		n = 256
	}
	links := make([]string, n)
	for i := range links {
		links[i] = fmt.Sprintf("l%04d/", i) + strings.Repeat("d/", 62) + "link.md"
		if err := source.CheckPath(links[i]); err != nil {
			t.Fatal(err)
		}
	}
	cat := &Catalog{}
	t.Run("named regular file", func(t *testing.T) {
		allocs := testing.AllocsPerRun(1, func() {
			index := newSymlinkTree(links)
			files, errs := cat.glob(nil, h, "a.md", "team:a.md", Files, false, index)
			if !slices.Equal(files, []string{"a.md"}) || len(errs) != 0 {
				t.Fatalf("named file: %v, %v", files, errs)
			}
		})
		if limit := float64(4*n + 1024); allocs > limit {
			t.Errorf("named read allocated %.0f objects, limit %.0f; link ancestors must not be expanded", allocs, limit)
		}
	})
	t.Run("pattern", func(t *testing.T) {
		allocs := testing.AllocsPerRun(1, func() {
			index := newSymlinkTree(links)
			files, errs := cat.glob(nil, h, "**/*.md", "team:**/*.md", Files, false, index)
			if len(files) != 0 || !strings.Contains(fmt.Sprint(errs), "more than 10,000") {
				t.Fatalf("pattern budget: %v, %v", files, errs)
			}
		})
		// Deep traversal uses about 9 allocations per budget operation; 16 leaves margin for filesystem checks.
		if limit := float64(4*n + 16*maxGlobEntries); allocs > limit {
			t.Errorf("pattern allocated %.0f objects, limit %.0f; charge metadata work before allocating", allocs, limit)
		}
	})
}

func TestSpreadSymlinkBudget(t *testing.T) {
	h := testenv.Home(t)
	var want []string
	for i := range 60 {
		name := fmt.Sprintf("docs/f%02d/a.md", i)
		testenv.Write(t, filepath.Join(h, filepath.FromSlash(name)), "REGULAR")
		want = append(want, name)
	}
	cat := &Catalog{}
	for _, tc := range []struct{ pattern, linkRoot string }{
		{"docs/*/*.md", "docs"},
		{"docs/**/*.md", "links"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			var links []string
			for i := range 240 {
				links = append(links, fmt.Sprintf("%s/f%02d/link%03d.txt", tc.linkRoot, i%60, i))
			}
			files, errs := cat.glob(nil, h, tc.pattern, "team:"+tc.pattern, Files, false, newSymlinkTree(links))
			if !slices.Equal(files, want) || len(errs) != 0 {
				t.Fatalf("spread links: files %v, errors %v", files, errs)
			}
		})
	}
}

func TestSymlinkPrefixLookup(t *testing.T) {
	index := newSymlinkTree([]string{"a/x", "a.b/x", "a-b/x"})
	for _, name := range []string{"a", "a-b", "a.b", "a.c", "a/x"} {
		t.Run(name, func(t *testing.T) {
			want := name == "a" || name == "a-b" || name == "a.b"
			dir := &tree{pattern: true, linkPaths: index.paths, left: 1}
			if found, err := dir.virtualDirectory(name); found != want || err != nil {
				t.Fatalf("virtual directory %q: %v, %v; want %v", name, found, err, want)
			}
			entries := &tree{linkPaths: index.paths, left: 1}
			if want {
				entries.left++
			}
			children, err := entries.linkChildren(name)
			if err != nil || (want && (len(children) != 1 || children["x"] == nil)) || (!want && len(children) != 0) {
				t.Fatalf("children of %q: %v, %v", name, children, err)
			}
		})
	}
}
