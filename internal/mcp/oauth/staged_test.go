package oauth

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/oauthtest"
)

func TestReplacementSecretsStayStagedUntilAuthorizationCommit(t *testing.T) {
	for _, outcome := range []string{"provider refusal", "commit refusal", "success"} {
		t.Run(outcome, func(t *testing.T) {
			fake, f := setup(t, oauthtest.Options{Lifetime: time.Hour})
			old := f.Server
			old.URL = "https://old.example/mcp"
			if _, err := f.Store.Update(old, true, func(r *creds.Record) error {
				r.Token.Access, r.Token.Refresh = "old-token", "old-refresh"
				r.Client.ID, r.Client.Secret = "old-app", "old-client-secret"
				r.Secrets.Headers = map[string]string{"Authorization": "old-header"}
				r.Generation = 123
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			files, _ := filepath.Glob(filepath.Join(f.Store.Dir, "credentials", "*.age"))
			before, _ := os.ReadFile(files[0])
			assertUnchanged := func() {
				t.Helper()
				after, err := os.ReadFile(files[0])
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("pending authorization replaced the old credentials")
				}
			}
			generation := int64(123)
			f.Replace, f.ExpectedGeneration = true, &generation
			f.Secrets = &creds.Secrets{Headers: map[string]string{"Authorization": "new-header"}}
			var edit func(url.Values)
			if outcome == "provider refusal" {
				edit = func(q url.Values) { q.Set("error", "access_denied") }
			}
			open := browser(fake, edit)
			f.OpenBrowser = func(authURL string) error {
				assertUnchanged()
				return open(authURL)
			}
			committed := false
			f.Commit = func(save func() error) error {
				assertUnchanged()
				if outcome == "commit refusal" {
					return errors.New("definition changed before commit")
				}
				committed = true
				return save()
			}
			_, err := f.Run(context.Background(), challenge(t, f))
			if outcome != "success" {
				if err == nil || committed {
					t.Fatalf("failed authorization committed: %v", err)
				}
				assertUnchanged()
				return
			}
			if err != nil || !committed {
				t.Fatalf("authorization did not commit: %v", err)
			}
			r := stored(t, f)
			if !r.Matches(f.Server) || r.Token.Access == "" || r.Token.Access == "old-token" || r.Token.Refresh == "old-refresh" ||
				r.Client.Secret == "old-client-secret" || r.Secrets.Headers["Authorization"] != "new-header" || r.Generation <= generation {
				t.Fatalf("replacement mixed old and new credentials: %+v", r)
			}
		})
	}
}
