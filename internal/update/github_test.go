package update

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// A test binary never asks GitHub: the release check fails at once with
// GitHub's own addresses, before any request.
func TestTestsDontAskGitHub(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		t.Errorf("a request to %s", r.URL)
		return nil, errors.New("no network in tests")
	})
	for _, url := range []string{releaseURL, releasesURL} {
		if _, err := getJSON(context.Background(), url, "v1.0.0", 1<<10, new(any)); !errors.Is(err, errTestAsksGitHub) {
			t.Errorf("%s: %v", url, err)
		}
	}
}

// A local answer that redirects to GitHub is refused too, before the
// redirected request.
func TestTestsDontFollowRedirectsToGitHub(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	local := 0
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "releases.test" {
			t.Errorf("a request to %s", r.URL)
			return nil, errors.New("no network in tests")
		}
		local++
		h := http.Header{"Location": {releaseURL}}
		return &http.Response{StatusCode: http.StatusFound, Header: h, Body: http.NoBody, Request: r}, nil
	})
	if _, err := getJSON(context.Background(), "https://releases.test/latest", "v1.0.0", 1<<10, new(any)); !errors.Is(err, errTestAsksGitHub) {
		t.Errorf("redirect: %v", err)
	}
	if local != 1 {
		t.Errorf("%d local requests", local)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
