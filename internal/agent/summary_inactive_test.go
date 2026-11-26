package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/resolve"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// The session message names the first sources that can't be read, with
// their repair, and counts the other errors.
func TestSummaryNamesInactiveSources(t *testing.T) {
	res := &resolve.Result{}
	for i := range 5 {
		res.Errors = append(res.Errors, &catalog.Error{Err: &yamlx.Error{Mistake: yamlx.Inactive, Part: yamlx.Part{Kind: "source", Name: fmt.Sprintf("s%d", i)},
			Msg: fmt.Sprintf("run `ctxtra source update s%d`", i)}})
	}
	got := Summary(res)
	for _, want := range []string{"5 errors (run ctxtra doctor)", "⚠ source s0: run ctxtra source update s0", "⚠ source s2: run ctxtra source update s2",
		"⚠ 2 more errors (run ctxtra doctor)"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "s3") {
		t.Errorf("more than three named:\n%s", got)
	}
}

// The session message names the first repo sets that can't be read, with
// their bindings, and counts the other errors.
func TestSummaryNamesUnreadableRepoSets(t *testing.T) {
	res := &resolve.Result{UnreadableSets: []resolve.UnreadableSet{
		{Binding: "iam-repos", Entry: "iam-team:services", Why: "source iam-team has no repo set services"},
		{Binding: "work", Entry: "!corp:clients", Why: "source corp has no repo set clients"},
	}}
	for i := range 3 {
		res.UnreadableSets = append(res.UnreadableSets, resolve.UnreadableSet{Binding: fmt.Sprintf("b%d", i), Entry: "acme", Why: "the config has no repo set acme"})
	}
	got := Summary(res)
	for _, want := range []string{
		"⚠ binding iam-repos: repo set iam-team:services can't be read (source iam-team has no repo set services); the entry matches nothing",
		"⚠ binding work: repo set !corp:clients can't be read (source corp has no repo set clients); the binding is left out",
		"⚠ binding b0: repo set acme can't be read",
		"⚠ 2 more errors (run ctxtra doctor)"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "b1") {
		t.Errorf("more than three named:\n%s", got)
	}
}

// A missing file: the session message names the first errors as doctor
// does, with their file and line, after the errors that have a command to
// run, and counts the rest under one bound.
func TestSummaryNamesErrors(t *testing.T) {
	missing := &catalog.Error{File: "/x/config.yaml", Err: &yamlx.Error{Pos: yamlx.Pos{Line: 7}, Mistake: yamlx.BrokenEntry,
		Part: yamlx.Part{Kind: "binding", Name: "me"}, Msg: "~/notes/gone.md is not a file"}}
	inactive := &catalog.Error{Err: &yamlx.Error{Mistake: yamlx.Inactive, Part: yamlx.Part{Kind: "source", Name: "team"}, Msg: "run `ctxtra source update team`"}}
	set := &catalog.Error{Err: &yamlx.Error{Mistake: yamlx.BrokenLocation, Part: yamlx.Part{Kind: "binding", Name: "b"}, Msg: "repo-sets acme can't be read"}}
	control := &catalog.Error{Err: &yamlx.Error{Mistake: yamlx.BrokenEntry, Msg: "a\x1b[2Jb\nc `d`"}}
	res := &resolve.Result{Errors: []*catalog.Error{missing, inactive, set, control},
		UnreadableSets: []resolve.UnreadableSet{{Binding: "b", Entry: "acme", Why: "the config has no repo set acme", Err: set}}}
	got := Summary(res)
	want := "ctxtra added nothing; 4 errors (run ctxtra doctor)\n\n⚠ source team: run ctxtra source update team\n" +
		"⚠ binding b: repo set acme can't be read (the config has no repo set acme); the entry matches nothing\n" +
		"⚠ " + missing.Error() + "\n⚠ 1 more error (run ctxtra doctor)"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	long := &catalog.Error{Err: &yamlx.Error{Mistake: yamlx.BrokenEntry, Msg: strings.Repeat("é", 300)}}
	res.Errors, res.UnreadableSets = []*catalog.Error{long}, nil
	if got := Summary(res); !strings.HasSuffix(got, "\n⚠ "+strings.Repeat("é", 159)+"…") {
		t.Errorf("long error: %q", got)
	}
	res.Errors = []*catalog.Error{control}
	if got := Summary(res); !strings.Contains(got, "⚠ a\\u001b[2Jb c d;") {
		t.Errorf("not sanitized: %q", got)
	}
}
