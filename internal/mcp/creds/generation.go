package creds

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

const generationVersion = 1

type generationRecord struct {
	Version    int   `json:"version"`
	Generation int64 `json:"generation"`
}

func (s *Store) generationPath(id string) string {
	return filepath.Join(s.Dir, "generations", fileID(id)+".json")
}

func (s *Store) readGeneration(id string) (int64, error) {
	data, err := fsutil.ReadBounded(s.generationPath(id), 1024)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var record generationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return 0, err
	}
	if record.Version > generationVersion {
		return 0, &NewerError{id}
	}
	if record.Version != generationVersion || record.Generation < 0 {
		return 0, errors.New("invalid credential generation record")
	}
	return record.Generation, nil
}

// rememberGeneration retains cancellation history after encrypted records
// are removed. The caller holds the identity lock.
func (s *Store) rememberGeneration(id string, generation int64) error {
	previous, err := s.readGeneration(id)
	if err != nil || generation <= previous {
		return err
	}
	data, err := json.Marshal(generationRecord{Version: generationVersion, Generation: generation})
	if err != nil {
		return err
	}
	return writeDurable(s.generationPath(id), append(data, '\n'))
}

func (s *Store) currentGeneration(id string, r *Record) (int64, error) {
	generation, err := s.readGeneration(id)
	if r != nil {
		generation = max(generation, r.Generation)
	}
	return generation, err
}

// Inspect captures the record and its generation together, before prompts.
// It retains a legacy record's generation so deletion can invalidate it
// without needing its encryption key.
func (s *Store) Inspect(srv upstream.Server) (*Record, int64, error) {
	unlock, err := s.lock(srv)
	if err != nil {
		return nil, 0, err
	}
	defer unlock()
	if err := s.checkVersion(srv.ID, srv.ID); err != nil {
		return nil, 0, err
	}
	r, loadErr := s.load(srv.ID)
	if loadErr != nil && !errors.Is(loadErr, errUnreadable) {
		return nil, 0, loadErr
	}
	generation, err := s.currentGeneration(srv.ID, r)
	if err == nil {
		err = s.rememberGeneration(srv.ID, generation)
	}
	if err != nil {
		return nil, 0, err
	}
	return r, generation, loadErr
}
