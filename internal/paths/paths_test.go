package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestFileLocations(t *testing.T) {
	for _, scenario := range []string{"Defaults", "macOS", "Custom_state_folder", "Relative_value"} {
		t.Run(scenario, func(t *testing.T) {
			h := testenv.Home(t)
			want := []string{".config/ctxtra", ".local/share/ctxtra", ".local/state/ctxtra", ".cache/ctxtra"}
			for i, key := range XDGVars {
				switch scenario {
				case "Custom_state_folder":
					base := filepath.Join(h, "custom", key)
					t.Setenv(key, base)
					want[i] = filepath.Join(base, "ctxtra")
				case "Relative_value":
					t.Setenv(key, "relative")
				}
			}
			for i, got := range []string{ConfigDir(), DataDir(), StateDir(), CacheDir()} {
				p := want[i]
				if !filepath.IsAbs(p) {
					p = filepath.Join(h, p)
				}
				if got != p {
					t.Errorf("%s: got %q, want %q", XDGVars[i], got, p)
				}
			}
			if ConfigPath() != filepath.Join(ConfigDir(), "config.yaml") ||
				SkillDir("ctxtra") != filepath.Join(DataDir(), "skills", "ctxtra") ||
				BackupDir() != filepath.Join(StateDir(), "backups") ||
				LogPath() != filepath.Join(StateDir(), "log.jsonl") {
				t.Fatal("file outside its folder")
			}
		})
	}
}

func TestOneConfigPath(t *testing.T) {
	h := testenv.Home(t)
	t.Setenv("CTXTRA_CONFIG", filepath.Join(h, "ignored.yaml"))
	if ConfigPath() != filepath.Join(h, ".config", "ctxtra", "config.yaml") {
		t.Fatal("config override was used")
	}
	t.Setenv("CTXTRA_LOG", "~/custom.log")
	if LogPath() != filepath.Join(h, "custom.log") {
		t.Fatal("log override was ignored")
	}
}

func TestTildeReal(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real, link := filepath.Join(root, "real"), filepath.Join(root, "link")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	file := filepath.Join(real, "a", "b.md")
	outside := filepath.Join(real+"x", "b.md")
	for _, home := range []string{real, link} {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		for p, want := range map[string]string{real: "~", file: "~/a/b.md", filepath.Join(home, "a", "b.md"): "~/a/b.md", outside: outside} {
			if got := TildeReal(p); got != want {
				t.Errorf("home %s: TildeReal(%s) = %s, want %s", home, p, got, want)
			}
		}
	}
	// Saved in the config, Tilde's result has to expand to the same text.
	if got := Tilde(file); got != file {
		t.Errorf("Tilde(%s) = %s through a symlinked home", file, got)
	}
}
