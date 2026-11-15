package creds

import (
	"errors"
	"fmt"
	"maps"

	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

// WriteGuard binds a write to the record inspected before prompting.
// Replacement requires both explicit consent and an expected generation.
// CreateOnly also rejects records written without advancing a legacy generation.
type WriteGuard struct {
	ExpectedGeneration *int64
	Replace            bool
	CreateOnly         bool
}

func (g WriteGuard) check(srv upstream.Server, r *Record, generation int64) error {
	if g.Replace && g.ExpectedGeneration == nil {
		return errors.New("credential replacement needs an expected generation")
	}
	if g.ExpectedGeneration != nil && generation != *g.ExpectedGeneration {
		return ErrSignInChanged
	}
	if r != nil && !r.Matches(srv) && !g.Replace {
		return bindingConflict(srv)
	}
	if g.CreateOnly && r != nil {
		return fmt.Errorf("%s already has shared credentials; use ctxtra mcp login %s to change them", srv.ID, srv.ID)
	}
	return nil
}

// Settings groups secret and callback changes into one credential write.
// Nil fields preserve the corresponding stored values.
type Settings struct {
	Secrets      *Secrets
	ClientSecret *string
	CallbackPort *int
}

// SaveSettings checks the binding and generation under the identity lock,
// then stores all settings together. The caller made sure a key exists.
func (s *Store) SaveSettings(srv upstream.Server, guard WriteGuard, settings Settings) (*Record, error) {
	return s.CommitSettings(srv, guard, &settings, nil)
}

// CommitSettings checks the binding and generation before commit runs,
// holding the identity lock until the optional settings are saved. A nil
// settings argument commits without creating credentials or needing a key.
// Callers must restore their own changes if the settings save fails.
func (s *Store) CommitSettings(srv upstream.Server, guard WriteGuard, settings *Settings, commit func() error) (*Record, error) {
	unlock, err := s.lock(srv)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.RequireApproval(srv); err != nil {
		return nil, err
	}
	if settings == nil {
		r, _, err := s.readGuardedLocked(srv, true, guard)
		if err == nil && commit != nil {
			err = commit()
		}
		return r, err
	}
	r, err := s.updateLocked(srv, true, func(r *Record) error {
		if commit != nil {
			if err := commit(); err != nil {
				return err
			}
		}
		if settings.Secrets != nil {
			r.Secrets = Secrets{Env: maps.Clone(settings.Secrets.Env), Headers: maps.Clone(settings.Secrets.Headers)}
		}
		if settings.ClientSecret != nil {
			r.Client.ID, r.Client.Secret = srv.ClientID, *settings.ClientSecret
		}
		if settings.CallbackPort != nil {
			r.Client.CallbackPort = *settings.CallbackPort
		}
		r.Generation = s.nextGeneration(r.Generation)
		return nil
	}, guard)
	if err != nil {
		return nil, err
	}
	if settings.Secrets != nil && r.Token.Access == "" {
		_ = s.setState(srv, signedIn(r))
	}
	return r, nil
}
