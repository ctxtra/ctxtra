package oauth

import (
	"context"
	"testing"

	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/oauthtest"
)

func TestCreateOnlyAuthorizationPreservesInterveningLegacyRecord(t *testing.T) {
	for _, when := range []string{"before discovery", "before commit"} {
		t.Run(when, func(t *testing.T) {
			_, f := setup(t, oauthtest.Options{})
			ch := challenge(t, f)
			f.CreateOnly = true
			insert := func() {
				t.Helper()
				if _, err := f.Store.Update(f.Server, true, func(r *creds.Record) error {
					r.Generation, r.Token.Access = 0, "existing-token"
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if when == "before discovery" {
				insert()
				f.OpenBrowser = func(string) error { t.Fatal("existing record reached authorization"); return nil }
			} else {
				f.Commit = func(save func() error) error { insert(); return save() }
			}
			if _, err := f.Run(context.Background(), ch); err == nil {
				t.Fatal("create-only authorization replaced an intervening record")
			}
			if got := stored(t, f); got.Token.Access != "existing-token" || got.Generation != 0 {
				t.Fatalf("existing credentials changed: %+v", got)
			}
		})
	}
}
