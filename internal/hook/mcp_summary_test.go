package hook

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPSummary(t *testing.T) {
	for _, harness := range []string{"claude-code", "codex"} {
		t.Run(harness, func(t *testing.T) {
			h := testenv.Home(t)
			testenv.Write(t, h+"/rule.md", "A rule.")
			testenv.Skill(t, h+"/deploy", "description: Deploy.")
			for _, tc := range []struct{ name, legacyValue, servers, want string }{
				{"none", "", "[one]", "✓ ctxtra added 1 instruction and 1 skill"},
				{"one", "", "[one]", "✓ ctxtra added 1 instruction, 1 skill and 1 MCP server"},
				{"several", "", "[one, two]", "✓ ctxtra added 1 instruction, 1 skill and 2 MCP servers"},
				{"legacy_disabled_is_ignored", "0", "[one, two]", "✓ ctxtra added 1 instruction, 1 skill and 2 MCP servers"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Setenv("CTXTRA_EXPERIMENTAL_MCP", tc.legacyValue)
					selectedDir := "~"
					if tc.name == "none" {
						selectedDir = "~/unselected"
					}
					testenv.Config(t, h, "schema: ctxtra/config/v1\nmcp-servers:\n  one: {url: 'https://one.example.invalid/mcp'}\n  two: {url: 'https://two.example.invalid/mcp'}\nbindings:\n  - name: files\n    instructions: [~/rule.md]\n    skills: [~/deploy]\n  - name: selected\n    location: {dirs: ['"+selectedDir+"']}\n    instructions: [~/rule.md]\n    mcp-servers: "+tc.servers+"\n  - name: repeated\n    location: {dirs: ['"+selectedDir+"']}\n    mcp-servers: "+tc.servers+"\n  - name: elsewhere\n    location: {dirs: ['~/elsewhere']}\n    mcp-servers: [two]\n")
					var out, errOut bytes.Buffer
					input, _ := json.Marshal(map[string]string{"cwd": h})
					RunSlot(harness, "SessionStart", 0, bytes.NewReader(input), &out, &errOut)
					var answer struct {
						Message string `json:"systemMessage"`
					}
					if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
						t.Fatal(err)
					}
					if line := strings.Split(answer.Message, "\n")[0]; line != tc.want {
						t.Fatalf("got %q, want %q; stderr: %s", answer.Message, tc.want, &errOut)
					}
					wantList := ""
					if tc.name != "none" {
						wantList = "\nMCP servers:\n  - one"
						if tc.name == "several" || tc.name == "legacy_disabled_is_ignored" {
							wantList += "\n  - two"
						}
					}
					if wantList == "" && strings.Contains(answer.Message, "MCP servers:") || wantList != "" && !strings.HasSuffix(answer.Message, wantList) {
						t.Fatalf("unexpected list: %q", answer.Message)
					}
				})
			}
		})
	}
}
