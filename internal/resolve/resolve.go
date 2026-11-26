// Package resolve works out what a session gets: which bindings match the
// session folder, and the files and skills they add.
package resolve

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ctxtra/ctxtra/internal/caserule"
	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/gitrepo"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Hit is a file or a skill added to the session.
type Hit struct {
	Path    string // the file, or the skill's SKILL.md
	Ref     string // a file of a source: SOURCE:PATH
	Content string // a file's text; a skill's SKILL.md text after the frontmatter
	Origin  string // the binding that added it
	// Source is the source the file or SKILL.md is in, and SourcePath its
	// slash-separated path in it; both "" outside sources.
	Source, SourcePath string
	// Global is true for a file of a binding without conditions: every
	// session gets it.
	Global bool
	// Skills only.
	Name, Description string
	// Dropped files only: the limit the text was over, for example
	// "max-ctx-chars".
	Over string
}

// MCPServer is an MCP server the session gets.
type MCPServer struct {
	upstream.Server
	Origin string // the binding that added it
	// Unapproved is why a source's server isn't approved on this machine,
	// "" when it is or needs no approval. The session keeps its name, so
	// no other server is served in its place, and the proxy doesn't start
	// it.
	Unapproved string
}

// MCPConflict records why one selected identity was suppressed.
type MCPConflict struct {
	Winner, Loser, Prefix, Origin string
}

// Trace explains one binding, for `explain`.
type Trace struct {
	Origin  string
	Matched bool
	Reason  string
	// Files and Skills are the real paths of the files and SKILL.md files
	// the binding added, as in the session's hits.
	Files  []string
	Skills []string
	// MCPServers are the IDs of the binding's servers.
	MCPServers []string
	// Conditions are the binding's conditions, each with whether it held.
	Conditions []Condition
	// Excluded are what the binding's ! content entries removed.
	Excluded []Exclusion
	// Unavailable are the sources the binding names whose content this
	// machine can't use: what they would add is left out.
	Unavailable []string
	// Errors are the errors of the binding's entries in this session.
	Errors []*catalog.Error
}

// UnreadableSet is a repo-sets entry of a binding whose set can't be read.
type UnreadableSet struct {
	Binding string
	// Entry is the entry as written, with its !.
	Entry string
	// Why is why the set can't be read.
	Why string
}

// SessionMessage is how many instructions and skills the session message
// lists; 0 leaves a list out.
type SessionMessage struct {
	MaxInstructions, MaxSkills int
}

// DefaultSessionMessage is the session message without settings.
var DefaultSessionMessage = SessionMessage{MaxInstructions: 30, MaxSkills: 30}

// Result is what a session in Dir gets.
type Result struct {
	Dir    string
	Agent  string
	Repo   *gitrepo.Repo // nil outside git
	Files  []Hit
	Skills []Hit
	// MCPServers are the bound MCP servers, in binding order, each once.
	MCPServers []MCPServer
	// MCPConflicts are suppressed identities sharing an earlier tool prefix.
	MCPConflicts []MCPConflict
	Dropped      []Hit // over max-ctx-chars or the agent's capacity, see Hit.Over
	// Known are files that apply here too, which the session already has
	// from another folder: listed after the files, without their text.
	Known []Hit
	// Notices are for the user, not the model: new commits of the sources
	// the session uses, same names. A notice's command to run, if any, is on
	// its second line.
	Notices  []string
	Trace    []Trace
	MaxChars int
	Summary  SessionMessage
	// Errors are every error the session reports: in the config, the lock,
	// the sources and their manifests, and in what the bindings add.
	Errors []*catalog.Error
	// SameSkills are skills that have a name another skill of the session
	// has. All of them are delivered, as the agent would get them by hand.
	SameSkills []SameSkill
	// UnreadableSets are the repo-sets entries whose sets can't be read,
	// for the session message, which names them. Their errors are in
	// Errors.
	UnreadableSets []UnreadableSet
	// CaseRule is how the session folder's volume compares names, when a
	// binding compares folders.
	CaseRule caserule.Rule
	// RepoErr is why git couldn't tell the session's repository, when a
	// binding needed it.
	RepoErr error
	// Profiles are the active profiles; their errors are in Errors.
	Profiles profile.Active
}

// Chars is the length of s in characters (Unicode code points), the unit of
// max-ctx-chars.
func Chars(s string) int { return utf8.RuneCountInString(s) }

// Fit drops whole files from the end until fits accepts the result, and
// marks them as over the limit over. Files dropped before stay after them:
// they came later. The skill list is never dropped, so the result may still
// not fit. fits must accept every result with fewer files than one it
// accepts.
func (r *Result) Fit(fits func(*Result) bool, over string) {
	if fits(r) {
		return
	}
	files := r.Files
	c := *r
	// The most files that fit; at most len(files)-1 here.
	n := sort.Search(len(files), func(n int) bool {
		c.Files = files[:n+1]
		return !fits(&c)
	})
	var dropped []Hit
	var bindings []string
	for _, h := range files[n:] {
		h.Over = over
		dropped = append(dropped, h)
		if !slices.Contains(bindings, h.Origin) {
			bindings = append(bindings, h.Origin)
		}
	}
	r.Files = files[:n:n]
	r.Dropped = append(dropped, r.Dropped...)
	msg := fmt.Sprintf("the text is over %s; %d files of bindings %s are left out", over, len(dropped), strings.Join(bindings, ", "))
	// With files left, the search found that they fit; with none, the
	// skills alone may be over.
	if c.Files = r.Files; n == 0 && !fits(&c) {
		var skills []string
		for _, h := range r.Skills {
			if !slices.Contains(skills, h.Origin) {
				skills = append(skills, h.Origin)
			}
		}
		msg = fmt.Sprintf("the text is over %s even without instruction files; the skills of bindings %s are too long", over, strings.Join(skills, ", "))
		if len(dropped) > 0 {
			msg = fmt.Sprintf("the text is over %s; %d files of bindings %s are left out, and the skills of bindings %s are still too long",
				over, len(dropped), strings.Join(bindings, ", "), strings.Join(skills, ", "))
		}
	}
	r.Errors = append(r.Errors, &catalog.Error{Err: &yamlx.Error{Mistake: yamlx.OverCapacity, Msg: msg}})
}
