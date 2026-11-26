package main

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// allowedSkips are the only skips a test may have: a test that applies to
// some agents or machines only skips the others. A key is the file, from
// the repository's top, the test function the call is in, and the call's
// arguments as written. A skip of behavior that isn't built has no place
// here.
var allowedSkips = map[string]string{
	// The machine can't do what the test needs.
	`internal/fsutil/read_test.go TestReadBounded (err)`:                                                                                                "the file system can't make a FIFO",
	`internal/skill/skill_unix_test.go TestRefreshRefuses (err)`:                                                                                        "the file system can't make a FIFO",
	`internal/hook/log_unix_test.go TestLogFIFODoesNotBlock (err)`:                                                                                      "the file system can't make a FIFO",
	`internal/paths/paths_test.go TestTildeReal (err)`:                                                                                                  "the file system can't make a symlink",
	`internal/mcp/status/status_test.go TestSymlinkedFolder (err)`:                                                                                      "the file system can't make a symlink",
	`internal/fsutil/retention_test.go TestWriteAtomicFailedReplacementKeepsBackups ("directory permissions do not prevent writes for this user")`:      "root ignores permissions",
	`internal/fsutil/backup_failure_test.go TestWriteAtomicPruneFailureIsNotWriteFailure ("directory permissions do not prevent writes for this user")`: "root ignores permissions",
	`internal/fsutil/lock_unix_test.go TestLockSharedReadOnly ("root writes read-only files")`:                                                          "root ignores permissions",
	`internal/mcp/creds/key_test.go TestUnreadableKeyFile ("root reads any file")`:                                                                      "root ignores permissions",
	`internal/cli/import_unix_test.go TestNativeFilesLockedLink ("the locked folder can be read (root?)")`:                                              "root ignores permissions",
	`internal/hook/slot_test.go TestSlotsReadOnlyState ("root writes anyway")`:                                                                          "root ignores permissions",
	`internal/cli/report_test.go TestShowLongTitle ("no control characters in Windows file names")`:                                                     "Windows file names can't have control characters",
	`internal/cli/mcpproxy_unix_test.go TestProxyStartBudget ("git: %v %s", err, out)`:                                                                  "git can't make a repository here",
	`internal/agent/dsh/dsh_test.go TestPluginRuns ("node not found")`:                                                                                  "the plugin runs in Node.js, which the machine may not have",
	`internal/agent/opencode/opencode_test.go TestPluginRuns ("node not found")`:                                                                        "the plugin runs in Node.js, which the machine may not have",
	`internal/agent/pi/pi_test.go TestExtensionRuns ("node not found")`:                                                                                 "the extension runs in Node.js, which the machine may not have",
	`internal/agent/hermes/hermes_test.go TestPluginRuns ("python3 not found")`:                                                                         "the plugin runs in Python, which the machine may not have",
	`internal/agent/hermes/hermes_test.go TestPluginTimeout ("python3 not found")`:                                                                      "the plugin runs in Python, which the machine may not have",
	`internal/mcp/creds/multiprocess_test.go TestProcessesRefreshAtOnce ("starts processes")`:                                                           "-short leaves out tests that start processes",
	`internal/mcp/creds/multiprocess_test.go TestProcessRefreshRacesLogin ("starts processes")`:                                                         "-short leaves out tests that start processes",
	`internal/mcp/creds/multiprocess_test.go TestProcessRefreshRacesRemove ("starts processes")`:                                                        "-short leaves out tests that start processes",
	`internal/mcp/creds/multiprocess_test.go TestProcessDiesHoldingTheLock ("starts processes")`:                                                        "-short leaves out tests that start processes",
	`e2e/native_test.go checkNativeFiles ("the disk isn't case-sensitive")`:                                                                             "the disk isn't case-sensitive",
	`e2e/tui_test.go tmuxBin ("tmux not found")`:                                                                                                        "the TUI tests need tmux",
	`e2e/harness_test.go forEachAgent ("-agent", *agentFlag)`:                                                                                           "-agent runs one agent",
	`e2e/harness_test.go forEachAgent ("%s doesn't support %s", d.Name(), c)`:                                                                           "the agent lacks a capability the test needs",
	`e2e/harness_test.go forEachAgent (err)`:                                                                                                            "the agent isn't installed; CTXTRA_E2E_REQUIRE makes it fail instead",
	`e2e/mcpprobe/probe_test.go TestProbe (err)`:                                                                                                        "the agent isn't installed",
	// The test is about some agents only.
	`internal/cli/contract_test.go TestClientContract ("an MCP entry: TestMCPClientContract")`:                                                              "TestMCPClientContract checks MCP entries",
	`internal/cli/contract_test.go TestClientContract ("no hook commands")`:                                                                                 "the setup has no hook commands",
	`e2e/mcp_proxy_test.go mcpSession ("no MCP entry for " + a.Name())`:                                                                                     "the agent has no MCP entry",
	`e2e/mcp_test.go TestMCPInstructions ("checked in Claude Code and Codex")`:                                                                              "the agents that show the proxy's instructions",
	`e2e/tui_test.go TestCodexTrustTUI ("Codex only")`:                                                                                                      "Codex's hook trust in its TUI",
	`e2e/tui_test.go TestCodexUpgradeKeepsTrust ("Codex only")`:                                                                                             "Codex's hook trust in its TUI",
	`e2e/upgrade_test.go TestUpgradeFromRelease ("no client fixture")`:                                                                                      "agents with a setup of an earlier release",
	`e2e/copilot_test.go TestCopilotCompactionDropsText ("Copilot only")`:                                                                                   "Copilot's compaction",
	`e2e/capacity_test.go TestCopilotDenseText ("Copilot only")`:                                                                                            "Copilot's limit",
	`e2e/mcp_remote_test.go TestMCPSignInDuringSession ("checked in Claude Code, which takes tool list changes")`:                                           "only Claude Code takes tool list changes",
	`e2e/mcp_opencode_test.go TestMCPOpenCodeService ("OpenCode's shared service")`:                                                                         "OpenCode's shared service",
	`e2e/profiles_test.go TestProfileVariable ("Muse runs hooks without the shell's environment: it sees only the stored selection")`:                       "Muse runs hooks without the shell's environment",
	`e2e/profiles_test.go TestProfileVariableReachesTheMCPProxy ("Muse clears the environment of hooks and MCP servers; tested with the stored selection")`: "Muse clears the environment of hooks and MCP servers",
	`e2e/profiles_test.go TestProfileMuseHooksWithoutTheEnvironment ("Muse clears the shell environment")`:                                                  "Muse's hooks and proxy without the environment",
	`e2e/profile_clear_test.go TestProfileClearStartsOver ("OpenCode's /clear")`:                                                                            "OpenCode's /clear in its TUI",
}

// A test may skip what the machine or the agent can't do, from the list
// above, and never anything else: every Skip, Skipf and SkipNow call in the
// Go tests, the e2e module included, and every skip step of a testscript,
// with or without a condition.
func TestNoPlaceholderSkips(t *testing.T) {
	found := map[string]bool{}
	for _, root := range []string{"cmd", "internal", "e2e"} {
		err := filepath.WalkDir(filepath.Join("..", "..", root), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel := strings.TrimPrefix(filepath.ToSlash(path), "../../")
			switch {
			case strings.HasSuffix(path, ".go") && (strings.HasSuffix(path, "_test.go") || root == "e2e"):
				for _, key := range goSkips(t, path, rel) {
					found[key] = true
					if allowedSkips[key] == "" {
						t.Errorf("%s: a skip that isn't in allowedSkips", key)
					}
				}
			case strings.HasSuffix(path, ".txtar"):
				for _, line := range scriptSkips(t, path) {
					t.Errorf("%s:%d: a testscript skip: %s", rel, line.n, line.text)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for key := range allowedSkips {
		if !found[key] {
			t.Errorf("allowedSkips has %s, which no test has", key)
		}
	}
}

// goSkips are the Skip, Skipf and SkipNow calls in the Go file at path, as
// keys of allowedSkips.
func goSkips(t *testing.T, path, rel string) []string {
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Skip" && sel.Sel.Name != "Skipf" && sel.Sel.Name != "SkipNow" {
				return true
			}
			args := string(src[fset.Position(call.Lparen).Offset+1 : fset.Position(call.Rparen).Offset])
			args = strings.Join(strings.Fields(args), " ")
			args = strings.TrimSuffix(args, ",")
			keys = append(keys, rel+" "+fn.Name.Name+" ("+args+")")
			return true
		})
	}
	return keys
}

type scriptLine struct {
	n    int
	text string
}

// scriptSkips are the skip steps of the testscript at path.
func scriptSkips(t *testing.T, path string) []scriptLine {
	skip := regexp.MustCompile(`^\s*(\[[^\]]*\]\s*)*skip(\s|$)`)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []scriptLine
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; s.Scan(); n++ {
		if strings.HasPrefix(s.Text(), "-- ") {
			break
		}
		if skip.MatchString(s.Text()) {
			out = append(out, scriptLine{n, strings.TrimSpace(s.Text())})
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
