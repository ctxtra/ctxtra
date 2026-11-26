package hook

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"

	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/logfile"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
)

// Entry is one hook run in the log.
type Entry struct {
	Ts        string   `json:"ts"`
	Harness   string   `json:"harness"`
	Event     string   `json:"event"`
	Dir       string   `json:"dir,omitempty"`
	Repo      string   `json:"repo,omitempty"`
	Files     []string `json:"files,omitempty"`
	Skills    []string `json:"skills,omitempty"`
	Dropped   []string `json:"dropped,omitempty"`
	Errors    []string `json:"errors,omitempty"`
	Chars     int      `json:"chars"`
	Source    string   `json:"source,omitempty"`
	AgentType string   `json:"agent_type,omitempty"`
	Error     string   `json:"error,omitempty"`
	Skipped   string   `json:"skipped,omitempty"` // why the hook added nothing, see agent.Skipper
	Reused    string   `json:"reused,omitempty"`  // the session start got an earlier one's text, see slot.go
	// Profiles are the active profiles, ProfilesFrom where they come from,
	// and ProfilesKept is set when they come from the session's record.
	Profiles     []string `json:"profiles,omitempty"`
	ProfilesFrom string   `json:"profiles_from,omitempty"`
	ProfilesKept bool     `json:"profiles_kept,omitempty"`
	// Kind is set by other writers of the log, such as "mcp" for the MCP
	// proxy; hook runs have none.
	Kind string `json:"kind,omitempty"`
}

func newEntry(ts, harness, event string, p payload, res *resolve.Result, chars int) Entry {
	e := Entry{Ts: ts, Harness: harness, Event: event, Dir: res.Dir, Chars: chars, Source: p.Source, AgentType: p.AgentType,
		Profiles: res.Profiles.Names, ProfilesFrom: string(res.Profiles.Origin), ProfilesKept: res.Profiles.Kept}
	for _, err := range res.Errors {
		e.Errors = append(e.Errors, err.Error())
	}
	if res.Repo != nil {
		e.Repo = res.Repo.Root
	}
	for _, h := range res.Files {
		e.Files = append(e.Files, h.Path)
	}
	for _, h := range res.Skills {
		e.Skills = append(e.Skills, h.Path)
	}
	for _, h := range res.Dropped {
		e.Dropped = append(e.Dropped, h.Path)
	}
	return e
}

// logEntry appends e to the log.
func logEntry(e Entry) { logfile.Append(e) }

// LastRuns returns the last logged run per agent and event for sessions in
// dir (or in repo, when repo is set), sorted by agent and event. It reads the
// rotated log first, so a run in the current log wins.
func LastRuns(dir, repo string) []Entry {
	last := map[string]Entry{}
	path := paths.LogPath()
	for _, p := range []string{path + ".1", path} {
		scanLog(p, func(e Entry) {
			if e.Kind != "" {
				return
			}
			// A session finds its repository only when a binding needs
			// it, so a run in the same folder counts too.
			if (repo != "" && e.Repo == repo) || e.Dir == dir {
				last[e.Harness+" "+e.Event] = e
			}
		})
	}
	keys := make([]string, 0, len(last))
	for k := range last {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Entry, len(keys))
	for i, k := range keys {
		out[i] = last[k]
	}
	return out
}

// scanLog calls fn for each entry in the log at path. It skips a missing or
// unreadable file and malformed lines.
func scanLog(path string, fn func(Entry)) {
	f, err := fsutil.OpenRegular(path, os.O_RDONLY, 0)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			fn(e)
		}
	}
}
