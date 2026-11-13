package creds

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

// ErrSignInChanged is a sign-in result that came too late: another
// sign-in or a removal of the server finished while it ran.
var ErrSignInChanged = errors.New("another sign-in or a removal of the server finished meanwhile; nothing was saved")

// Generation is the current sign-in generation, zero for an untouched
// identity. Durable markers retain it across deletion. A sign-in captures
// it before prompting and gives it to SaveSignIn.
func (s *Store) Generation(srv upstream.Server) (int64, error) {
	_, generation, err := s.Inspect(srv)
	if errors.Is(err, errUnreadable) {
		err = nil
	}
	return generation, err
}

// SignInChange is staged until authorization succeeds. Replace permits
// replacing the conflicting record whose generation the user approved.
type SignInChange struct {
	Replace bool
	Secrets *Secrets
}

// SaveSignIn commits the authorization and staged secrets in one write.
// It refuses a changed generation or a conflicting binding without consent.
// The caller made sure the store has a key.
func (s *Store) SaveSignIn(srv upstream.Server, gen int64, issuer string, c Client, t Token, changes ...SignInChange) (*Record, error) {
	unlock, err := s.lock(srv)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.RequireApproval(srv); err != nil {
		return nil, err
	}
	var change SignInChange
	if len(changes) > 0 {
		change = changes[0]
	}
	r, err := s.updateLocked(srv, true, func(r *Record) error {
		r.Issuer, r.Client, r.Token = issuer, c, t
		if change.Secrets != nil {
			r.Secrets = Secrets{Env: maps.Clone(change.Secrets.Env), Headers: maps.Clone(change.Secrets.Headers)}
		}
		r.Generation = s.nextGeneration(r.Generation)
		return nil
	}, WriteGuard{ExpectedGeneration: &gen, Replace: change.Replace})
	if err != nil {
		return nil, err
	}
	_ = s.setState(srv, func(st *State) {
		signedIn(r)(st)
		st.Scopes = nil
	})
	return r, nil
}

// SetSecrets replaces srv's secret values under its lock. The caller made
// sure the store has a key.
// For a server without OAuth, its state then says it is signed in.
func (s *Store) SetSecrets(srv upstream.Server, env, headers map[string]string) error {
	_, err := s.SaveSettings(srv, WriteGuard{}, Settings{Secrets: &Secrets{Env: env, Headers: headers}})
	return err
}

// SetClientSecret stores the secret of srv's pre-registered client under
// its lock. The caller made sure the store has a key.
func (s *Store) SetClientSecret(srv upstream.Server, secret string) error {
	_, err := s.SaveSettings(srv, WriteGuard{}, Settings{ClientSecret: &secret})
	return err
}

// Secrets are srv's secret values: the variables of its SecretEnv for a
// local server, the headers of its SecretHeaders for a remote one. Every
// name must have a value, and the record must have been made for srv as
// it is defined now; otherwise there are none.
func (s *Store) Secrets(srv upstream.Server) (env, headers map[string]string, err error) {
	if len(srv.SecretEnv) == 0 && len(srv.SecretHeaders) == 0 {
		return nil, nil, nil
	}
	r, err := s.loadShared(srv)
	switch {
	case errors.Is(err, errUnreadable):
		return nil, nil, &LoginError{Name: srv.ID, Reason: "its secret values can't be read"}
	case err != nil:
		return nil, nil, err
	case r == nil:
		return nil, nil, &LoginError{Name: srv.ID, Reason: "its secret values are not set"}
	case !r.Matches(srv):
		return nil, nil, &LoginError{Name: srv.ID, Reason: "its secret values were set for another address or command"}
	}
	env, headers = map[string]string{}, map[string]string{}
	var missing []string
	for _, k := range srv.SecretEnv {
		v, ok := r.Secrets.Env[k]
		if !ok {
			missing = append(missing, k)
		}
		env[k] = v
	}
	for _, k := range srv.SecretHeaders {
		v, ok := lookupHeader(r.Secrets.Headers, k)
		if !ok {
			missing = append(missing, k)
		}
		headers[k] = v
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, nil, &LoginError{Name: srv.ID, Reason: fmt.Sprintf("no value is set for %s", strings.Join(missing, ", "))}
	}
	return env, headers, nil
}

// lookupHeader finds a header's value; header names ignore case.
func lookupHeader(h map[string]string, name string) (string, bool) {
	for k, v := range h {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
}

// nextGeneration is the generation of a sign-in after old: the time in
// nanoseconds, or old+1 when the clock is behind.
func (s *Store) nextGeneration(old int64) int64 {
	return max(s.now().UnixNano(), old+1)
}

// Logout deletes srv's tokens and secret values, the client secret too, on
// this machine, under its lock. The client registration stays, with a
// chosen callback port.
// The provider isn't told: the grant stays valid there.
func (s *Store) Logout(srv upstream.Server) error {
	unlock, err := s.lock(srv)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.checkVersion(srv.ID, srv.ID); err != nil {
		return err
	}
	existing, err := s.load(srv.ID)
	if err != nil && !errors.Is(err, errUnreadable) {
		return err
	}
	// Logout concerns the shared identity, not the selected declaration.
	// Keep its binding so clearing secrets retains registration and callback settings.
	if existing != nil {
		srv.Origin, srv.URL, srv.Command = existing.Origin, existing.URL, existing.Command
	}
	// A new generation: a sign-in that started before the logout can't
	// save.
	// Also without a record: an empty one with a new generation, so a
	// first sign-in that is still waiting for the browser can't save.
	_, err = s.updateLocked(srv, true, func(r *Record) error {
		r.Token, r.Secrets, r.Client.Secret = Token{}, Secrets{}, ""
		r.Generation = s.nextGeneration(r.Generation)
		return nil
	})
	if errors.Is(err, ErrNoKey) {
		// No key, so no sign-in has started: nothing to cancel.
		err = nil
	}
	if err != nil {
		return err
	}
	return s.setState(srv, func(st *State) {
		st.SignedIn, st.NeedsLogin, st.Reason = false, false, "signed out"
	})
}

// SetCallbackPort stores the redirect listener's port for srv, under its
// lock.
func (s *Store) SetCallbackPort(srv upstream.Server, port int) error {
	_, err := s.SaveSettings(srv, WriteGuard{}, Settings{CallbackPort: &port})
	return err
}
