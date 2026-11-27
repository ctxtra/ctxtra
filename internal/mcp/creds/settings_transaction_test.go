package creds

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/edit"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

func TestSettingsFailureRestoresConfigAndSharedCredentials(t *testing.T) {
	for _, stage := range []string{"generations", "credentials"} {
		t.Run(stage, func(t *testing.T) {
			h := testenv.Home(t)
			file := testenv.Config(t, h, config.NewFile)
			keyring.MockInit()
			s := &Store{Dir: t.TempDir()}
			if _, err := s.EnsureKey(); err != nil {
				t.Fatal(err)
			}
			srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp", ClientID: "app"}
			if err := s.SetClientSecret(srv, "old"); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(s.recordPath(srv.ID, version))
			generation, err := s.Generation(srv)
			if err != nil {
				t.Fatal(err)
			}
			oldSync := syncFile
			t.Cleanup(func() { syncFile = oldSync })
			syncFile = func(f *os.File) error {
				if filepath.Dir(f.Name()) == filepath.Join(s.Dir, stage) {
					return errors.New("settings sync failed")
				}
				return oldSync(f)
			}
			secret := "new"
			wroteConfig := false
			files := edit.Files{Config: file, Mutex: filepath.Join(h, "config.lock"), Backups: filepath.Join(h, "backups")}
			add := func(tx *edit.Tx) error {
				_, err := tx.Config.Add(config.MCPServers, "shared", &yaml.Node{Kind: yaml.MappingNode,
					Content: []*yaml.Node{yamlx.Text("url"), yamlx.Text("https://example.com/mcp")}}, config.Place{})
				return err
			}
			err = files.RunCommit(add, func(write func() error) error {
				_, err := s.CommitSettings(srv, WriteGuard{ExpectedGeneration: &generation}, &Settings{ClientSecret: &secret}, func() error {
					if err := write(); err != nil {
						return err
					}
					wroteConfig = true
					return nil
				})
				return err
			})
			syncFile = oldSync
			if err == nil || !wroteConfig {
				t.Fatalf("did not exercise failure after config write: %v", err)
			}
			after, _ := os.ReadFile(s.recordPath(srv.ID, version))
			cfg, _ := os.ReadFile(file)
			if !bytes.Equal(before, after) || string(cfg) != config.NewFile {
				t.Fatal("failed settings save changed config or shared credentials")
			}
		})
	}
}

func TestCommitWithoutSettingsNeedsNoKeyAndChecksBeforeWriting(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	srv := upstream.Server{ID: "shared", Name: "shared", URL: "https://example.com/mcp"}
	wrong := int64(1)
	called := false
	commit := func() error { called = true; return nil }
	if _, err := s.CommitSettings(srv, WriteGuard{ExpectedGeneration: &wrong}, nil, commit); !errors.Is(err, ErrSignInChanged) || called {
		t.Fatalf("stale generation reached config write: %v, called=%v", err, called)
	}
	if _, err := s.CommitSettings(srv, WriteGuard{}, nil, commit); err != nil || !called {
		t.Fatalf("keyless config write: %v, called=%v", err, called)
	}
	if _, err := os.Stat(s.keyInfoPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("keyless commit created a credential key: %v", err)
	}
}
