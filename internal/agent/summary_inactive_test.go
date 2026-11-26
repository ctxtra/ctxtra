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
// their repair, and counts the rest.
func TestSummaryNamesInactiveSources(t *testing.T) {
	res := &resolve.Result{}
	for i := range 5 {
		res.Errors = append(res.Errors, &catalog.Error{Err: &yamlx.Error{Mistake: yamlx.Inactive, Part: yamlx.Part{Kind: "source", Name: fmt.Sprintf("s%d", i)},
			Msg: fmt.Sprintf("run `ctxtra source update s%d`", i)}})
	}
	got := Summary(res)
	for _, want := range []string{"5 errors (run ctxtra doctor)", "⚠ source s0: run ctxtra source update s0", "⚠ source s2: run ctxtra source update s2",
		"⚠ 2 more sources can't be read (run ctxtra doctor)"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "s3") {
		t.Errorf("more than three named:\n%s", got)
	}
}
