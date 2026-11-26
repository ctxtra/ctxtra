package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// Values that look like secrets are only warned about, each once and in
// order; ordinary names aren't.
func TestSecretLookingValues(t *testing.T) {
	h := testenv.Home(t)
	testenv.Config(t, h, `schema: ctxtra/config/v1
mcp-servers:
  a: {url: 'https://x.example/mcp', headers: {Authorization: Bearer abc, X-Api-Key: k, X-Team: t}}
  b: {command: [x], env: {GITHUB_TOKEN: t, DB_PASSWORD: p, MY_SECRET_X: s, API_KEY: k, KEYBOARD: k, DB: dev}}
`)
	_, out, _ := run("doctor", h, "--json")
	var doc struct {
		Diagnostics []struct{ Severity, Message string }
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	var notes []string
	for _, d := range doc.Diagnostics {
		switch {
		case strings.Contains(d.Message, "config.yaml"):
			t.Errorf("a config error: %s", d.Message)
		case strings.Contains(d.Message, "looks like a secret"):
			notes = append(notes, d.Message)
		}
	}
	want := []string{
		"mcp-servers.a: headers.Authorization looks like a secret, which the config shouldn't hold; use secret-headers: [Authorization]",
		"mcp-servers.a: headers.X-Api-Key", "mcp-servers.b: env.API_KEY", "mcp-servers.b: env.DB_PASSWORD", "mcp-servers.b: env.GITHUB_TOKEN",
		"mcp-servers.b: env.MY_SECRET_X looks like a secret, which the config shouldn't hold; use secret-env: [MY_SECRET_X]",
	}
	if len(notes) != len(want) {
		t.Fatalf("notes %q", notes)
	}
	for i, w := range want {
		if !strings.Contains(notes[i], w) {
			t.Errorf("note %d: %s, want %q", i, notes[i], w)
		}
	}
}
