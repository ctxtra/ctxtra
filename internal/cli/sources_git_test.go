package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// teamRepo is a repository srv/name served at the returned URL, with files
// committed.
func teamRepo(t *testing.T, h, name string, files map[string]string) (url, dir string) {
	t.Helper()
	dir = filepath.Join(h, "srv", name)
	for p, content := range files {
		testenv.Write(t, filepath.Join(dir, filepath.FromSlash(p)), content)
	}
	testenv.Repo(t, dir, "")
	return testenv.GitServer(t, filepath.Join(h, "srv")) + "/" + name, dir
}

// commitAll commits everything in the repository dir.
func commitAll(t *testing.T, dir string) {
	t.Helper()
	testenv.Run(t, dir, "git", "add", "-A")
	testenv.Run(t, dir, "git", "commit", "-q", "-m", "change")
}

// folderState is every file and folder under dir with its mode, size, time and
// content, for telling whether a command wrote there.
func folderState(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) && p == dir {
			return nil
		}
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		line := strings.TrimPrefix(p, dir) + " " + fi.Mode().String() + " " + fi.ModTime().String()
		if fi.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			line += " " + hex.EncodeToString(sum[:])
		}
		out = append(out, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// source update --dry-run may download into the cache and changes nothing
// else: not the config, the lock, the store in the data folder, or the
// state folder, also for a new commit, a new source and a source without
// an accepted commit.
func TestUpdateDryRunWritesOnlyTheCache(t *testing.T) {
	h := testenv.Home(t)
	url, dir := teamRepo(t, h, "team", map[string]string{"a.md": "A", "skills/x/SKILL.md": "---\ndescription: X.\n---\n"})
	other, _ := teamRepo(t, h, "other", map[string]string{"teams/billing/b.md": "B"})
	testenv.Config(t, h, "schema: ctxtra/config/v1\ncheck-releases: false\n")
	if code, out, errOut := run("source", "add", "--repo", url); code != 0 {
		t.Fatalf("add: %s%s", out, errOut)
	}
	if code, out, errOut := run("binding", "add", "team", "--instruction", "team:a.md", "--skill", "team:skills/*"); code != 0 {
		t.Fatalf("binding add: %s%s", out, errOut)
	}
	testenv.Write(t, filepath.Join(dir, "a.md"), "A2")
	testenv.Write(t, filepath.Join(dir, "skills/y/SKILL.md"), "---\nname: y\n---\n")
	commitAll(t, dir)
	// A source written by hand, without an accepted commit.
	cfg := paths.ConfigPath()
	b, _ := os.ReadFile(cfg)
	text := strings.Replace(string(b), "sources:\n", "sources:\n  billing:\n    repo: "+other+"\n    branch: main\n    subdir: teams/billing\n", 1)
	b = []byte(strings.Replace(text, "check-releases:", "  - name: more\n    instructions: ['billing:b.md']\ncheck-releases:", 1))
	testenv.Write(t, cfg, string(b))
	// Every folder exists before, so that one created by the command shows.
	folders := []string{paths.ConfigDir(), paths.DataDir(), paths.StateDir()}
	for _, f := range folders {
		if err := os.MkdirAll(f, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	before := map[string][]string{}
	for _, f := range folders {
		before[f] = folderState(t, f)
	}
	cfgText, _ := os.ReadFile(cfg)
	code, out, errOut := run("source", "update", "--dry-run")
	if code != 0 || !strings.Contains(out, "+A2") || !strings.Contains(out, "billing: no accepted commit for it yet") ||
		!strings.Contains(out, "skills/y/SKILL.md: frontmatter has no description") {
		t.Fatalf("exit %d:\n%s%s\n%s", code, out, errOut, cfgText)
	}
	if code, out, _ := run("source", "update", "--dry-run", "--json"); code != 0 || !strings.Contains(out, `"pending": true`) {
		t.Errorf("--json: exit %d\n%s", code, out)
	}
	for _, f := range folders {
		if got := folderState(t, f); !slices.Equal(got, before[f]) {
			t.Errorf("%s changed:\nbefore %q\nafter  %q", f, before[f], got)
		}
	}
	if _, err := os.Stat(source.GitDir()); err != nil {
		t.Errorf("the cache: %v", err)
	}
}

// A large product repository: source add refuses a repository over the
// limit, counted for the whole repository also with --subdir, before it
// writes anything.
func TestSourceAddSizeLimits(t *testing.T) {
	h := testenv.Home(t)
	big := strings.Repeat("x", source.MaxFileBytes+1)
	url, _ := teamRepo(t, h, "product", map[string]string{"skills/a/SKILL.md": "---\ndescription: A.\n---\n", "assets/big.bin": big})
	cfg := testenv.Config(t, h, "schema: ctxtra/config/v1\n")
	for _, c := range []struct {
		name, want string
		repoBytes  int64
	}{
		{"a file over the limit outside subdir", "larger than 10485760 bytes", 0},
		{"a repository over the limit", "over ctxtra's limit of 1 GiB", 1 << 10},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.repoBytes > 0 {
				t.Cleanup(source.LimitRepoBytes(c.repoBytes))
			}
			code, out, errOut := run("source", "add", "--repo", url, "--subdir", "skills")
			if code != 1 || !strings.Contains(errOut, c.want) {
				t.Errorf("exit %d:\n%s%s", code, out, errOut)
			}
			if b, _ := os.ReadFile(cfg); string(b) != "schema: ctxtra/config/v1\n" {
				t.Errorf("the config changed:\n%s", b)
			}
			for _, p := range []string{filepath.Join(paths.ConfigDir(), "ctxtra.lock"), source.StoreDir(), paths.StateDir()} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("%s was written", p)
				}
			}
		})
	}
}

// A large product repository, on update: a new commit over the limits is
// refused before anything uses it, also when it changes nothing under the
// source's subdir, and a refused update changes neither the state nor the
// data folder.
func TestUpdateRefusesACommitOverTheLimits(t *testing.T) {
	h := testenv.Home(t)
	url, dir := teamRepo(t, h, "team", map[string]string{"teams/billing/a.md": "A"})
	testenv.Config(t, h, "schema: ctxtra/config/v1\ncheck-releases: false\n")
	if code, out, errOut := run("source", "add", "--repo", url, "--subdir", "teams/billing", "--name", "billing"); code != 0 {
		t.Fatalf("add: %s%s", out, errOut)
	}
	testenv.Write(t, filepath.Join(dir, "outside.bin"), strings.Repeat("x", source.MaxFileBytes+1))
	commitAll(t, dir)
	code, out, errOut := run("source", "update", "billing", "--dry-run")
	if code != 1 || !strings.Contains(errOut, "outside.bin is larger than") || strings.Contains(out, "up to date") {
		t.Errorf("dry run: exit %d\n%s%s", code, out, errOut)
	}

	// A real update, through the whole command: the accepted commit stays in
	// the store, and an agent found since the last setup would be recorded
	// by the notices after a command.
	testenv.Write(t, filepath.Join(dir, "teams/billing/a.md"), "A2")
	commitAll(t, dir)
	testenv.Write(t, filepath.Join(paths.StateDir(), "setup.json"), `{"installed": {}, "detected": []}`)
	if err := os.MkdirAll(filepath.Join(h, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	interactive := Interactive
	Interactive = func(IO) bool { return true }
	t.Cleanup(func() { Interactive = interactive })
	folders := []string{paths.ConfigDir(), paths.DataDir(), paths.StateDir()}
	before := map[string][]string{}
	for _, f := range folders {
		before[f] = folderState(t, f)
	}
	if len(before[paths.DataDir()]) < 3 {
		t.Fatalf("no accepted commit in the store: %q", before[paths.DataDir()])
	}
	if code, out, errOut := run("source", "update", "billing"); code != 1 || !strings.Contains(errOut, "outside.bin is larger than") {
		t.Errorf("update: exit %d\n%s%s", code, out, errOut)
	}
	for _, f := range folders {
		if got := folderState(t, f); !slices.Equal(got, before[f]) {
			t.Errorf("%s changed:\nbefore %q\nafter  %q", f, before[f], got)
		}
	}
}

// The limit of a repository with its history holds for a commit that the
// cache has from before, too: source add --rev with its full ID.
func TestSourceAddRevChecksTheCachedRepository(t *testing.T) {
	h := testenv.Home(t)
	url, dir := teamRepo(t, h, "team", map[string]string{"a.md": "A"})
	cfg := testenv.Config(t, h, "schema: ctxtra/config/v1\ncheck-releases: false\n")
	if code, out, errOut := run("source", "add", "--repo", url); code != 0 {
		t.Fatalf("add: %s%s", out, errOut)
	}
	before, _ := os.ReadFile(cfg)
	t.Cleanup(source.LimitRepoBytes(1))
	code, out, errOut := run("source", "add", "--repo", url, "--rev", strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD")), "--name", "pinned")
	if code != 1 || !strings.Contains(errOut, "over ctxtra's limit of 1 GiB") {
		t.Errorf("exit %d\n%s%s", code, out, errOut)
	}
	if after, _ := os.ReadFile(cfg); string(after) != string(before) {
		t.Errorf("the config changed:\n%s", after)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
