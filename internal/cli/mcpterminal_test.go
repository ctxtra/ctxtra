package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/oauth"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPShowSanitizesSourceBeforeApproval(t *testing.T) {
	h := testenv.Home(t)
	entry := lockedSource(t, "work", "https://example.com/context", map[string]string{
		manifest.FileName: "schema: ctxtra/source/v1\nmcp-servers:\n  build:\n    command: [missing-mcp, \"\\e]0;command\\a\"]\n    notes: \"\\e]0;notes\\a\\u202e\"\n"})
	// A manifest of a repo source is displayed even when it cannot be approved or run.
	writeLock(t, testenv.Config(t, h, "schema: ctxtra/config/v1\nsources:\n  work: {repo: 'https://example.com/context', branch: main}\n"), entry)
	var output bytes.Buffer
	if n := Run([]string{"mcp", "show", "work:build"}, IO{In: strings.NewReader(""), Out: &output, Err: &output}, "test"); n != 1 {
		t.Fatalf("unapproved show unexpectedly succeeded: %d, %s", n, &output)
	}
	if strings.ContainsAny(output.String(), "\x1b\x07\u202e") || !strings.Contains(output.String(), `\u001b]0;notes\u0007\u202e`) || !strings.Contains(output.String(), `\u001b]0;command\u0007`) {
		t.Fatalf("unsafe or missing source display: %q", output.String())
	}
	output.Reset()
	if n := Run([]string{"mcp", "list", "--json"}, IO{Out: &output, Err: &output}, "test"); n != 0 || !json.Valid(output.Bytes()) {
		t.Fatalf("machine output changed: %d, %s", n, &output)
	}
	var data any
	if err := json.Unmarshal(output.Bytes(), &data); err != nil || !strings.Contains(output.String(), `\u001b]0;notes\u0007`) {
		t.Fatalf("machine output lost original metadata: %v, %s", err, &output)
	}
}

func TestMCPStatusSanitizesUnapprovedCommand(t *testing.T) {
	h := testenv.Home(t)
	entry := lockedSource(t, "work", "https://example.com/context", map[string]string{
		manifest.FileName: "schema: ctxtra/source/v1\nmcp-servers:\n  build:\n    command: [\"evil\\e]0;title\\a\\u009b2J\"]\n"})
	writeLock(t, testenv.Config(t, h, "schema: ctxtra/config/v1\nsources:\n  work: {repo: 'https://example.com/context', branch: main}\nbindings:\n  - {name: global, mcp-servers: ['work:build']}\n"), entry)
	var output bytes.Buffer
	if n := Run([]string{"mcp", "status", h}, IO{Out: &output, Err: &output}, "test"); n != 0 || strings.ContainsAny(output.String(), "\x1b\x07\u009b") || !strings.Contains(output.String(), `evil\u001b]0;title\u0007\u009b2J`) {
		t.Fatalf("unsafe status: %d, %q", n, output.String())
	}
}

func TestMCPHumanCatalogSanitizesIssuerWithoutChangingJSON(t *testing.T) {
	h := testenv.Home(t)
	testenv.Config(t, h, "schema: ctxtra/config/v1\nmcp-servers:\n  shared: {url: https://example.com/mcp}\n")
	issuer := "issuer\x1b]0;title\x07\u009b2J"
	state, err := json.Marshal(creds.State{Version: 1, Name: "shared", URL: "https://example.com/mcp", SignedIn: true, Issuer: issuer})
	if err != nil {
		t.Fatal(err)
	}
	testenv.Write(t, filepath.Join(openStore().Dir, "state", "shared.json"), string(state))
	for _, args := range [][]string{{"mcp", "list"}, {"doctor"}} {
		var output bytes.Buffer
		Run(args, IO{Out: &output, Err: &output}, "test")
		if strings.ContainsAny(output.String(), "\x1b\x07\u009b") || !strings.Contains(output.String(), `issuer\u001b]0;title\u0007\u009b2J`) {
			t.Fatalf("unsafe %v: %q", args, output.String())
		}
	}
	var output bytes.Buffer
	if n := Run([]string{"mcp", "list", "--json"}, IO{Out: &output, Err: &output}, "test"); n != 0 {
		t.Fatalf("machine state: %d, %q", n, output.String())
	}
	var document struct {
		Data []mcpInfo `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &document); err != nil || len(document.Data) != 1 || document.Data[0].SignIn != "signed in with "+issuer {
		t.Fatalf("machine state lost metadata: %v, %q", err, output.String())
	}
}

func TestMCPSignInResultSanitizesMetadata(t *testing.T) {
	var output bytes.Buffer
	printSignInResult(&output, upstream.Server{ID: "work:build", Notes: "Choose work\n\x1b]0;notes\x07\u202e"}, &oauth.Result{Issuer: "issuer\x1b[2J", Account: "user\x1b]0;account\x07"})
	if strings.ContainsAny(output.String(), "\x1b\x07\u202e") || !strings.Contains(output.String(), "Choose work\n\\u001b]0;notes\\u0007\\u202e") || !strings.Contains(output.String(), `issuer\u001b[2J`) || !strings.Contains(output.String(), `user\u001b]0;account\u0007`) {
		t.Fatalf("unsafe sign-in result: %q", output.String())
	}
}
