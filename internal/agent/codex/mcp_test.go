package codex

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

// realConfig is the shape of a Codex config.toml: comments, settings,
// other MCP servers, trust records and hook state.
const realConfig = `# my settings
model = "gpt-6.1-sol"
approval_policy = "on-request"

[mcp_servers.linear]
url = "https://mcp.linear.app/mcp"  # remote

[mcp_servers.pg]
command = "npx"
args = ["-y", "server-postgres"]
env = { PGUSER = "dev" }

[projects."/Users/me/dev/api"]
trust_level = "trusted"

[hooks.state."/Users/me/.codex/hooks.json:session_start:0:0"]
trusted_hash = "sha256:abc"
`

func setupMCP(t *testing.T, o agent.MCPOptions) (agent.Report, error) {
	t.Helper()
	var r agent.Report
	err := Agent{}.SetupMCP(o, &r)
	return r, err
}

func add(bin string, envPass ...string) agent.MCPOptions {
	return agent.MCPOptions{Options: agent.Options{Binary: bin}, Want: true, EnvPass: envPass}
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	m := map[string]any{}
	if _, err := toml.Decode(s, &m); err != nil {
		t.Fatalf("%v\n%s", err, s)
	}
	return m
}

func ctxtraEntry(t *testing.T, path string) map[string]any {
	t.Helper()
	b, _ := os.ReadFile(path)
	m := decode(t, string(b))
	servers, _ := m["mcp_servers"].(map[string]any)
	e, _ := servers["ctxtra-proxy"].(map[string]any)
	return e
}

func TestCodexAddKeepsTheRest(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	testenv.Write(t, path, realConfig)
	r, err := setupMCP(t, add("/bin/ctxtra", "PGHOST", "HTTPS_PROXY"))
	if err != nil || len(r.Changes) != 1 {
		t.Fatalf("%v %v", r.Changes, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), realConfig) {
		t.Errorf("the rest changed:\n%s", b)
	}
	tail := strings.TrimPrefix(string(b), realConfig)
	if !strings.HasPrefix(tail, "\n"+beginMarker+"\n[mcp_servers.ctxtra-proxy]\ncommand = \"/bin/ctxtra\"\nargs = [\"mcp\", \"proxy\", \"codex\", \"--setup-version=1\"]\nenv_vars = [\"XDG_CONFIG_HOME\", \"XDG_DATA_HOME\", \"XDG_STATE_HOME\", \"XDG_CACHE_HOME\", \"CTXTRA_LOG\"") ||
		!strings.HasSuffix(tail, "\"SSL_CERT_DIR\", \"PGHOST\"]\nstartup_timeout_sec = 30\nrequired = true\n"+endMarker+"\n") {
		t.Errorf("block:\n%s", tail)
	}
	e := ctxtraEntry(t, path)
	vars, _ := e["env_vars"].([]any)
	if e["command"] != "/bin/ctxtra" || len(vars) == 0 || strings.Count(tail, "HTTPS_PROXY") != 1 {
		t.Errorf("entry %v", e)
	}
	if r, _ := setupMCP(t, add("/bin/ctxtra", "PGHOST", "HTTPS_PROXY")); len(r.Changes) != 0 {
		t.Errorf("not idempotent")
	}
	if n := (Agent{}).CheckMCP("", agent.MCPCheck{Want: true, Known: true}); len(n) != 1 || !strings.Contains(n[0], "/bin/ctxtra, which isn't an executable file") && !strings.Contains(n[0], "doesn't exist") {
		t.Errorf("notices %v", n)
	}
}

func TestCodexTheVariableReachesTheMCPProxy(t *testing.T) {
	if !slices.Contains(EnvVars(nil), profile.Env) {
		t.Error("the default allow-list omits the profile variable")
	}
	vars := EnvVars([]string{profile.Env, "PGHOST", profile.Env})
	count := 0
	for _, v := range vars {
		if v == profile.Env {
			count++
		}
	}
	if count != 1 {
		t.Errorf("profile variable missing or repeated: %v", vars)
	}
}

func TestCodexReportsAMissingProfileVariable(t *testing.T) {
	h := testenv.Home(t)
	bin := filepath.Join(h, "ctxtra")
	testenv.Write(t, bin, "#!/bin/sh\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := setupMCP(t, add(bin)); err != nil {
		t.Fatal(err)
	}
	path := Agent{}.MCPPath()
	b, _ := os.ReadFile(path)
	testenv.Write(t, path, strings.Replace(string(b), `"CTXTRA_PROFILES", `, "", 1))
	n := (Agent{}).CheckMCP(bin, agent.MCPCheck{Want: true, Known: true})
	if len(n) != 1 || n[0] != "the ctxtra MCP entry doesn't pass CTXTRA_PROFILES to the proxy; run ctxtra setup --agent codex" {
		t.Errorf("notices: %v", n)
	}
}

// A key the user adds to ctxtra's table, inside the block or after it,
// and a tools subtable survive setup and its updates.
func TestCodexUserKeysSurvive(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	testenv.Write(t, path, realConfig)
	if _, err := setupMCP(t, add("/old/ctxtra")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	s := strings.Replace(string(b), "startup_timeout_sec = 30\n", "startup_timeout_sec = 30\ndefault_tools_approval_mode = \"approve\"\n", 1)
	s += "enabled = true\n\n[mcp_servers.ctxtra-proxy.tools.gh__create_issue_ef677c]\napproval_mode = \"approve\"\n"
	testenv.Write(t, path, s)
	if _, err := setupMCP(t, add("/new/ctxtra", "PGHOST")); err != nil {
		t.Fatal(err)
	}
	e := ctxtraEntry(t, path)
	tools, _ := e["tools"].(map[string]any)
	if e["command"] != "/new/ctxtra" || e["default_tools_approval_mode"] != "approve" || e["enabled"] != true || tools["gh__create_issue_ef677c"] == nil {
		t.Errorf("entry %v", e)
	}
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "\"PGHOST\"]") {
		t.Errorf("env_vars not updated:\n%s", b)
	}
	// --remove removes the user's keys in ctxtra's table and its
	// subtables too, and nothing else.
	if _, err := setupMCP(t, agent.MCPOptions{Options: agent.Options{Remove: true}}); err != nil {
		t.Fatal(err)
	}
	if e := ctxtraEntry(t, path); e != nil {
		t.Errorf("left %v", e)
	}
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), realConfig) {
		t.Errorf("the rest changed:\n%s", b)
	}
}

func TestCodexRemove(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	testenv.Write(t, path, realConfig)
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	r, err := setupMCP(t, agent.MCPOptions{Options: agent.Options{Remove: true}})
	if err != nil || len(r.Changes) != 1 {
		t.Fatalf("%v %v", r.Changes, err)
	}
	if b, _ := os.ReadFile(path); string(b) != realConfig {
		t.Errorf("not as before:\n%q", b)
	}
	if r, err := setupMCP(t, agent.MCPOptions{Options: agent.Options{Remove: true}}); err != nil || len(r.Changes) != 0 {
		t.Errorf("nothing to remove: %v %v", r.Changes, err)
	}
	// A file that had only the entry is empty again.
	testenv.Write(t, path, "")
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	if _, err := setupMCP(t, agent.MCPOptions{Options: agent.Options{Remove: true}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); len(b) != 0 {
		t.Errorf("%q", b)
	}
}

func TestCodexNewFileAndDryRun(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	o := add("/bin/ctxtra")
	o.DryRun = true
	if r, err := setupMCP(t, o); err != nil || len(r.Changes) != 1 {
		t.Fatalf("%v %v", r.Changes, err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("dry run wrote")
	}
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), beginMarker+"\n") {
		t.Errorf("%s", b)
	}
	if r, _ := setupMCP(t, agent.MCPOptions{Options: agent.Options{Binary: "/bin/ctxtra"}}); len(r.Changes) != 0 {
		t.Errorf("without servers, the entry stays")
	}
}

// Every other form of an entry named ctxtra-proxy, and broken markers: setup
// and --remove change nothing and say the file was changed by hand.
func TestCodexHandEdited(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	block := beginMarker + "\n[mcp_servers.ctxtra-proxy]\ncommand = \"/bin/ctxtra\"\nargs = [\"mcp\", \"proxy\", \"codex\", \"--setup-version=1\"]\n" + endMarker + "\n"
	for name, body := range map[string]string{
		"quoted table":     realConfig + "[mcp_servers.\"ctxtra-proxy\"]\ncommand = \"x\"\n",
		"dotted keys":      "mcp_servers.ctxtra-proxy.command = \"x\"\n" + realConfig,
		"inline map":       "mcp_servers = { ctxtra-proxy = { command = \"x\" } }\n",
		"no markers":       realConfig + "[mcp_servers.ctxtra-proxy]\ncommand = \"/bin/ctxtra\"\nargs = [\"mcp\", \"proxy\", \"codex\", \"--setup-version=1\"]\n",
		"begin only":       realConfig + beginMarker + "\n[mcp_servers.x]\ncommand = \"x\"\n",
		"end first":        endMarker + "\n" + realConfig + beginMarker + "\n",
		"two blocks":       realConfig + block + block,
		"other header":     realConfig + strings.Replace(block, "[mcp_servers.ctxtra-proxy]", "[mcp_servers.other]", 1),
		"invalid TOML":     realConfig + "[[[\n",
		"not the proxy":    realConfig + strings.Replace(block, `"mcp", "proxy"`, `"serve"`, 1),
		"redefined inside": realConfig + strings.Replace(block, endMarker, "[mcp_servers.linear]\nx = 1\n"+endMarker, 1),
	} {
		testenv.Write(t, path, body)
		for _, o := range []agent.MCPOptions{add("/bin/ctxtra"), {Options: agent.Options{Remove: true}}} {
			_, err := setupMCP(t, o)
			if err == nil || !strings.Contains(err.Error(), "left alone") {
				t.Errorf("%s (remove %v): %v", name, o.Remove, err)
			}
			if b, _ := os.ReadFile(path); string(b) != body {
				t.Errorf("%s: changed", name)
			}
		}
		if n := (Agent{}).CheckMCP("", agent.MCPCheck{Want: true}); len(n) != 1 {
			t.Errorf("%s: notices %v", name, n)
		}
	}
}

func TestCodexCheck(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	testenv.Write(t, path, realConfig)
	a := Agent{}
	if n := a.CheckMCP("", agent.MCPCheck{}); len(n) != 0 {
		t.Errorf("%v", n)
	}
	if n := a.CheckMCP("", agent.MCPCheck{Want: true}); len(n) != 1 || n[0] != "the ctxtra MCP proxy is missing; run ctxtra setup --agent codex" {
		t.Errorf("%v", n)
	}
	if n := a.CheckMCP("", agent.MCPCheck{Known: true}); len(n) != 1 || !strings.HasPrefix(n[0], "the ctxtra MCP proxy is gone from ~/.codex/config.toml") {
		t.Errorf("%v", n)
	}
	bin := filepath.Join(h, "ctxtra")
	testenv.Write(t, bin, "#!/bin/sh\n")
	_ = os.Chmod(bin, 0o755)
	if _, err := setupMCP(t, add(bin)); err != nil {
		t.Fatal(err)
	}
	if n := a.CheckMCP(bin, agent.MCPCheck{Want: true, Known: true}); len(n) != 0 {
		t.Errorf("%v", n)
	}
	b, _ := os.ReadFile(path)
	testenv.Write(t, path, strings.Replace(string(b), "startup_timeout_sec = 30", "startup_timeout_sec = 10", 1))
	if n := a.CheckMCP(bin, agent.MCPCheck{Want: true}); len(n) != 1 || !strings.Contains(n[0], "shorter startup_timeout_sec") {
		t.Errorf("%v", n)
	}
	b, _ = os.ReadFile(path)
	testenv.Write(t, path, strings.Replace(strings.Replace(string(b), "startup_timeout_sec = 10", "startup_timeout_sec = 30", 1), "required = true", "required = false", 1))
	if n := a.CheckMCP(bin, agent.MCPCheck{Want: true}); len(n) != 1 || !strings.Contains(n[0], "isn't required") {
		t.Errorf("%v", n)
	}
	if st := a.MCPStatus(bin, agent.MCPCheck{Want: true}); !strings.HasPrefix(st.State, "MCP entry runs ~/ctxtra (ok)") || st.OK {
		t.Errorf("%+v", st)
	}
}

// An empty [mcp_servers] table of the user's stays, and --remove works.
func TestCodexEmptyParentTable(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	body := "model = \"x\"\n\n[mcp_servers]\n"
	testenv.Write(t, path, body)
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	if _, err := setupMCP(t, agent.MCPOptions{Options: agent.Options{Remove: true}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != body {
		t.Errorf("%q", b)
	}
}

// The agent writes the file between setup's read and its write: setup
// edits the new content, and keeps the agent's change.
func TestCodexConcurrentChange(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	testenv.Write(t, path, realConfig)
	f, err := readTOML(path)
	if err != nil {
		t.Fatal(err)
	}
	testenv.Write(t, path, realConfig+"[projects.\"/new\"]\ntrust_level = \"trusted\"\n")
	if _, _, err := fsutil.WriteIfUnchanged(path, f.raw, []byte("x"), t.TempDir(), false); err != fsutil.ErrChanged {
		t.Fatalf("%v", err)
	}
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "/new") || !strings.Contains(string(b), beginMarker) {
		t.Errorf("%s", b)
	}
}

// ctxtra's subtables before the block, and with quoted keys, go with
// --remove too.
func TestCodexRemoveSubtables(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	testenv.Write(t, path, realConfig)
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	body := "[mcp_servers.ctxtra-proxy.tools.a]\napproval_mode = \"approve\"\n\n" + string(b) +
		"\n[\"mcp_servers\".ctxtra-proxy.tools.b]\napproval_mode = \"approve\"\n\n[mcp_servers.other]\ncommand = \"x\"\n"
	testenv.Write(t, path, body)
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatalf("setup with subtables: %v", err)
	}
	if _, err := setupMCP(t, agent.MCPOptions{Options: agent.Options{Remove: true}}); err != nil {
		t.Fatal(err)
	}
	m := decode(t, read(t, path))
	servers, _ := m["mcp_servers"].(map[string]any)
	if servers["ctxtra-proxy"] != nil || servers["other"] == nil || servers["linear"] == nil {
		t.Errorf("%v", servers)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// legacyBlock is the entry as setup wrote it before the rename.
const legacyBlock = legacyBeginMarker + "\n" + legacyTableHeader + "\ncommand = \"/old/ctxtra\"\nargs = [\"mcp\", \"proxy\", \"codex\", \"--setup-version=1\"]\n" +
	"startup_timeout_sec = 30\ndefault_tools_approval_mode = \"approve\"\nrequired = true\n" + legacyEndMarker + "\nenabled = true\n"

// Setup renames the old block, with the keys and subtables the user added,
// and changes nothing else.
func TestCodexRenamesLegacy(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	body := "[mcp_servers.ctxtra.tools.a]\napproval_mode = \"approve\"\n\n" + realConfig + "\n" + legacyBlock +
		"\n[ \"mcp_servers\" . 'ctxtra' .tools.b]\napproval_mode = \"approve\"\n"
	testenv.Write(t, path, body)
	a := Agent{}
	if n := a.CheckMCP("", agent.MCPCheck{Want: true, Known: true}); !slices.ContainsFunc(n, func(s string) bool {
		return strings.HasPrefix(s, "the ctxtra MCP proxy has its old name ctxtra;")
	}) {
		t.Errorf("notices %v", n)
	}
	if argv, ok := a.MCPCommand(); !ok || argv[0] != "/old/ctxtra" {
		t.Errorf("command %v %v", argv, ok)
	}
	if _, err := setupMCP(t, add("/new/ctxtra")); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	servers, _ := decode(t, got)["mcp_servers"].(map[string]any)
	e, _ := servers["ctxtra-proxy"].(map[string]any)
	tools, _ := e["tools"].(map[string]any)
	if servers["ctxtra"] != nil || e["command"] != "/new/ctxtra" || e["default_tools_approval_mode"] != "approve" ||
		e["enabled"] != true || tools["a"] == nil || tools["b"] == nil {
		t.Errorf("servers %v", servers)
	}
	if strings.Contains(got, legacyBeginMarker) || strings.Contains(got, legacyEndMarker) ||
		!strings.Contains(got, "\n"+beginMarker+"\n"+tableHeader+"\n") || !strings.Contains(got, "[ \"mcp_servers\" . ctxtra-proxy .tools.b]") {
		t.Errorf("file:\n%s", got)
	}
	if n := a.CheckMCP("/new/ctxtra", agent.MCPCheck{Want: true, Known: true}); slices.ContainsFunc(n, func(s string) bool { return strings.Contains(s, "old name") }) {
		t.Errorf("notices %v", n)
	}
}

// --remove removes the old block and its subtables; a table named ctxtra
// that setup didn't write is the user's and stays.
func TestCodexLegacyRemoveAndUserTable(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	testenv.Write(t, path, realConfig+"\n"+legacyBlock+"\n[mcp_servers.ctxtra.tools.a]\napproval_mode = \"approve\"\n")
	if _, err := setupMCP(t, agent.MCPOptions{Options: agent.Options{Remove: true}}); err != nil {
		t.Fatal(err)
	}
	// Blank lines stay.
	if got := read(t, path); strings.TrimRight(got, "\n") != strings.TrimRight(realConfig, "\n") {
		t.Errorf("not as before:\n%q", got)
	}
	user := realConfig + "\n[mcp_servers.ctxtra]\ncommand = \"ctxtra-docs\"\n"
	testenv.Write(t, path, user)
	if n := (Agent{}).CheckMCP("", agent.MCPCheck{}); len(n) != 0 {
		t.Errorf("notices %v", n)
	}
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	servers, _ := decode(t, read(t, path))["mcp_servers"].(map[string]any)
	if old, _ := servers["ctxtra"].(map[string]any); old["command"] != "ctxtra-docs" || servers["ctxtra-proxy"] == nil {
		t.Errorf("servers %v", servers)
	}
	if !strings.HasPrefix(read(t, path), user) {
		t.Errorf("the user's table changed")
	}
}

// An old block next to the new name, or the old and new markers mixed:
// setup changes nothing.
func TestCodexLegacyHandEdited(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	for name, body := range map[string]string{
		"new name taken": realConfig + legacyBlock + "[mcp_servers.ctxtra-proxy]\ncommand = \"x\"\n",
		"mixed markers":  realConfig + strings.Replace(legacyBlock, legacyEndMarker, endMarker, 1),
		"new header":     realConfig + strings.Replace(legacyBlock, legacyTableHeader, tableHeader, 1),
	} {
		testenv.Write(t, path, body)
		for _, o := range []agent.MCPOptions{add("/bin/ctxtra"), {Options: agent.Options{Remove: true}}} {
			if _, err := setupMCP(t, o); err == nil || !strings.Contains(err.Error(), "left alone") {
				t.Errorf("%s (remove %v): %v", name, o.Remove, err)
			}
			if read(t, path) != body {
				t.Errorf("%s: changed", name)
			}
		}
	}
}

// An older ctxtra ran setup after this one: it added its block next to the
// new one. Setup removes the old block, with its tables, and keeps the new
// one; --remove removes both.
func TestCodexLegacyNextToNew(t *testing.T) {
	h := testenv.Home(t)
	path := filepath.Join(h, ".codex", "config.toml")
	testenv.Write(t, path, realConfig)
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	withNew := strings.Replace(string(b), "required = true\n", "required = true\nenabled = true\n", 1)
	both := withNew + "\n" + legacyBlock + "\n[mcp_servers.ctxtra.tools.a]\napproval_mode = \"approve\"\n"
	testenv.Write(t, path, both)
	a := Agent{}
	if n := a.CheckMCP("", agent.MCPCheck{Want: true, Known: true}); !slices.ContainsFunc(n, func(s string) bool { return strings.Contains(s, "old name ctxtra") }) {
		t.Errorf("notices %v", n)
	}
	if argv, ok := a.MCPCommand(); !ok || argv[0] != "/bin/ctxtra" {
		t.Errorf("command %v %v", argv, ok)
	}
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); strings.TrimRight(got, "\n") != strings.TrimRight(withNew, "\n") {
		t.Errorf("got\n%s", got)
	}
	testenv.Write(t, path, both)
	if _, err := setupMCP(t, agent.MCPOptions{Options: agent.Options{Remove: true}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); strings.Contains(got, "ctxtra") {
		t.Errorf("got\n%s", got)
	}
	// The old block first, and one that doesn't run the proxy.
	testenv.Write(t, path, legacyBlock+"\n"+withNew)
	if _, err := setupMCP(t, add("/bin/ctxtra")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); strings.Contains(got, "ctxtra MCP server") || !strings.Contains(got, "enabled = true") {
		t.Errorf("got\n%s", got)
	}
	notOurs := withNew + "\n" + strings.Replace(legacyBlock, `"mcp", "proxy"`, `"serve"`, 1)
	testenv.Write(t, path, notOurs)
	if _, err := setupMCP(t, add("/bin/ctxtra")); err == nil || read(t, path) != notOurs {
		t.Errorf("changed: %v", err)
	}
}
