package yamlx

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Mistake is a kind of error in ctxtra's files. Each kind leaves out a fixed
// part of what the user wrote: the smallest part that still makes sense.
type Mistake int

const (
	// Unreadable: YAML syntax, a wrong or missing schema, more than one
	// document, an anchor, alias or <<, a manifest with bindings or sources.
	Unreadable Mistake = iota + 1
	// DuplicateKey: a key that a map has twice.
	DuplicateKey
	// Tag: a YAML tag, such as an unquoted !entry.
	Tag
	// NotText: a number, true/false, a date or null where text is expected.
	NotText
	// UnknownTopKey: a top-level key the file doesn't have.
	UnknownTopKey
	// BadValue: a top-level key with a value of the wrong kind.
	BadValue
	// BrokenSource: a bad name, repo with dir, two of branch, tag and rev, a
	// rev that isn't a full commit ID, an unknown key.
	BrokenSource
	// Inactive: a repo source without a matching lock entry, or whose commit
	// isn't downloaded.
	Inactive
	// BrokenLockEntry: a lock entry that can't be used.
	BrokenLockEntry
	// BrokenServer: an MCP server definition that can't be used, or two
	// names for one server.
	BrokenServer
	// BrokenBinding: an unknown key, an invalid profile or unknown agent
	// name, nothing to add, a content list of only ! entries without
	// collections.
	BrokenBinding
	// DuplicateBinding: two bindings with the same name.
	DuplicateBinding
	// EmptyCondition: an empty list, empty map or null in a condition.
	EmptyCondition
	// BrokenLocation: a broken positive location entry. It stays and matches
	// nothing, so the binding never applies wider.
	BrokenLocation
	// BrokenExclusion: a ! location entry that can't be checked.
	BrokenExclusion
	// BrokenEntry: a content entry that can't be used, such as a file that
	// matches nothing, an unknown source, a path that leaves its source, a
	// missing collection or repo set, or a repo set entry that isn't a
	// repository.
	BrokenEntry
	// BrokenGroup: an unknown key in a collection or a repo set.
	BrokenGroup
	// EmptyRepoSet: a repo set without entries. It matches nothing.
	EmptyRepoSet
	// CollectionCycle: collections that contain each other.
	CollectionCycle
	// UntrustedFile: a file that others can change, outside a trusted dir
	// source, a symlink to one, or a symlink in a repo source.
	UntrustedFile
	// UnreadableFile: a file that can't be read, such as one over the size
	// limits.
	UnreadableFile
	// NotApproved: a source's MCP server not approved on this machine.
	NotApproved
	// OverCapacity: more text than the agent takes or max-ctx-chars allows.
	OverCapacity
	// SelectionUnreadable: the stored profile selection can't be read.
	SelectionUnreadable
	// BadProfileName: an invalid name in CTXTRA_PROFILES.
	BadProfileName
	// UnusedProfile: an active profile that no binding uses, likely a typo.
	// The other profiles still apply.
	UnusedProfile
)

// Out is how much of what the user wrote an error leaves out.
type Out int

const (
	// OutNothing: the error is reported, and nothing is left out on purpose.
	OutNothing Out = iota
	// OutItem: one file, skill or server that an entry adds.
	OutItem
	// OutEntry: one list entry.
	OutEntry
	// OutPart: the part that holds the error: a source, an MCP server, a
	// collection, a repo set, a binding, a top-level key or a lock entry.
	// Entries that use a left-out source or set get nothing from it.
	OutPart
	// OutFile: the whole file. For the lock, every repo source.
	OutFile
)

// leftOut is the table of what each mistake leaves out.
var leftOut = map[Mistake]Out{
	Unreadable:          OutFile,
	DuplicateKey:        OutPart,
	Tag:                 OutPart,
	NotText:             OutPart,
	UnknownTopKey:       OutPart,
	BadValue:            OutPart,
	BrokenSource:        OutPart,
	Inactive:            OutPart,
	BrokenLockEntry:     OutPart,
	BrokenServer:        OutPart,
	BrokenBinding:       OutPart,
	DuplicateBinding:    OutPart,
	EmptyCondition:      OutPart,
	BrokenLocation:      OutNothing,
	BrokenExclusion:     OutPart,
	BrokenEntry:         OutEntry,
	BrokenGroup:         OutPart,
	EmptyRepoSet:        OutNothing,
	CollectionCycle:     OutPart,
	UntrustedFile:       OutItem,
	UnreadableFile:      OutItem,
	NotApproved:         OutItem,
	OverCapacity:        OutNothing,
	SelectionUnreadable: OutFile,
	BadProfileName:      OutEntry,
	UnusedProfile:       OutNothing,
}

// Out is what m leaves out.
func (m Mistake) Out() Out { return leftOut[m] }

// Part names a part of a file as the user knows it: "source iam-team",
// "binding obs", "key max-ctx-chars". The zero Part is the whole file.
type Part struct {
	Kind string
	Name string
}

func (p Part) String() string {
	switch {
	case p.Kind == "":
		return "the file"
	case p.Name == "":
		return p.Kind
	}
	return p.Kind + " " + p.Name
}

// Pos is a position in a file, from 1. The zero Pos is no position.
type Pos struct {
	Line   int
	Column int
}

// At is the position of n.
func At(n *yaml.Node) Pos { return Pos{n.Line, n.Column} }

// Error is a mistake in a file: where it is, which part it is in, and what
// is wrong.
type Error struct {
	Pos     Pos
	Mistake Mistake
	Part    Part
	Msg     string
}

// Out is what the error leaves out.
func (e *Error) Out() Out { return e.Mistake.Out() }

func (e *Error) Error() string {
	s := e.Msg
	if e.Part.Kind != "" {
		s = e.Part.String() + ": " + s
	}
	if e.Pos.Line > 0 {
		s = fmt.Sprintf("line %d: %s", e.Pos.Line, s)
	}
	return s + e.Effect()
}

// Effect is the end of Error that says what is left out: "; the entry is
// left out", or "" for nothing.
func (e *Error) Effect() string {
	switch e.Out() {
	case OutFile:
		return "; nothing in the file is used"
	case OutPart:
		if e.Part.Kind != "" {
			return "; " + e.Part.String() + " is left out"
		}
	case OutEntry:
		return "; the entry is left out"
	case OutItem:
		return "; it is left out"
	}
	return ""
}

// Errorf is an error at n, in part p.
func Errorf(n *yaml.Node, m Mistake, p Part, format string, args ...any) *Error {
	var pos Pos
	if n != nil {
		pos = At(n)
	}
	return &Error{Pos: pos, Mistake: m, Part: p, Msg: fmt.Sprintf(format, args...)}
}
