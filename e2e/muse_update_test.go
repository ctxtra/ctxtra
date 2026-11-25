package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The detached checker inherits the hook's recorded XDG values. Both sources
// are local, so no network is used.
func TestMuseBackgroundConfig(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"TMPDIR=" + filepath.Join(home, "tmp"),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid"}
	write := func(path string, b []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(dir, bin string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", bin, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	repo := func(name string) (string, string) {
		dir := filepath.Join(home, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		run(dir, "git", "init", "-q", "-b", "main")
		run(dir, "git", "commit", "-q", "--allow-empty", "-m", name)
		return dir, run(dir, "git", "rev-parse", "HEAD")
	}
	chosen, head := repo("chosen")
	other, otherHead := repo("other")
	cfg := func(repo, commit string) []byte {
		return []byte(fmt.Sprintf("schema: ctxtra/v1\ncheck-updates: false\nsources:\n  team: {local-git: %q, commit: %q}\n", repo, commit))
	}
	custom := filepath.Join(home, "custom", "ctxtra", "config.yaml")
	write(custom, cfg(chosen, head))
	write(filepath.Join(home, ".config/ctxtra/config.yaml"), cfg(other, otherHead))
	write(filepath.Join(home, "tmp", ".keep"), nil)
	env = append(env, "XDG_CONFIG_HOME="+filepath.Join(home, "custom"))
	run(home, ctxtraBin, "setup", "--agent", "muse")
	b, err := os.ReadFile(filepath.Join(home, "custom", "muse", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct{ Hooks []struct{ Command string } }
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatal(err)
	}
	env = env[:len(env)-1]
	run(home, "/bin/sh", "-c", settings.Hooks["SessionStart"][0].Hooks[0].Command)
	file := filepath.Join(home, ".local/state/ctxtra/state.json")
	deadline := time.Now().Add(15 * time.Second)
	for {
		b, _ := os.ReadFile(file)
		var state struct {
			Sources map[string]struct{ URL, Head, Checked string }
		}
		if json.Unmarshal(b, &state) == nil {
			for _, s := range state.Sources {
				if s.Checked != "" {
					if s.URL != chosen || s.Head != head || len(state.Sources) != 1 {
						t.Fatalf("checker used another config: %s", b)
					}
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("chosen source was not checked: %s", b)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
