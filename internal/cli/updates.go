package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/source"
	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/update"
)

// backgroundTimeout ends a check that a session started. Each request has a
// shorter timeout of its own; this one only ends a check that hangs, with
// room for many sources that wait for each other.
const backgroundTimeout = 15 * time.Minute

func (a *app) checkUpdatesCmd() *cobra.Command {
	var (
		background bool
		sel        update.Selection
	)
	c := &cobra.Command{
		Use:   "check-updates",
		Short: "Check for a new ctxtra release, new commits and moved tags of sources",
		Long: `Check for a new ctxtra release (unless the config has check-releases: false),
and for new commits or a moved tag of every repo source unless it has
check-updates: false, and save the results for sessions. A source at a rev, or
one not on this machine, isn't checked. The checks read version numbers and the
commit IDs of branches and tags. For a branch source with subdir, a check that
finds a new commit also fetches the branch into ctxtra's cache, to tell whether
anything changed under subdir. A check accepts nothing, and git never asks for
credentials. Sessions run this check in the background at most once an hour;
run it to check now. It exits with 1 when a check fails.`,
		Example: "  ctxtra check-updates",
		Args:    noArgs,
		GroupID: "inspect",
		RunE: a.run(func(*cobra.Command, []string) error {
			source.NoPrompt = true
			env := loadEnv()
			if background {
				ctx, cancel := context.WithTimeout(context.Background(), backgroundTimeout)
				defer cancel()
				_, _ = update.Run(ctx, update.FromConfig(env.Config, env.Lock), a.version, &sel)
				return nil
			}
			return code(a.checkUpdates(env))
		}),
	}
	// What the session that started the check claimed.
	f := c.Flags()
	f.BoolVar(&background, "background", false, "print nothing, and check only what the other flags select; for sessions")
	f.BoolVar(&sel.Release, "release", false, "with --background: check for a new release")
	f.StringArrayVar(&sel.Sources, "source", nil, "with --background: check the source `NAME` (repeatable)")
	for _, name := range []string{"background", "release", "source"} {
		_ = f.MarkHidden(name)
	}
	return c
}

func (a *app) checkUpdates(env *resolve.Env) int {
	if err := env.Fatal(); err != nil {
		return failEnv(a.IO, "check-updates", false, err)
	}
	for _, e := range env.Errors {
		fmt.Fprintf(a.Err, "warning: %s\n", e)
	}
	checks := update.FromConfig(env.Config, env.Lock)
	switch {
	case !checks.Releases && len(checks.Sources) == 0:
		fmt.Fprintln(a.Out, "nothing to check: the config has check-releases: false, and no repo source has update checks on")
		return 0
	}
	results, werr := update.Run(context.Background(), checks, a.version, nil)
	n := 0
	if werr != nil {
		fmt.Fprintf(a.Err, "ctxtra check-updates: can't save the results in %s, so sessions won't show them: %v\n", paths.Tilde(state.Path()), werr)
		n = 1
	}
	// Shown from the results, not from state.json, which may not have them.
	st := state.Load()
	for _, r := range results {
		if r.Err != nil {
			name := r.Name
			if r.Release {
				name = "ctxtra"
			}
			fmt.Fprintf(a.Out, "%s: can't check: %v\n", name, r.Err)
			n = 1
			continue
		}
		if r.Release {
			st.Ctxtra = state.Release{Check: state.Check{Checked: r.At}, Latest: r.Latest}
			fmt.Fprintf(a.Out, "ctxtra %s: %s\n", a.version, releaseStatus(checks, st, a.version))
			continue
		}
		src := env.Config.Source(r.Name)
		e := st.Get(update.StateKey(src))
		e.Head, e.Unchanged, e.Checked, e.Error = r.Latest, r.Unchanged, r.At, ""
		accepted := ""
		if le := env.Lock.Entry(src.Name); le != nil {
			accepted = le.Commit
		}
		fmt.Fprintf(a.Out, "%s: %s\n", src.Name, checkStatus(src, accepted, e))
	}
	return n
}

// lastWorked tells when the last check that worked ran, and what it found,
// after a failed one.
func lastWorked(c state.Check, found string) string {
	if c.Checked.IsZero() {
		return ""
	}
	return " (the last one that worked, " + c.Checked.Format(timeFormat) + ", found " + found + ")"
}

// timeFormat is how reports show the time of a fetch or check.
const timeFormat = "2006-01-02 15:04"

// releaseStatus says what doctor knows about ctxtra's releases.
func releaseStatus(cfg update.Config, st state.State, version string) string {
	r := st.Ctxtra
	switch {
	case !cfg.Releases:
		return "release check off (check-releases: false)"
	case r.Error != "":
		return "the last check for a new release failed: " + r.Error + lastWorked(r.Check, "latest release "+r.Latest)
	case r.Checked.IsZero() && !update.IsRelease(version):
		return "a " + version + " build, which sessions don't check for new releases"
	case r.Checked.IsZero():
		return "not checked for a new release yet"
	}
	if latest, command := update.Available(cfg, st, version); latest != "" {
		s := "update " + latest + " is available - " + update.Releases
		if command != "" {
			s += "; run `" + command + "` to update"
		}
		return s
	}
	checked := " (checked " + r.Checked.Format(timeFormat) + ")"
	if !update.IsRelease(version) {
		return "the latest release is " + r.Latest + checked + "; this is a " + version + " build"
	}
	return "the latest release" + checked
}

// releaseInfo is what doctor --json knows about ctxtra's releases.
func releaseInfo(cfg update.Config, st state.State, version string) map[string]any {
	m := map[string]any{"version": version, "check_releases": cfg.Releases}
	r := st.Ctxtra
	if r.Latest != "" {
		m["latest"] = r.Latest
	}
	if !r.Checked.IsZero() {
		m["checked"] = r.Checked.Format(time.RFC3339)
	}
	if r.Error != "" {
		m["check_error"] = r.Error
	}
	return m
}

// checkStatus says what the update checks know about the repo source src,
// with the accepted commit accepted, which has check-updates on.
func checkStatus(src *config.Source, accepted string, e state.Source) string {
	none, found, what := "no new commits", "new commits", "for new commits"
	if src.Ref.Kind == config.Tag {
		none, found, what = "the tag has not moved", "the tag moved", "of the tag"
	}
	switch {
	case e.Error != "":
		was := none
		switch {
		case e.New(accepted):
			was = found
		case e.Head != "" && e.Head == e.Dismissed && e.Head != accepted:
			was = "the dismissed commit"
		}
		return "the last check " + what + " failed: " + e.Error + lastWorked(e.Check, was)
	case e.Checked.IsZero():
		return "not checked " + what + " yet"
	}
	checked := " (checked " + e.Checked.Format(timeFormat) + ")"
	switch {
	case e.New(accepted):
	case e.Head != "" && e.Head == e.Dismissed && e.Head != accepted:
		return none + " but the dismissed one" + checked
	default:
		return none + checked
	}
	them := "them"
	if src.Ref.Kind == config.Tag {
		them = "it"
	}
	return fmt.Sprintf("%s%s; run `ctxtra source update %s` to review %s", found, checked, src.Name, them)
}
