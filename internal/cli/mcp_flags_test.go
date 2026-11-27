package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPHeadersAndScopesAreText(t *testing.T) {
	testenv.Home(t)
	args := []string{"mcp", "add", "null", "https://example.com/mcp", "--no-login"}
	headers := map[string]string{"!X": "!null", "X-Number": "1.10", "X-Word": "on", "X-Notes": "a: b #1", "X-Equal": "a=b", "X-Quotes": "'q' \"dq\""}
	for _, key := range []string{"!X", "X-Number", "X-Word", "X-Notes", "X-Equal", "X-Quotes"} {
		args = append(args, "--header", key+"="+headers[key])
	}
	scopes := []string{"!off", "null", "1.10", "a: b #1", "'q' \"dq\""}
	for _, scope := range scopes {
		args = append(args, "--scope", scope)
	}
	if n, out, err := run(args...); n != 0 {
		t.Fatalf("add: %d %s %s", n, out, err)
	}
	srv := readWrittenConfig(t).MCPServer("null")
	if !reflect.DeepEqual(srv.Headers, headers) || !reflect.DeepEqual(srv.Scopes, scopes) {
		t.Fatalf("text changed: %#v", srv)
	}
	before, _ := os.ReadFile(paths.ConfigPath())
	if n, out, err := run(args...); n != 0 || !strings.Contains(out, "already") {
		t.Fatalf("same entry: %d %s %s", n, out, err)
	}
	after, _ := os.ReadFile(paths.ConfigPath())
	if !bytes.Equal(before, after) {
		t.Fatal("same entry changed config")
	}
}

func TestASourcesServerUnderAnotherName(t *testing.T) {
	h := testenv.Home(t)
	testenv.Write(t, filepath.Join(h, "source", "ctxtra-source.yaml"), "schema: ctxtra/source/v1\nmcp-servers:\n  jira: {url: https://example.com/jira}\n")
	testenv.Config(t, h, config.NewFile+"sources:\n  'on': {dir: '~/source'}\n")
	before, _ := os.ReadFile(paths.ConfigPath())
	if n, out, e := run("mcp", "add", "jira_iam", "--use", "on:jira", "--dry-run"); n != 0 || !strings.Contains(out, "nothing changed") {
		t.Fatalf("preview: %d %s %s", n, out, e)
	}
	after, _ := os.ReadFile(paths.ConfigPath())
	if !bytes.Equal(before, after) {
		t.Fatal("preview changed config")
	}
	if n, out, e := run("mcp", "add", "jira_iam", "--use", "on:jira"); n != 0 {
		t.Fatalf("use: %d %s %s", n, out, e)
	}
	srv := readWrittenConfig(t).MCPServer("jira_iam")
	if srv.Use() != "on:jira" || srv.URL != "" || len(srv.Command) != 0 {
		t.Fatalf("alias: %#v", srv)
	}
	if env := loadEnv(); env != nil {
		alias, ok := env.Catalog.Server("jira_iam")
		original, exists := env.Catalog.Server("on:jira")
		if !ok || !exists || alias.ID != "on:jira" || original.ID != alias.ID || original.Name != "jira_iam" {
			t.Fatalf("alias %#v, original %#v", alias, original)
		}
	}
	if _, err := os.Stat(openStore().Dir); !os.IsNotExist(err) {
		t.Fatalf("alias wrote credentials: %v", err)
	}
	before, _ = os.ReadFile(paths.ConfigPath())
	for _, tt := range []struct {
		args []string
		want int
	}{
		{[]string{"mcp", "add", "other", "--use", "on:jira"}, 1},
		{[]string{"mcp", "add", "other", "--use", "!on:jira"}, 1},
		{[]string{"mcp", "add", "other", "--use", "on:missing"}, 1},
		{[]string{"mcp", "add", "other", "--use", "on:jira", "--header", "X=yes"}, 2},
		{[]string{"mcp", "add", "other", "https://example.com", "--use", "on:jira"}, 2},
	} {
		if n, out, e := run(tt.args...); n != tt.want {
			t.Fatalf("%v: %d %s %s", tt.args, n, out, e)
		}
		after, _ := os.ReadFile(paths.ConfigPath())
		if !bytes.Equal(before, after) {
			t.Fatal("refused alias changed config")
		}
	}
}

func TestTheSameAliasRemainsSuccessfulWhenItsServerDisappears(t *testing.T) {
	for _, removeManifest := range []bool{true, false} {
		t.Run(fmt.Sprintf("manifest=%t", removeManifest), func(t *testing.T) {
			h := testenv.Home(t)
			manifest := filepath.Join(h, "source", "ctxtra-source.yaml")
			testenv.Write(t, manifest, "schema: ctxtra/source/v1\nmcp-servers:\n  jira: {url: https://example.com/jira}\n")
			testenv.Config(t, h, config.NewFile+"sources:\n  team: {dir: '~/source'}\n")
			args := []string{"mcp", "add", "jira_team", "--use", "team:jira"}
			if n, out, e := run(args...); n != 0 {
				t.Fatalf("add: %d %s %s", n, out, e)
			}
			if removeManifest {
				if err := os.Remove(manifest); err != nil {
					t.Fatal(err)
				}
			} else {
				testenv.Write(t, manifest, "schema: ctxtra/source/v1\n")
			}
			before, err := os.ReadFile(paths.ConfigPath())
			if err != nil {
				t.Fatal(err)
			}
			for _, dry := range []bool{false, true} {
				call := slices.Clone(args)
				if dry {
					call = append(call, "--dry-run")
				}
				if n, out, e := run(call...); n != 0 || !strings.Contains(out, "already") {
					t.Fatalf("same alias: %d %s %s", n, out, e)
				}
				after, _ := os.ReadFile(paths.ConfigPath())
				if !bytes.Equal(before, after) {
					t.Fatal("same alias changed config")
				}
			}
			if n, _, e := run("mcp", "add", "missing", "--use", "team:missing"); n != 1 {
				t.Fatalf("a new alias needs validation: %d %s", n, e)
			}
			testenv.Write(t, paths.ConfigPath(), "schema: [\n")
			if n, _, e := run(args...); n != 1 {
				t.Fatalf("a fatal config must still fail: %d %s", n, e)
			}
		})
	}
}
