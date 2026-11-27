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

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
