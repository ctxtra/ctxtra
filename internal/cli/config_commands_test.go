package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func readWrittenConfig(t *testing.T) *config.Config {
	t.Helper()
	data, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	c := config.Parse(data, config.Options{})
	if len(c.Errors) > 0 {
		t.Fatalf("written config has errors: %v\n%s", c.Errors, data)
	}
	return c
}

func TestCollectionsAndRepoSetsFromTheCommandLine(t *testing.T) {
	h := testenv.Home(t)
	t.Chdir(h)
	testenv.Write(t, filepath.Join(h, "a: b #1.md"), "A")
	testenv.Write(t, filepath.Join(h, "'quoted'.md"), "Q")
	testenv.Skill(t, filepath.Join(h, "skills", "yes"), "description: Yes.")
	testenv.Config(t, h, config.NewFile+"mcp-servers:\n  'null': {url: https://example.com/mcp}\n  'off': {url: https://example.com/off}\ncollections:\n  'on': {mcp-servers: ['off']}\n  'no': {mcp-servers: ['off']}\n")
	commands := [][]string{
		{"collection", "add", "yes", "--instruction", "./a: b #1.md", "--instruction", "'quoted'.md", "--skill", "skills/*", "--collection", "on", "--collection", "no", "--collection", "!no", "--mcp-server", "null", "--mcp-server", "!off"},
		{"repo-set", "add", "on", "--repo", "github.com/acme/1.10", "--repo", "!github.com/acme/null"},
		{"repo-set", "add", "parts", "--repo", "github.com/acme/mono", "--subdir", "yes", "--subdir", "!null", "--subdir", "a: b #1/**", "--subdir", "'q'/**"},
	}
	for _, args := range commands {
		if n, out, err := run(args...); n != 0 {
			t.Fatalf("%v: %d %s %s", args, n, out, err)
		}
	}
	c := readWrittenConfig(t)
	got := c.Collection("yes")
	if !reflect.DeepEqual(entryTexts(got.Instructions), []string{"~/a: b #1.md", "~/'quoted'.md"}) || !reflect.DeepEqual(entryTexts(got.Collections), []string{"on", "no", "!no"}) || !reflect.DeepEqual(entryTexts(got.MCPServers), []string{"null", "!off"}) || !reflect.DeepEqual(entryTexts(got.Skills), []string{"~/skills/*"}) {
		t.Fatalf("collection lost text: %#v", got.Content)
	}
	set := c.RepoSet("on")
	if len(set.Entries) != 2 || set.Entries[0].Repo != "github.com/acme/1.10" || !set.Entries[1].Not || set.Entries[1].Repo != "github.com/acme/null" {
		t.Fatalf("set: %#v", set)
	}
	if got := entryTexts(c.RepoSet("parts").Entries[0].Subdirs); !reflect.DeepEqual(got, []string{"yes", "!null", "a: b #1/**", "'q'/**"}) {
		t.Fatalf("subdirs: %q", got)
	}
	for _, args := range commands {
		before, _ := os.ReadFile(paths.ConfigPath())
		if n, out, err := run(args...); n != 0 || !strings.Contains(out, "already") {
			t.Fatalf("same entry: %d %s %s", n, out, err)
		}
		after, _ := os.ReadFile(paths.ConfigPath())
		if !bytes.Equal(before, after) {
			t.Fatal("same entry changed config")
		}
	}
}

func TestAnyEntryFromAFile(t *testing.T) {
	for _, command := range []string{"collection", "repo-set"} {
		t.Run(command, func(t *testing.T) {
			h := testenv.Home(t)
			testenv.Write(t, filepath.Join(h, "yes"), "YES")
			entry := "instructions: ['~/yes']\n"
			if command == "repo-set" {
				entry = "- repo: github.com/acme/a\n  subdirs: [yes, '!null']\n- repo: github.com/acme/b\n  subdirs: ['a: b/**']\n"
			}
			testenv.Write(t, filepath.Join(h, "entry.yaml"), entry)
			for _, replace := range []bool{true, false, true} {
				args := []string{command, "add", "yes", "--file", "-"}
				if replace {
					args = append(args, "--replace")
				}
				var out bytes.Buffer
				if n := Run(args, IO{In: strings.NewReader(entry), Out: &out, Err: &out}, "test"); n != 0 {
					t.Fatalf("%v: %d %s", args, n, out.String())
				}
				readWrittenConfig(t)
			}
			if n, _, e := run(command, "add", "yes", "--replace", "--file", filepath.Join(h, "entry.yaml")); n != 0 {
				t.Fatalf("file: %d %s", n, e)
			}
			if n, _, e := run(command, "remove", "yes", "--dry-run"); n != 0 {
				t.Fatalf("remove preview: %d %s", n, e)
			}
			readWrittenConfig(t)
			if n, _, e := run(command, "remove", "yes"); n != 0 {
				t.Fatalf("remove: %d %s", n, e)
			}
			c := readWrittenConfig(t)
			if len(c.Collections)+len(c.RepoSets) != 0 {
				t.Fatal("entry was not removed")
			}
		})
	}
}

func TestAnExclusionWithSourceInACollection(t *testing.T) {
	h := testenv.Home(t)
	testenv.Write(t, filepath.Join(h, "source", "yes"), "YES")
	testenv.Write(t, filepath.Join(h, "source", "no"), "NO")
	testenv.Skill(t, filepath.Join(h, "source", "skills", "yes"), "description: Yes.")
	testenv.Config(t, h, config.NewFile+"sources:\n  'on': {dir: '~/source'}\n")
	args := []string{"collection", "add", "yes", "--source", "on", "--instruction", "*", "--instruction", "!no", "--skill", "skills/*"}
	if n, out, e := run(args...); n != 0 {
		t.Fatalf("add: %d %s %s", n, out, e)
	}
	c := readWrittenConfig(t).Collection("yes")
	if !reflect.DeepEqual(entryTexts(c.Instructions), []string{"on:*", "!on:no"}) || !reflect.DeepEqual(entryTexts(c.Skills), []string{"on:skills/*"}) {
		t.Fatalf("source values: %#v", c.Content)
	}
}

func TestChangingCollectionsAndSetsInPlace(t *testing.T) {
	for _, command := range []string{"collection", "repo-set"} {
		t.Run(command, func(t *testing.T) {
			h := testenv.Home(t)
			testenv.Write(t, filepath.Join(h, "a.md"), "A")
			testenv.Write(t, filepath.Join(h, "b.md"), "B")
			flags := []string{"--instruction", "~/a.md"}
			replacement := "instructions: ['~/b.md']\n"
			if command == "repo-set" {
				flags = []string{"--repo", "github.com/acme/a"}
				replacement = "['github.com/acme/b']\n"
			}
			for _, name := range []string{"first", "middle", "last"} {
				if n, out, e := run(append([]string{command, "add", name}, flags...)...); n != 0 {
					t.Fatalf("add: %d %s %s", n, out, e)
				}
			}
			file := filepath.Join(h, "!entry: #1 'quoted'.yaml")
			testenv.Write(t, file, replacement)
			before, _ := os.ReadFile(paths.ConfigPath())
			if n, _, e := run(command, "add", "middle", "--file", file); n != 1 {
				t.Fatalf("taken name: %d %s", n, e)
			}
			after, _ := os.ReadFile(paths.ConfigPath())
			if !bytes.Equal(before, after) {
				t.Fatal("taken name changed config")
			}
			if n, _, e := run(command, "add", "middle", "--replace", "--file", file, "--dry-run"); n != 0 {
				t.Fatalf("preview: %d %s", n, e)
			}
			after, _ = os.ReadFile(paths.ConfigPath())
			if !bytes.Equal(before, after) {
				t.Fatal("preview changed config")
			}
			if n, _, e := run(command, "add", "middle", "--replace", "--file", file); n != 0 {
				t.Fatalf("replace: %d %s", n, e)
			}
			c := readWrittenConfig(t)
			var names []string
			if command == "collection" {
				for _, c := range c.Collections {
					names = append(names, c.Name)
				}
				if got := entryTexts(c.Collection("middle").Instructions); !reflect.DeepEqual(got, []string{"~/b.md"}) {
					t.Fatalf("replacement: %q", got)
				}
			} else {
				for _, s := range c.RepoSets {
					names = append(names, s.Name)
				}
				if c.RepoSet("middle").Entries[0].Repo != "github.com/acme/b" {
					t.Fatal("set was not replaced")
				}
			}
			if !reflect.DeepEqual(names, []string{"first", "middle", "last"}) {
				t.Fatalf("order: %q", names)
			}
		})
	}
}

func TestARefusedChangeChangesNothing(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want int
	}{
		{"empty collection", []string{"collection", "add", "x"}, 2},
		{"empty repo set", []string{"repo-set", "add", "x"}, 2},
		{"invalid collection name", []string{"collection", "add", "!no", "--instruction", "a.md"}, 1},
		{"invalid set name", []string{"repo-set", "add", "!no", "--repo", "github.com/a"}, 1},
		{"invalid binding name", []string{"binding", "add", "!no", "--instruction", "a.md"}, 1},
		{"missing file", []string{"collection", "add", "x", "--instruction", "gone"}, 1},
		{"missing collection", []string{"collection", "add", "x", "--collection", "missing"}, 1},
		{"missing server", []string{"collection", "add", "x", "--mcp-server", "missing"}, 1},
		{"missing source", []string{"collection", "add", "x", "--source", "missing", "--instruction", "a"}, 1},
		{"a folder in a set", []string{"repo-set", "add", "x", "--repo", "~/a"}, 1},
		{"two repositories with subdir", []string{"repo-set", "add", "x", "--repo", "github.com/a", "--repo", "github.com/b", "--subdir", "a"}, 1},
		{"no repository with subdir", []string{"repo-set", "add", "x", "--subdir", "a"}, 1},
		{"file and flags", []string{"collection", "add", "x", "--file", "-", "--instruction", "a"}, 2},
		{"a bad flag", []string{"repo-set", "add", "x", "--repos", "x"}, 2},
		{"unknown setting", []string{"config", "set", "sources", "x"}, 1},
		{"wrong setting type", []string{"config", "set", "check-releases", "!false"}, 1},
		{"extra argument", []string{"config", "unset", "check-releases", "x"}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := testenv.Home(t)
			file := testenv.Config(t, h, config.NewFile)
			if n, out, e := run(tt.args...); n != tt.want {
				t.Fatalf("%v: %d, want %d\n%s%s", tt.args, n, tt.want, out, e)
			}
			after, _ := os.ReadFile(file)
			if string(after) != config.NewFile {
				t.Fatalf("changed config: %s", after)
			}
		})
	}
}

func TestEveryListAcceptsJSON(t *testing.T) {
	testenv.Home(t)
	for _, c := range commands(tree()) {
		if c.Name() != "list" {
			continue
		}
		args := append(strings.Fields(c.CommandPath())[1:], "--json")
		if n, out, err := run(args...); n != 0 || !json.Valid([]byte(out)) {
			t.Errorf("%v: %d %s %s", args, n, out, err)
		}
	}
}

func TestConfigHelpShowsTheFormatAndSettingsCommands(t *testing.T) {
	testenv.Home(t)
	if n, out, e := run("help", "config"); n != 0 || !strings.Contains(out, "# Config spec:") {
		t.Fatalf("config format: %d %s %s", n, out, e)
	}
	if n, out, e := run("config", "--help"); n != 0 || !strings.Contains(out, "set") || !strings.Contains(out, "unset") {
		t.Fatalf("settings commands: %d %s %s", n, out, e)
	}
}
