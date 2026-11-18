package codex

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
)

// ctxtra's MCP entry is a block of lines between two markers in Codex's
// config.toml, so that setup finds and replaces its own lines without
// rewriting the file. Setup owns only the keys in mcpKeys: a key the user
// adds to the table, inside the block or after it, and subtables such as
// [mcp_servers.ctxtra-proxy.tools.NAME], stay.
const (
	beginMarker = "# >>> ctxtra MCP proxy: written by ctxtra setup; keys you add are kept"
	endMarker   = "# <<< ctxtra MCP proxy"
	tableHeader = "[mcp_servers." + agent.MCPEntryName + "]"
)

// The block under the entry's old name, which setup renames.
const (
	legacyBeginMarker = "# >>> ctxtra MCP server: written by ctxtra setup; keys you add are kept"
	legacyEndMarker   = "# <<< ctxtra MCP server"
	legacyTableHeader = "[mcp_servers." + agent.LegacyMCPEntryName + "]"
)

// StartupTimeout is the entry's startup_timeout_sec: above the proxy's own
// deadline for its first tool list, so that Codex never gives up on it.
const StartupTimeout = 30

// mcpKeys are the keys setup owns, in the order it writes them. required:
// Codex waits for a required server's first tool list (up to
// startup_timeout_sec), for an optional one about 1 second.
var mcpKeys = []string{"command", "args", "env_vars", "startup_timeout_sec", "required"}

func (Agent) MCPPath() string { return filepath.Join(dir(), "config.toml") }

// EnvVars are the variables Codex passes to the proxy: Codex starts MCP
// servers with an allow-listed environment. They make the proxy read the
// hook's config and log, reach the keychain on Linux, work behind a proxy,
// and give local servers their env-pass variables.
func EnvVars(envPass []string) []string {
	// The Secret Service's variables matter only on Linux; listed
	// everywhere, so the entry is the same on every system.
	// Keep the legacy opt-in name in the serialized setup contract.
	vars := []string{"CTXTRA_CONFIG", "CTXTRA_LOG", "CTXTRA_EXPERIMENTAL_MCP", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"}
	for _, v := range []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "ALL_PROXY"} {
		vars = append(vars, v, strings.ToLower(v))
	}
	vars = append(vars, "SSL_CERT_FILE", "SSL_CERT_DIR")
	extra := slices.Clone(envPass)
	slices.Sort(extra)
	for _, v := range slices.Compact(extra) {
		if !slices.Contains(vars, v) {
			vars = append(vars, v)
		}
	}
	return vars
}

func (a Agent) wanted(bin string, envPass []string) map[string]any {
	return map[string]any{
		"command":             bin,
		"args":                toAny(agent.MCPArgs(a.Name())),
		"env_vars":            toAny(EnvVars(envPass)),
		"startup_timeout_sec": int64(StartupTimeout),
		"required":            true,
	}
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// tomlFile is config.toml as text, and as data.
type tomlFile struct {
	path       string
	raw        []byte // nil for a missing file
	lines      []string
	data       map[string]any
	begin, end int  // the markers' lines; -1 without them
	legacy     bool // the block has the old markers and name
	// staleBegin and staleEnd are the markers of an old block next to a new
	// one, which an older ctxtra adds when it runs setup; -1 without one.
	staleBegin, staleEnd int
}

var errHandEdited = errors.New("the ctxtra MCP proxy isn't the one ctxtra setup wrote")

func readTOML(path string) (*tomlFile, error) {
	b, err := fsutil.ReadBounded(path, agent.MaxConfig)
	if errors.Is(err, os.ErrNotExist) {
		b, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseTOML(path, b, string(b))
}

// parseTOML reads text, the content of the file whose bytes on disk are
// raw.
func parseTOML(path string, raw []byte, text string) (*tomlFile, error) {
	f := &tomlFile{path: path, raw: raw, data: map[string]any{}, begin: -1, end: -1, staleBegin: -1, staleEnd: -1}
	if _, err := toml.Decode(text, &f.data); err != nil {
		return nil, fmt.Errorf("not valid TOML: %v", err)
	}
	if len(text) > 0 {
		f.lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	blocks := map[bool][2][]int{} // legacy: the lines of its begin and end markers
	for i, l := range f.lines {
		for legacy, m := range map[bool][2]string{false: {beginMarker, endMarker}, true: {legacyBeginMarker, legacyEndMarker}} {
			b := blocks[legacy]
			switch strings.TrimSpace(l) {
			case m[0]:
				b[0] = append(b[0], i)
			case m[1]:
				b[1] = append(b[1], i)
			}
			blocks[legacy] = b
		}
	}
	found := map[bool]bool{}
	for legacy, b := range blocks {
		switch {
		case len(b[0]) == 0 && len(b[1]) == 0:
			continue
		case len(b[0]) != 1 || len(b[1]) != 1 || b[1][0] < b[0][0]:
			return f, errHandEdited
		}
		header := tableHeader
		if legacy {
			header = legacyTableHeader
		}
		if first := firstLine(f.lines, b[0][0], b[1][0]); first < 0 || strings.TrimSpace(f.lines[first]) != header {
			return f, errHandEdited
		}
		found[legacy] = true
	}
	switch {
	case found[false] && found[true]:
		f.begin, f.end = blocks[false][0][0], blocks[false][1][0]
		f.staleBegin, f.staleEnd = blocks[true][0][0], blocks[true][1][0]
		if f.staleBegin < f.end && f.begin < f.staleEnd {
			return f, errHandEdited
		}
		servers, _ := f.data["mcp_servers"].(map[string]any)
		if old, _ := servers[agent.LegacyMCPEntryName].(map[string]any); old == nil {
			return f, errHandEdited
		} else if _, ours := mcpBinary(old); !ours {
			return f, errHandEdited
		}
	case found[false], found[true]:
		f.legacy = found[true]
		f.begin, f.end = blocks[f.legacy][0][0], blocks[f.legacy][1][0]
	}
	// The new name is taken by a table setup didn't write: in place of the
	// block, or next to an old one that setup would rename to it.
	if servers, _ := f.data["mcp_servers"].(map[string]any); (f.begin < 0 || f.legacy) && servers[agent.MCPEntryName] != nil {
		return f, errHandEdited
	}
	// ctxtra's block, changed to run something else: the user's now.
	if _, ours := mcpBinary(f.entry()); f.begin >= 0 && !ours {
		return f, errHandEdited
	}
	return f, nil
}

// firstLine is the first line in the block that isn't blank or a comment.
func (f *tomlFile) firstLine() int { return firstLine(f.lines, f.begin, f.end) }

// firstLine is the first line between the markers on lines begin and end
// that isn't blank or a comment, -1 for none.
func firstLine(lines []string, begin, end int) int {
	for i := begin + 1; i < end; i++ {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "#") {
			return i
		}
	}
	return -1
}

// name is the entry's name in the file.
func (f *tomlFile) name() string {
	if f.legacy {
		return agent.LegacyMCPEntryName
	}
	return agent.MCPEntryName
}

// entry is the block's table as data, nil when there is none. Without a
// block, it is nil: a table of the old name is then the user's.
func (f *tomlFile) entry() map[string]any {
	if f.begin < 0 {
		return nil
	}
	servers, _ := f.data["mcp_servers"].(map[string]any)
	e, ok := servers[f.name()]
	if !ok {
		return nil
	}
	m, _ := e.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func (f *tomlFile) text() string {
	if len(f.lines) == 0 {
		return ""
	}
	return strings.Join(f.lines, "\n") + "\n"
}

// keyLine is one key's line, as the TOML encoder writes it.
func keyLine(k string, v any) string {
	var b bytes.Buffer
	_ = toml.NewEncoder(&b).Encode(map[string]any{k: v})
	return strings.TrimSuffix(b.String(), "\n")
}

// lineKey is the key a line in the block sets, "" for none.
func lineKey(l string) string {
	k, _, ok := strings.Cut(l, "=")
	if !ok || strings.HasPrefix(strings.TrimSpace(l), "#") {
		return ""
	}
	return strings.Trim(strings.TrimSpace(k), `"'`)
}

// SetupMCP adds, updates or removes the entry. Codex writes config.toml
// itself (trust records): when it did meanwhile, the edit is made again.
func (a Agent) SetupMCP(o agent.MCPOptions, r *agent.Report) error {
	if !o.Remove && !o.Want {
		return nil
	}
	for range 3 {
		err := a.setupOnce(o, r)
		if !errors.Is(err, fsutil.ErrChanged) {
			return err
		}
	}
	return fmt.Errorf("%s: %v; left alone, run setup again", a.MCPPath(), fsutil.ErrChanged)
}

func (a Agent) setupOnce(o agent.MCPOptions, r *agent.Report) error {
	path := a.MCPPath()
	f, err := readTOML(path)
	again := "ctxtra setup"
	if o.Remove {
		again += " --remove"
	}
	switch {
	case errors.Is(err, errHandEdited):
		return fmt.Errorf("%s: %v; left alone. Restore or delete it, then run %s --agent codex", path, err, again)
	case err != nil:
		return fmt.Errorf("%s: %v; left alone", path, err)
	case o.Remove && f.begin < 0:
		return nil
	}
	expect := deepCopy(f.data)
	if f.staleBegin >= 0 {
		f.removeBlock(f.staleBegin, f.staleEnd, agent.LegacyMCPEntryName)
		servers, _ := expect["mcp_servers"].(map[string]any)
		delete(servers, agent.LegacyMCPEntryName)
		raw := f.raw
		if f, err = parseTOML(path, raw, f.text()); err != nil {
			return fmt.Errorf("%s: %v; left alone. Restore or delete it, then run %s --agent codex", path, errHandEdited, again)
		}
	}
	if o.Remove {
		f.removeBlock(f.begin, f.end, f.name())
		servers, _ := expect["mcp_servers"].(map[string]any)
		delete(servers, f.name())
		if len(servers) == 0 {
			delete(expect, "mcp_servers")
		}
	} else {
		if f.legacy {
			f.rename()
			servers, _ := expect["mcp_servers"].(map[string]any)
			servers[agent.MCPEntryName] = servers[agent.LegacyMCPEntryName]
			delete(servers, agent.LegacyMCPEntryName)
		}
		want := a.wanted(o.Binary, o.EnvPass)
		if f.begin < 0 {
			block := []string{beginMarker, tableHeader}
			for _, k := range mcpKeys {
				block = append(block, keyLine(k, want[k]))
			}
			block = append(block, endMarker)
			if len(f.lines) > 0 && strings.TrimSpace(f.lines[len(f.lines)-1]) != "" {
				f.lines = append(f.lines, "")
			}
			f.lines = append(f.lines, block...)
		} else {
			f.updateBlock(want)
		}
		servers, _ := expect["mcp_servers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
			expect["mcp_servers"] = servers
		}
		e, _ := servers[agent.MCPEntryName].(map[string]any)
		if e == nil {
			e = map[string]any{}
		}
		maps.Copy(e, want)
		servers[agent.MCPEntryName] = e
	}
	out := f.text()
	// Codex refuses an invalid config, which would break every session:
	// the new file must say exactly what the old one said, but for ctxtra's
	// own keys.
	got := map[string]any{}
	if _, err := toml.Decode(out, &got); err != nil || !reflect.DeepEqual(normalize(got), normalize(expect)) {
		return fmt.Errorf("%s: %v; left alone. Restore or delete it, then run %s --agent codex", path, errHandEdited, again)
	}
	written, changed, err := fsutil.WriteIfUnchanged(path, f.raw, []byte(out), paths.BackupDir(), o.DryRun)
	if changed {
		r.Changes = append(r.Changes, written)
	}
	return err
}

// rename gives the old block the new markers and name, and the tables
// under it, such as [mcp_servers.ctxtra.tools.NAME], the new name too.
func (f *tomlFile) rename() {
	f.lines[f.begin], f.lines[f.end] = beginMarker, endMarker
	f.lines[f.firstLine()] = tableHeader
	re := tableRE(agent.LegacyMCPEntryName)
	for i, l := range f.lines {
		t := strings.TrimLeft(l, " \t")
		if re.MatchString(t) {
			f.lines[i] = l[:len(l)-len(t)] + re.ReplaceAllString(t, "${1}"+agent.MCPEntryName+"${3}")
		}
	}
	f.legacy = false
}

// updateBlock sets the owned keys in the block: their lines are replaced
// in place, missing ones go after the table header.
func (f *tomlFile) updateBlock(want map[string]any) {
	header := f.firstLine()
	seen := map[string]bool{}
	for i := header + 1; i < f.end; i++ {
		k := lineKey(f.lines[i])
		if v, ok := want[k]; ok && !seen[k] {
			f.lines[i] = keyLine(k, v)
			seen[k] = true
		}
	}
	var missing []string
	for _, k := range mcpKeys {
		if !seen[k] {
			missing = append(missing, keyLine(k, want[k]))
		}
	}
	f.lines = slices.Insert(f.lines, header+1, missing...)
	f.end += len(missing)
}

func deepCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			v = deepCopy(sub)
		}
		out[k] = v
	}
	return out
}

// normalize makes decoded data comparable: empty tables and missing ones
// are the same for this check.
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			if m, ok := normalize(val).(map[string]any); ok && len(m) == 0 {
				continue
			}
			out[k] = normalize(val)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalize(val)
		}
		return out
	}
	return v
}

// mcpBinary is the program of a well-formed entry.
func mcpBinary(e map[string]any) (bin string, ours bool) {
	bin, _ = e["command"].(string)
	args, _ := e["args"].([]any)
	return bin, len(args) >= 2 && args[0] == "mcp" && args[1] == "proxy"
}

func (a Agent) MCPStatus(bin string, o agent.MCPCheck) agent.Status {
	notices := a.CheckMCP(bin, o)
	f, err := readTOML(a.MCPPath())
	if err != nil || f.begin < 0 {
		return agent.Status{State: "no MCP entry", OK: len(notices) == 0, Details: notices}
	}
	b, _ := mcpBinary(f.entry())
	s := agent.Status{State: "MCP entry runs " + paths.Tilde(b), OK: len(notices) == 0, Details: notices}
	if _, ok := agent.CheckBinary(b); ok {
		s.State += " (ok)"
	}
	return s
}

func (a Agent) CheckMCP(bin string, o agent.MCPCheck) []string {
	path := a.MCPPath()
	repair := agent.Repair(a.Name())
	f, err := readTOML(path)
	switch {
	case !o.Want && !o.Known && (err != nil || f.begin < 0):
		return nil
	case errors.Is(err, errHandEdited):
		return []string{fmt.Sprintf("%s: %v; restore or delete it, then %s", paths.Tilde(path), err, repair)}
	case err != nil:
		return []string{agent.UnreadableNotice(path, err)}
	case f.begin < 0 && o.Known:
		return []string{agent.GoneNotice("the ctxtra MCP proxy is", path, a.Name(), repair)}
	case f.begin < 0:
		return []string{"the ctxtra MCP proxy is missing; " + repair}
	}
	b, ours := mcpBinary(f.entry())
	if !ours {
		return []string{fmt.Sprintf("%s: %v; restore or delete it, then %s", paths.Tilde(path), errHandEdited, repair)}
	}
	out := agent.BinaryNotices("the ctxtra MCP proxy runs", []string{b}, bin, repair)
	if t, _ := f.entry()["startup_timeout_sec"].(int64); t < StartupTimeout {
		out = append(out, "the ctxtra MCP proxy has a shorter startup_timeout_sec than setup writes; "+repair)
	}
	if missing := missingVars(f.entry(), o.EnvPass); o.Want && len(missing) > 0 {
		out = append(out, fmt.Sprintf("the ctxtra MCP proxy doesn't pass %s to local servers; %s", strings.Join(missing, ", "), repair))
	}
	if r, _ := f.entry()["required"].(bool); !r {
		out = append(out, "the ctxtra MCP proxy isn't required, so Codex doesn't wait for its tools; "+repair)
	}
	if f.legacy || f.staleBegin >= 0 {
		out = append(out, fmt.Sprintf("the ctxtra MCP proxy has its old name %s; %s", agent.LegacyMCPEntryName, repair))
	}
	return out
}

func (a Agent) MCP() agent.MCPHost { return a }

func (a Agent) MCPCommand() ([]string, bool) {
	f, err := readTOML(a.MCPPath())
	if err != nil || f.begin < 0 {
		return nil, false
	}
	e := f.entry()
	bin, ours := mcpBinary(e)
	if !ours {
		return nil, false
	}
	out := []string{bin}
	args, _ := e["args"].([]any)
	for _, x := range args {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out, true
}

// MCPDeadline: Codex waits about 1 second for an optional server's first
// tool list (measured with Codex 0.166.0); the entry is required, so Codex
// waits for it up to startup_timeout_sec.
func (Agent) MCPDeadline() time.Duration { return agent.MCPDeadline }

// tableRE matches a header of a table under mcp_servers.NAME, with each
// key bare or quoted: the second group is NAME.
func tableRE(name string) *regexp.Regexp {
	q := regexp.QuoteMeta(name)
	return regexp.MustCompile(`^(\[\[?\s*(?:"mcp_servers"|'mcp_servers'|mcp_servers)\s*\.\s*)("` + q + `"|'` + q + `'|` + q + `)(\s*[.\]])`)
}

// removeBlock removes the block between the markers on lines begin and
// end, of the table mcp_servers.NAME, with what else belongs to that table,
// and a blank line it leaves doubled.
func (f *tomlFile) removeBlock(begin, end int, name string) {
	f.lines = slices.Delete(f.lines, begin, end+1)
	f.removeRest(begin, name)
	if begin > 0 && begin < len(f.lines)+1 && strings.TrimSpace(f.lines[begin-1]) == "" &&
		(begin == len(f.lines) || strings.TrimSpace(f.lines[begin]) == "") {
		f.lines = slices.Delete(f.lines, begin-1, begin)
	}
}

// removeRest removes what still belongs to the table mcp_servers.NAME once
// its block, which started at line from, is gone: keys the user added after
// the block, up to the next table, and every table under it, wherever it
// is. Comments and blank lines stay. The check of the result catches what
// this misses.
func (f *tomlFile) removeRest(from int, name string) {
	reOurTable := tableRE(name)
	ours := false
	var out []string
	for i, l := range f.lines {
		if i == from {
			ours = true // the lines right after the block are in ctxtra's table
		}
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			ours = reOurTable.MatchString(t)
			if ours {
				continue
			}
		}
		if ours && t != "" && !strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, l)
	}
	f.lines = out
}

// missingVars are the names of envPass that the entry's env_vars lacks.
func missingVars(e map[string]any, envPass []string) []string {
	have, _ := e["env_vars"].([]any)
	var out []string
	for _, v := range envPass {
		if !slices.Contains(have, any(v)) && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}
