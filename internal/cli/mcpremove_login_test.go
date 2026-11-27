package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/edit"
	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/oauth"
	"github.com/ctxtra/ctxtra/internal/mcp/oauthtest"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestMCPRemovalCancelsPendingAuthorization(t *testing.T) {
	for _, mode := range []string{"other config", "legacy record in other config", "remove and readd"} {
		t.Run(mode, func(t *testing.T) {
			h := testenv.Home(t)
			keyring.MockInit()
			fake := oauthtest.New(t, oauthtest.Options{})
			declaration := "schema: ctxtra/config/v1\nmcp-servers:\n  shared: {url: '" + fake.ResourceURL() + "'}\n"
			file := testenv.Config(t, h, declaration)
			srv, ok := serverIn(t, file, "shared")
			if !ok {
				t.Fatal("missing server")
			}
			store := openStore()
			store.HTTP = fake.Client()
			if _, err := store.EnsureKey(); err != nil {
				t.Fatal(err)
			}
			if mode == "legacy record in other config" {
				if _, err := store.Update(srv, true, func(r *creds.Record) error { r.Generation = 0; r.Client.CallbackPort = 4567; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			removalFile := file
			if mode != "remove and readd" {
				removalFile = filepath.Join(h, "other-config.yaml")
				testenv.Write(t, removalFile, declaration)
			}
			generation, err := store.Generation(srv)
			if err != nil || generation != 0 {
				t.Fatalf("initial generation: %d, %v", generation, err)
			}
			var output bytes.Buffer
			f := &oauth.SignIn{Server: srv, Store: store, HTTP: fake.Client(), Out: &output, ExpectedGeneration: &generation, Commit: stillDeclared(srv)}
			f.OpenBrowser = func(authURL string) error {
				s := newSources(IO{Out: &output, Err: &output})
				s.file, s.files.Config = removalFile, removalFile
				remove := func(tx *edit.Tx) error { return tx.Config.Remove(config.MCPServers, "shared") }
				if err := s.writeCommit(remove, func(write func() error) error {
					if err := write(); err != nil {
						return err
					}
					return store.Delete(srv)
				}); err != nil {
					return err
				}
				if mode == "remove and readd" {
					if n := mcpAdd(IO{In: strings.NewReader(""), Out: &output, Err: &output}, "shared", srv.URL, nil, mcpAddOptions{noLogin: true}); n != 0 {
						t.Fatalf("readd failed: %s", &output)
					}
				}
				if r, err := store.Load(srv); err != nil || r != nil {
					t.Fatalf("removal retained credentials: %+v, %v", r, err)
				}
				client := *fake.Client()
				client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
				resp, err := client.Get(authURL)
				if err != nil {
					return err
				}
				resp.Body.Close()
				callback, err := url.Parse(resp.Header.Get("Location"))
				if err != nil {
					return err
				}
				go func() {
					client := &http.Client{Timeout: 5 * time.Second}
					if resp, err := client.Get(callback.String()); err == nil {
						resp.Body.Close()
					}
				}()
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ch, err := oauth.Detect(ctx, fake.Client(), srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Run(ctx, ch); !errors.Is(err, creds.ErrSignInChanged) {
				t.Fatalf("old authorization survived removal: %v, output %s", err, &output)
			}
			if r, err := store.Load(srv); err != nil || r != nil {
				t.Fatalf("old callback recreated credentials: %+v, %v", r, err)
			}
		})
	}
}
