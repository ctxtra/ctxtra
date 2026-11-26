package trust

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestLiveFolders(t *testing.T) {
	t.Run("A folder gets a remote later", func(t *testing.T) {
		h := testenv.Home(t)
		dir := testenv.Repo(t, filepath.Join(h, "dotfiles"), "")
		var live Live
		if why := live.Check("dotfiles", "~/dotfiles", dir); why != "" {
			t.Fatal(why)
		}
		testenv.Run(t, dir, "git", "remote", "add", "origin", "git@github.com:me/dotfiles.git")
		live = Live{}
		why := live.Check("dotfiles", "~/dotfiles", dir)
		for _, text := range []string{"remotes github.com/me/dotfiles", "ctxtra source trust dotfiles", "ctxtra source add --repo ~/dotfiles"} {
			if !strings.Contains(why, text) {
				t.Errorf("%q does not contain %q", why, text)
			}
		}
	})

	t.Run("Trusted remote", func(t *testing.T) {
		h := testenv.Home(t)
		dir := testenv.Repo(t, filepath.Join(h, "dotfiles"), "git@github.com:me/dotfiles.git")
		if err := state.Trust(paths.Real(dir), []string{"github.com/me/dotfiles"}); err != nil {
			t.Fatal(err)
		}
		for _, remote := range []string{"git@github.com:me/dotfiles.git", "https://github.com/me/dotfiles"} {
			testenv.Run(t, dir, "git", "remote", "set-url", "origin", remote)
			var live Live
			if why := live.Check("dotfiles", "~/dotfiles", dir); why != "" {
				t.Fatal(why)
			}
		}
		testenv.Run(t, dir, "git", "remote", "set-url", "origin", "https://github.com/other/dotfiles")
		var live Live
		if why := live.Check("dotfiles", "~/dotfiles", dir); !strings.Contains(why, "changed since you trusted it") {
			t.Fatalf("a changed remote: %s", why)
		}
	})
}

func TestSymlinkInANestedRepositoryWithoutRemotes(t *testing.T) {
	h := testenv.Home(t)
	outer := testenv.Repo(t, filepath.Join(h, "team"), "git@github.com:acme/team.git")
	inner := testenv.Repo(t, filepath.Join(outer, "local"), "")
	testenv.Write(t, filepath.Join(h, "notes", "rules.md"), "MINE")
	link := filepath.Join(inner, "notes")
	symlink(t, filepath.Join(h, "notes"), link)
	why := Route(link, filepath.Join(h, "notes"), New(nil))
	if !strings.Contains(why, "github.com/acme/team") || !strings.Contains(why, "no trusted dir source there") {
		t.Fatalf("an untrusted outer repository controls the link: %s", why)
	}
}
