package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestEditWithCommitRestoresConfigOnFailure(t *testing.T) {
	for _, shape := range []string{"existing", "missing", "symlink"} {
		t.Run(shape, func(t *testing.T) {
			h := testenv.Home(t)
			file := filepath.Join(h, "config.yaml")
			original := []byte("schema: ctxtra/v1\n# preserve this comment\n")
			target := file
			if shape == "symlink" {
				target = filepath.Join(h, "dotfiles.yaml")
				if err := os.Symlink(target, file); err != nil {
					t.Fatal(err)
				}
			}
			if shape != "missing" {
				if err := os.WriteFile(target, original, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("credential save failed")
			err := EditWithCommit(file, func(old []byte) ([]byte, error) {
				if len(old) == 0 {
					old = []byte(NewConfig)
				}
				if i := bytes.Index(old, []byte("preserve")); i >= 0 {
					old[i] = 'P'
				}
				return append(old, []byte("# committed config\n")...), nil
			}, func(write func() error) error {
				if err := write(); err != nil {
					return err
				}
				return failure
			})
			if !errors.Is(err, failure) {
				t.Fatalf("commit failure: %v", err)
			}
			data, err := os.ReadFile(file)
			if shape == "missing" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed edit left a new config: %s, %v", data, err)
				}
				return
			}
			if err != nil || !bytes.Equal(data, original) {
				t.Fatalf("failed edit did not restore content: %s, %v", data, err)
			}
			st, err := os.Stat(file)
			if err != nil || st.Mode().Perm() != 0o600 {
				t.Fatalf("restored config mode: %v, %v", st, err)
			}
			if shape == "symlink" {
				if link, err := os.Readlink(file); err != nil || link != target {
					t.Fatalf("restoration replaced the symlink: %q, %v", link, err)
				}
			}
		})
	}
}
