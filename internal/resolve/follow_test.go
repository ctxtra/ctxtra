package resolve

import (
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/profile"
)

func TestFollowingWorkKeepsTheAgentAndProfiles(t *testing.T) {
	r := &Result{Dir: "/work/app", Agent: "opencode", Profiles: profile.Active{Names: []string{"iam", "oncall"}},
		Skills: []Hit{{Name: "ctxtra-follow", Path: "/skills/ctxtra-follow/SKILL.md"}}}
	want := "ctxtra context show FOLDER --agent opencode --profile iam --profile oncall --exclude-session '/work/app' --part 1"
	for _, text := range []string{r.Render(), r.RenderFiles()} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	r.Skills = nil
	if text := r.Render(); strings.Contains(text, "ctxtra context show") {
		t.Errorf("follow command without the skill: %s", text)
	}
}
