// Package profile decides which profiles are active: for a new session, for
// a hook event of a running session, and for a command. The hook, the MCP
// proxy and the commands that show what a session gets all ask it, and
// resolve takes the answer as input.
package profile

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Env is the variable that sets the active profiles for one launch.
const Env = "CTXTRA_PROFILES"

// Origin is where an active set comes from.
type Origin string

const (
	// None: neither the variable nor a stored selection.
	None Origin = ""
	// FromEnv: CTXTRA_PROFILES, set even to an empty value.
	FromEnv Origin = Env
	// FromStored: the stored selection.
	FromStored Origin = "stored selection"
	// FromFlag: --profile on the command line.
	FromFlag Origin = "--profile"
)

// Active is a set of active profiles.
type Active struct {
	// Names are the active profiles, each once, in the order given.
	Names  []string
	Origin Origin
	// Kept is set when a hook event took the set from its session's
	// record, chosen when the session started.
	Kept bool
	// Errors are invalid names in the variable and a stored selection that
	// can't be read: each leaves out only itself.
	Errors []*catalog.Error
}

// String is the set and its origin, for messages: "iam, oncall (from
// CTXTRA_PROFILES)", "none".
func (a Active) String() string {
	s := "none"
	if len(a.Names) > 0 {
		s = strings.Join(a.Names, ", ")
	}
	from := ""
	if a.Origin != None {
		from = "from " + string(a.Origin)
	}
	if a.Kept {
		from = strings.TrimPrefix(from+", kept from the session's start", ", ")
	}
	if from != "" {
		s += " (" + from + ")"
	}
	return s
}

// Shown reports whether a session message names the set: when a profile
// is active or there is an error.
func (a Active) Shown() bool { return len(a.Names) > 0 || len(a.Errors) > 0 }

// Current is the set a new session gets: CTXTRA_PROFILES when it is set,
// even to an empty value, else the stored selection. A selection that can't
// be read is an error, and no profile comes from it.
func Current() Active {
	if v, ok := os.LookupEnv(Env); ok {
		names, errs := parse(v)
		return Active{Names: names, Origin: FromEnv, Errors: errs}
	}
	names, ok, err := Stored()
	switch {
	case err != nil:
		return Active{Errors: []*catalog.Error{{Err: &yamlx.Error{Mistake: yamlx.SelectionUnreadable, Msg: err.Error()}}}}
	case !ok:
		return Active{}
	}
	return Active{Names: names, Origin: FromStored}
}

// Given is the set of --profile names, for a command that shows a session
// with them instead of the current set. An invalid name is an error.
func Given(names []string) (Active, error) {
	out := Active{Origin: FromFlag}
	for _, n := range names {
		if err := config.CheckName(n); err != nil {
			return Active{}, fmt.Errorf("--profile %q: %w", n, err)
		}
		if !slices.Contains(out.Names, n) {
			out.Names = append(out.Names, n)
		}
	}
	return out, nil
}

// parse reads the variable's value: names separated by commas. Spaces
// around names and empty entries don't count, a name given twice counts
// once, and an invalid name is an error that leaves out only that name.
func parse(v string) ([]string, []*catalog.Error) {
	var names []string
	var errs []*catalog.Error
	for _, n := range strings.Split(v, ",") {
		n = strings.TrimSpace(n)
		switch err := config.CheckName(n); {
		case n == "" || slices.Contains(names, n):
		case err != nil:
			errs = append(errs, &catalog.Error{Err: &yamlx.Error{Mistake: yamlx.BadProfileName, Part: yamlx.Part{Kind: Env},
				Msg: fmt.Sprintf("%q is not a valid profile name: %v", n, err)}})
		default:
			names = append(names, n)
		}
	}
	return names, errs
}

// Used are the profile names that the bindings of c use, a ! entry
// included, each with the bindings that use it in config order.
func Used(c *config.Config) map[string][]string {
	out := map[string][]string{}
	for _, b := range c.Bindings {
		for _, e := range b.Profiles {
			if config.CheckName(e.Value) == nil && !slices.Contains(out[e.Value], b.Name) {
				out[e.Value] = append(out[e.Value], b.Name)
			}
		}
	}
	return out
}
