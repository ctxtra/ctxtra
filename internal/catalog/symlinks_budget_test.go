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
		if limit := float64(4*n + 2*maxGlobEntries + 1024); allocs > limit {
			t.Errorf("pattern allocated %.0f objects, limit %.0f; charge metadata work before allocating", allocs, limit)
		}
	})
}
