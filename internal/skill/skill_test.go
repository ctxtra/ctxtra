package skill

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ctxtra/ctxtra"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestRefresh(t *testing.T) {
	testenv.Home(t)
	const follow = "ctxtra-follow"
	file := filepath.Join(paths.SkillDir(Base), "SKILL.md")
	want, _ := fs.ReadFile(ctxtra.Skill(Base), "SKILL.md")

	// Not installed: nothing is written.
	if err := Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.SkillDir(Base)); err == nil {
		t.Fatal("Refresh installed the skill")
	}

	if changed, err := Write(Base); err != nil || !changed {
		t.Fatalf("Write: %v %v", changed, err)
	}
	if changed, err := Write(Base); err != nil || changed {
		t.Fatalf("Write again: %v %v", changed, err)
	}
	if _, err := Write("nope"); err == nil {
		t.Error("Write wrote a skill ctxtra doesn't have")
	}

	// From another version: Refresh writes this binary's, of the installed
	// skills only.
	os.WriteFile(file, []byte("old"), 0o644)
	if err := Refresh(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != string(want) {
		t.Errorf("after Refresh:\n%s", got)
	}
	if _, err := os.Stat(paths.SkillDir(follow)); err == nil {
		t.Errorf("Refresh installed %s", follow)
	}

	followFile := filepath.Join(paths.SkillDir(follow), "SKILL.md")
	os.MkdirAll(filepath.Dir(followFile), 0o755)
	os.WriteFile(followFile, []byte("old"), 0o644)
	if err := Refresh(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(followFile); string(got) == "old" {
		t.Errorf("Refresh didn't update %s", follow)
	}
}

// Codex reads a skill file with a shell command and shows the model at most
// 10,000 bytes of its output: the middle of a longer file is cut. Every file
// stays under that, with room to spare.
func TestFileSizes(t *testing.T) {
	const max = 9000
	for _, name := range ctxtra.SkillNames() {
		src := ctxtra.Skill(name)
		fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if b, _ := fs.ReadFile(src, p); len(b) > max {
				t.Errorf("%s/%s is %d bytes; keep each skill file under %d, and move details to references/", name, p, len(b), max)
			}
			return nil
		})
	}
}

// Each skill's folder name is its name, which is also the name of the
// binding that skill install adds.
func TestNames(t *testing.T) {
	names := ctxtra.SkillNames()
	if len(names) < 2 || names[0] != Base {
		t.Fatalf("skills: %v", names)
	}
	for _, name := range names {
		b, err := fs.ReadFile(ctxtra.Skill(name), "SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		if want := "---\nname: " + name + "\n"; string(b[:min(len(b), len(want))]) != want {
			t.Errorf("%s/SKILL.md doesn't start with %q", name, want)
		}
	}
}

// Install replaces the skill's folder as a whole: when writing the files or
// save fails, the folder is as it was, whether there was one or not.
func TestInstallAllOrNothing(t *testing.T) {
	want, _ := fs.ReadFile(ctxtra.Skill(Base), "SKILL.md")
	// state is the skill's folder: "" when there is none, else its
	// SKILL.md; and the names in the skills folder.
	state := func() (string, []string) {
		var names []string
		entries, _ := os.ReadDir(filepath.Dir(paths.SkillDir(Base)))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		b, err := os.ReadFile(filepath.Join(paths.SkillDir(Base), "SKILL.md"))
		if err != nil {
			return "", names
		}
		return string(b), names
	}
	for _, installed := range []bool{false, true} {
		for _, fail := range []string{"files", "save"} {
			t.Run(fail+map[bool]string{true: " over an installed skill", false: ""}[installed], func(t *testing.T) {
				testenv.Home(t)
				wantMD, wantNames := "", []string(nil)
				if installed {
					if _, err := Write(Base); err != nil {
						t.Fatal(err)
					}
					testenv.Write(t, filepath.Join(paths.SkillDir(Base), "SKILL.md"), "old")
					wantMD, wantNames = "old", []string{Base}
				}
				saved := false
				save := func() error { saved = true; return nil }
				if fail == "files" {
					// A file where a folder of the skill goes: writing fails
					// after SKILL.md.
					newTemp = func(dir, pattern string) (string, error) {
						tmp, err := os.MkdirTemp(dir, pattern)
						if err == nil {
							err = os.WriteFile(filepath.Join(tmp, "references"), nil, 0o644)
						}
						return tmp, err
					}
					t.Cleanup(func() { newTemp = os.MkdirTemp })
				} else {
					save = func() error { saved = true; return fs.ErrPermission }
				}
				if _, err := Install(Base, save); err == nil {
					t.Fatal("no error")
				}
				if saved != (fail == "save") {
					t.Errorf("save called: %v", saved)
				}
				md, names := state()
				if md != wantMD || !slices.Equal(names, wantNames) {
					t.Errorf("SKILL.md %q, folder %v; want %q, %v", md, names, wantMD, wantNames)
				}
			})
		}
	}
	testenv.Home(t)
	if changed, err := Install(Base, func() error { return nil }); err != nil || !changed {
		t.Fatalf("Install: %v %v", changed, err)
	}
	if md, names := state(); md != string(want) || !slices.Equal(names, []string{Base}) {
		t.Errorf("installed: %v", names)
	}
	if changed, err := Install(Base, func() error { return nil }); err != nil || changed {
		t.Errorf("again: %v %v", changed, err)
	}
}
