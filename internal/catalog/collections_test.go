package catalog

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// collectionsOf is the catalog of a dir source team with manifest
// collections and the files a.md and b.md, and the collections list of a
// binding.
func collectionsOf(t *testing.T, collections, list string) (*Catalog, []config.Entry) {
	t.Helper()
	c, _ := catalog(t, "sources: {team: {dir: ~/team}}\n", "", func(h string) {
		testenv.Write(t, filepath.Join(h, "team", "a.md"), "a")
		testenv.Write(t, filepath.Join(h, "team", "b.md"), "b")
		testenv.Write(t, filepath.Join(h, "team", "ctxtra-source.yaml"), "schema: ctxtra/source/v1\ncollections:\n"+collections)
	})
	b := config.Parse([]byte("schema: ctxtra/config/v1\nbindings:\n  - {name: b, collections: "+list+"}\n"), config.Options{})
	return c, b.Bindings[0].Collections
}

// repeated is a graph where each level nests the one below twice: 2^depth
// paths lead to base.
func repeated(depth int) string {
	var b strings.Builder
	b.WriteString("  base: {instructions: [a.md]}\n  c1: {collections: [base, base], instructions: [b.md]}\n")
	for i := 2; i <= depth; i++ {
		fmt.Fprintf(&b, "  c%d: {collections: [c%d, c%d]}\n", i, i-1, i-1)
	}
	return b.String()
}

func TestEachCollectionIsExpandedOnce(t *testing.T) {
	t.Run("a repeated graph with a nested collection left out", func(t *testing.T) {
		const depth = 40
		c, list := collectionsOf(t, repeated(depth), "['team:c40', '!team:base']")
		x, errs := c.Expand(list)
		if len(errs) > 0 || len(x.Errors) > 0 {
			t.Fatalf("errors %v %v", errs, x.Errors)
		}
		if got := refs(x.Files); !slices.Equal(got, []string{"team:b.md"}) {
			t.Errorf("files %v", got)
		}
		// Each collection once without the ! entry, and once with it.
		if c.expansions > 2*(depth+1) {
			t.Errorf("%d expansions for %d collections", c.expansions, depth+1)
		}
	})

	t.Run("a diamond with a ! entry inside", func(t *testing.T) {
		c, list := collectionsOf(t, "  base: {instructions: [a.md]}\n  left: {collections: [base]}\n"+
			"  right: {collections: [base, '!base'], instructions: [b.md]}\n  top: {collections: [right, left]}\n", "['team:top']")
		x, errs := c.Expand(list)
		if len(errs) > 0 || len(x.Errors) > 0 {
			t.Fatalf("errors %v %v", errs, x.Errors)
		}
		// right leaves base out; left still brings it.
		if got := refs(x.Files); !slices.Equal(got, []string{"team:b.md", "team:a.md"}) {
			t.Errorf("files %v", got)
		}
		if !slices.Equal(x.Used, []string{"team:top", "team:right", "team:left", "team:base"}) {
			t.Errorf("used %v", x.Used)
		}
	})

	t.Run("too many expansions", func(t *testing.T) {
		defer func(n int) { maxExpansions = n }(maxExpansions)
		maxExpansions = 3
		c, list := collectionsOf(t, repeated(5), "['team:c5']")
		x, _ := c.Expand(list)
		if len(x.Errors) != 1 || !strings.Contains(x.Errors[0].Error(), "need more than 3 expansions") {
			t.Errorf("errors %v", x.Errors)
		}
		// Running out is not kept: the next evaluation has its own budget.
		maxExpansions = 100
		if x, _ := c.Expand(list); len(x.Errors) != 0 || len(x.Files) != 2 {
			t.Errorf("again: files %v, errors %v", refs(x.Files), x.Errors)
		}
	})
}

func TestPatternMatchesItsOwnCollection(t *testing.T) {
	c, list := collectionsOf(t, "  all: {collections: ['*'], instructions: [a.md]}\n", "['team:all']")
	x, _ := c.Expand(list)
	if len(x.Files) != 0 || len(x.Errors) != 1 || !strings.Contains(x.Errors[0].Error(), "collection all nests itself") {
		t.Errorf("files %v, errors %v", refs(x.Files), x.Errors)
	}
}
