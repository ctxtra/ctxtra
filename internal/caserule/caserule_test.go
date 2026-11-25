package caserule

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestOf(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rule := Of(dir)
	if runtime.GOOS == "darwin" && rule == Unknown {
		t.Errorf("macOS answers for every local volume, got %v", rule)
	}
	if got := Of(filepath.Join(dir, "not", "there")); got != rule {
		t.Errorf("a folder that doesn't exist: %v, its parent's volume: %v", got, rule)
	}
}
