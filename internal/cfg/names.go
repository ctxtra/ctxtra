package cfg

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// MaxName is the longest name a user or team chooses.
const MaxName = 64

// CheckName says what is wrong with a name of a source, binding, collection,
// repo set or profile, nil when nothing: 1 to 64 letters, digits, ., _ and
// -, starting with a letter. Case counts.
func CheckName(name string) error {
	const rule = "names are 1 to 64 letters, digits, ., _ and -, starting with a letter"
	switch {
	case name == "":
		return fmt.Errorf("the name is empty; %s", rule)
	case len(name) > MaxName:
		return fmt.Errorf("name %q is longer than %d characters", name, MaxName)
	case !isLetter(rune(name[0])):
		return fmt.Errorf("name %q doesn't start with a letter; %s", name, rule)
	}
	for _, r := range name {
		if !isLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._-", r) || r > unicode.MaxASCII {
			return fmt.Errorf("name %q has %q; %s", name, r, rule)
		}
	}
	return nil
}

func isLetter(r rune) bool { return r < unicode.MaxASCII && unicode.IsLetter(r) }

// Entry is one entry of a list as written: a path, pattern or name, with
// its ! flag and, for a reference into a source, the source's name.
type Entry struct {
	// Text is the entry as written.
	Text string
	// Not is true for an entry that starts with !: it leaves out what it
	// matches.
	Not bool
	// InSource is true for SOURCE:VALUE, also when SOURCE is empty, which
	// is an error.
	InSource bool
	// Source is the source that SOURCE:VALUE names; "" for a path on disk
	// or a name of the file's own.
	Source string
	// Value is the path, pattern or name, without ! and SOURCE:.
	Value string
	Pos   yamlx.Pos
	// Err is why the entry can't be used, nil when it can. A broken content
	// entry is left out; a broken positive location entry matches nothing.
	Err *yamlx.Error
}

// Owner is the kind of file a list is in: it decides what its entries can
// reference.
type Owner int

const (
	// InConfig: paths on disk, relative to the config's folder, and
	// references into sources.
	InConfig Owner = iota
	// InManifest: paths in the source, and names of the same manifest.
	InManifest
)

// listKind is what a content list holds.
type listKind int

const (
	pathList   listKind = iota // instructions, skills: files and skill folders
	nameList                   // collections: names or patterns over names
	serverList                 // mcp-servers: server names or patterns over them
)

// splitEntry reads the ! flag and the SOURCE: prefix of text. An entry that
// starts with ./, / or ~ is always a path on disk.
func splitEntry(text string) Entry {
	e := Entry{Text: text}
	e.Value, e.Not = strings.CutPrefix(text, "!")
	if strings.HasPrefix(e.Value, "./") || strings.HasPrefix(e.Value, "/") || strings.HasPrefix(e.Value, "~") {
		return e
	}
	if source, rest, ok := strings.Cut(e.Value, ":"); ok {
		e.InSource, e.Source, e.Value = true, source, rest
	}
	return e
}

// checkSource says what is wrong with the SOURCE of a SOURCE:VALUE entry.
func checkSource(e Entry) string {
	switch {
	case !e.InSource:
		return ""
	case e.Source == "":
		return fmt.Sprintf("%s: the source name before : is empty; start a file name with a colon with ./", e.Text)
	case CheckName(e.Source) != nil:
		return fmt.Sprintf("%s: %s names no source (%v); start a file name with a colon with ./", e.Text, e.Source, CheckName(e.Source))
	}
	return ""
}

// contentEntry checks one entry of a content list. The error, if any, is
// the entry's own.
func contentEntry(text string, kind listKind, owner Owner) (Entry, string) {
	e := splitEntry(text)
	value := e.Value
	if e.InSource {
		if owner == InManifest {
			what := map[listKind]string{pathList: "files in its own source", nameList: "its own collections", serverList: "its own servers"}[kind]
			return e, fmt.Sprintf("%s: a manifest references only %s, without a source name", text, what)
		}
		if msg := checkSource(e); msg != "" {
			return e, msg
		}
	}
	if value == "" {
		return e, "the entry is empty"
	}
	switch kind {
	case pathList:
		return e, checkPath(e, owner)
	case nameList:
		return e, checkNamePattern(value, CheckName)
	default:
		return e, checkNamePattern(value, config.CheckServerName)
	}
}

// checkPath checks a path entry: inside its source, or on disk in the
// config.
func checkPath(e Entry, owner Owner) string {
	v := e.Value
	inSource := e.InSource || owner == InManifest
	if inSource {
		if strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~") {
			where := "the source"
			if owner == InManifest {
				where = "this source"
			}
			return fmt.Sprintf("%s: a path in a source is relative to the top of %s", e.Text, where)
		}
		if leaves(v) {
			return fmt.Sprintf("%s: the path leaves its source", e.Text)
		}
	}
	if !doublestar.ValidatePattern(v) {
		return fmt.Sprintf("%s is not a valid pattern", e.Text)
	}
	return ""
}

// leaves reports whether relative path p goes above its top.
func leaves(p string) bool {
	c := path.Clean(p)
	return c == ".." || strings.HasPrefix(c, "../")
}

// checkNamePattern checks a name, or a pattern over names.
func checkNamePattern(v string, check func(string) error) string {
	if strings.ContainsAny(v, "*?[{") {
		if !doublestar.ValidatePattern(v) || strings.Contains(v, "/") {
			return fmt.Sprintf("%s is not a valid pattern over names", v)
		}
		return ""
	}
	if err := check(v); err != nil {
		return err.Error()
	}
	return ""
}

// checkRepoPattern checks a location.repos pattern: a repository's address
// or host/path, with globs.
func checkRepoPattern(v string) string {
	switch {
	case v == "":
		return "the repository is empty"
	case strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~") || strings.HasPrefix(v, "."):
		return fmt.Sprintf("%s is a folder; this list holds repositories, such as github.com/acme/api; folders go in dirs", v)
	case strings.ContainsAny(v, " \t"):
		return fmt.Sprintf("%q is not a repository", v)
	}
	id := RepoID(v)
	if !strings.Contains(id, "/") {
		return fmt.Sprintf("%s is not a repository; write host/path, such as github.com/acme/api", v)
	}
	if !doublestar.ValidatePattern(id) {
		return fmt.Sprintf("%s is not a valid pattern", v)
	}
	return ""
}

// checkDir checks a location.dirs pattern.
func checkDir(v string) string {
	if !strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "~") {
		return fmt.Sprintf("%s is a relative folder; dirs are absolute or start with ~", v)
	}
	if strings.HasPrefix(v, "~") && v != "~" && !strings.HasPrefix(v, "~/") {
		return fmt.Sprintf("%s: only ~ for your own home folder is read", v)
	}
	if !doublestar.ValidatePattern(v) {
		return fmt.Sprintf("%s is not a valid pattern", v)
	}
	return ""
}

// checkSubdir checks a subdirs pattern, relative to a repository's root.
func checkSubdir(v string) string {
	switch {
	case v == "":
		return "the folder is empty"
	case strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~"):
		return fmt.Sprintf("%s: subdirs are relative to the repository's root", v)
	case leaves(v):
		return fmt.Sprintf("%s leaves the repository", v)
	case !doublestar.ValidatePattern(v):
		return fmt.Sprintf("%s is not a valid pattern", v)
	}
	return ""
}
