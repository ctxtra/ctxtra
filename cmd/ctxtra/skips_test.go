package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A test may skip what the machine or the agent can't do, never behavior
// that isn't built: a testscript step that skips without a condition, or a
// Go test that skips because something is "not implemented". The e2e
// module is checked too.
func TestNoPlaceholderSkips(t *testing.T) {
	script := regexp.MustCompile(`^\s*skip(\s|$)`)
	goSkip := regexp.MustCompile(`\.Skipf?\(.*(?i:not implemented|not built)`)
	check := func(root string, match func(path string) *regexp.Regexp) {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			re := match(path)
			if d.IsDir() || re == nil {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			s := bufio.NewScanner(f)
			s.Buffer(make([]byte, 1<<20), 1<<20)
			for n := 1; s.Scan(); n++ {
				if strings.HasPrefix(s.Text(), "-- ") && strings.HasSuffix(path, ".txtar") {
					break
				}
				if re.MatchString(s.Text()) {
					t.Errorf("%s:%d skips behavior that isn't built: %s", path, n, strings.TrimSpace(s.Text()))
				}
			}
			return s.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	check("testdata/script", func(path string) *regexp.Regexp {
		if strings.HasSuffix(path, ".txtar") {
			return script
		}
		return nil
	})
	for _, root := range []string{"../../cmd", "../../internal", "../../e2e"} {
		check(root, func(path string) *regexp.Regexp {
			if strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, "skips_test.go") {
				return goSkip
			}
			return nil
		})
	}
}
