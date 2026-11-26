package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestFollowingWorkIntoAnotherFolder(t *testing.T) {
	for _, selected := range [][]string{{"iam"}, nil} {
		name := "a Claude session follows its work"
		if selected == nil {
			name = "a session without profiles follows its work"
		}
		t.Run(name, func(t *testing.T) {
			h := testenv.Home(t)
			dir := filepath.Join(h, "session 'with quotes'")
			target := filepath.Join(h, "other")
			for _, d := range []string{dir, target} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			testenv.Skill(t, filepath.Join(h, "follow"), "name: ctxtra-follow\ndescription: Follow work.")
			for _, rule := range []string{"IAM", "OBS", "CLAUDE"} {
				testenv.Write(t, filepath.Join(h, rule+".md"), rule+"-RULES")
			}
			testenv.Config(t, h, `schema: ctxtra/config/v1
bindings:
  - {name: follow, skills: [~/follow]}
  - {name: iam, location: {dirs: [~/other]}, profiles: [iam], instructions: [~/IAM.md]}
  - {name: obs, location: {dirs: [~/other]}, profiles: [observability], instructions: [~/OBS.md]}
  - {name: agent, location: {dirs: [~/other]}, agents: [claude-code], instructions: [~/CLAUDE.md]}
`)
			if err := profile.Store(selected); err != nil {
				t.Fatal(err)
			}
			in, _ := json.Marshal(map[string]string{"cwd": dir, "session_id": "s", "source": "startup"})
			var out, errOut strings.Builder
			if n := Run([]string{"hook", "claude-code", "SessionStart", "--slot", "0", "--of", "40", "--setup-version=1"}, IO{In: strings.NewReader(string(in)), Out: &out, Err: &errOut}, "test"); n != 0 {
				t.Fatalf("hook: %d %s", n, errOut.String())
			}
			var answer struct {
				HookSpecificOutput struct{ AdditionalContext string }
			}
			if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
				t.Fatal(err)
			}
			_, command, ok := strings.Cut(answer.HookSpecificOutput.AdditionalContext, "```bash\n")
			command, _, _ = strings.Cut(command, "\n```")
			if !ok || !strings.Contains(command, " --agent claude-code ") {
				t.Fatalf("missing follow command: %s", out.String())
			}
			if selected == nil && !strings.HasPrefix(command, "CTXTRA_PROFILES= ") || selected != nil && !strings.Contains(command, " --profile iam ") {
				t.Fatalf("wrong selection in command: %s", command)
			}
			if err := profile.Store([]string{"observability"}); err != nil {
				t.Fatal(err)
			}
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			shim := filepath.Join(h, "bin", "ctxtra")
			testenv.Write(t, shim, "#!/bin/sh\nexec '"+strings.ReplaceAll(bin, "'", "'\"'\"'")+"' \"$@\"\n")
			if err := os.Chmod(shim, 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/sh", "-c", strings.Replace(command, "FOLDER", "'"+target+"'", 1))
			cmd.Env = append(os.Environ(), "CTXTRA_TEST_HOOK=1", "PATH="+filepath.Dir(shim)+":"+os.Getenv("PATH"))
			b, err := cmd.CombinedOutput()
			text := string(b)
			if err != nil || !strings.Contains(text, "CLAUDE-RULES") || strings.Contains(text, "OBS-RULES") || strings.Contains(text, "IAM-RULES") != (selected != nil) {
				t.Fatalf("follow: %v\n%s\n%s", err, command, text)
			}
		})
	}
}
