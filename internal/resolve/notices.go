package resolve

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/update"
)

// sameNames adds a notice for each skill and server whose name an earlier
// one has. Same names are not mistakes: both skills reach the session, as
// the agent would get them by hand, and of two servers the first in
// binding order is served.
func (r *Result) sameNames() {
	for _, s := range r.SameSkills {
		r.Notices = append(r.Notices, fmt.Sprintf("two skills are named %s: %s (binding %s) and %s (binding %s); the session gets both",
			s.Name, skillRef(s.First), s.First.Origin, skillRef(s.Other), s.Other.Origin))
	}
	for _, c := range r.MCPConflicts {
		r.Notices = append(r.Notices, c.Notice())
	}
}

// Notice says which of the two servers the session gets, and how to get
// both: a new name for a source's server in the config's mcp-servers.
func (c MCPConflict) Notice() string {
	n := fmt.Sprintf("MCP servers %s and %s are both named %s; the session gets %s, the first in binding order", c.Winner, c.Loser, c.Prefix, c.Winner)
	rename := c.Loser
	if !strings.Contains(rename, ":") {
		rename = c.Winner
	}
	source, server, ok := strings.Cut(rename, ":")
	switch {
	case !ok:
	case server != c.Prefix:
		n += fmt.Sprintf("; to get both, change the config's name %s for %s in mcp-servers", c.Prefix, rename)
	default:
		name := config.SuggestServerName(server + "_" + source)
		if config.CheckServerName(name) != nil {
			name = server + "_2"
		}
		n += fmt.Sprintf("; to get both, give %s a new name in the config's mcp-servers: %s: {use: '%s'}", rename, name, rename)
	}
	return n
}

func skillRef(h Hit) string {
	if h.Source != "" {
		return h.Source + ":" + filepath.ToSlash(filepath.Dir(h.SourcePath))
	}
	return paths.Tilde(filepath.Dir(h.Path))
}

// RemoteOf is the address that ctxtra fetches a repo source from: a local
// path with ~ expanded.
func RemoteOf(s *config.Source) string {
	if config.IsLocalRepo(s.Repo) {
		return filepath.Clean(paths.Expand(s.Repo))
	}
	return s.Repo
}

// updateNotices tells about the repo sources that the session uses: new
// commits or a moved tag that the last check found, for a source with
// update checks on, and a commit the user didn't accept.
func (r *Result) updateNotices(env *Env) {
	var st *state.State
	for _, src := range env.Catalog.Sources() {
		if src.State != catalog.Active || src.Live() || src.Ref.Kind == config.Rev || !r.usesSource(src.Name) {
			continue
		}
		e := env.Lock.Entry(src.Name)
		if e == nil {
			continue
		}
		if st == nil {
			s := state.Load()
			st = &s
		}
		info := st.Get(update.StateKey(src.Source))
		checks := src.CheckUpdates == nil || *src.CheckUpdates
		switch {
		case !checks || !info.New(e.Commit):
		case src.Ref.Kind == config.Tag:
			r.Notices = append(r.Notices, fmt.Sprintf("%s: the tag %s moved to another commit\nrun `ctxtra source update %s` to review it.", src.Name, src.Ref.Name, src.Name))
		default:
			r.Notices = append(r.Notices, fmt.Sprintf("%s has new commits on %s\nrun `ctxtra source update %s` to review them.", src.Name, src.Ref.Name, src.Name))
		}
		if info.Dismissed != "" && info.Dismissed != e.Commit {
			r.Notices = append(r.Notices, fmt.Sprintf("%s has a newer commit that you didn't accept\nrun `ctxtra source update %s` to review it again.", src.Name, src.Name))
		}
	}
}

// usesSource reports whether the session gets a file, skill or server of
// source name.
func (r *Result) usesSource(name string) bool {
	for _, list := range [][]Hit{r.Files, r.Skills, r.Dropped} {
		for _, h := range list {
			if h.Source == name {
				return true
			}
		}
	}
	for _, s := range r.MCPServers {
		if s.Source == name {
			return true
		}
	}
	return false
}
