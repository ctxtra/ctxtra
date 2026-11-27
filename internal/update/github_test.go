package update

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// gitHubURLs are GitHub's release addresses, also as other spellings of
// the same host.
var gitHubURLs = []string{releaseURL, releasesURL,
	strings.Replace(releaseURL, "api.github.com", "API.GITHUB.COM", 1),
	strings.Replace(releaseURL, "api.github.com", "api.github.com.", 1)}

// A test binary never asks GitHub: the release check fails at once with
// GitHub's own addresses, before any request.
func TestTestsDontAskGitHub(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		t.Errorf("a request to %s", r.URL)
		return nil, errors.New("no network in tests")
	})
	for _, url := range gitHubURLs {
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
	for _, to := range gitHubURLs {
		local := 0
		http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Hostname() != "releases.test" {
				t.Errorf("a request to %s", r.URL)
				return nil, errors.New("no network in tests")
			}
			local++
			h := http.Header{"Location": {to}}
			return &http.Response{StatusCode: http.StatusFound, Header: h, Body: http.NoBody, Request: r}, nil
		})
		if _, err := getJSON(context.Background(), "https://releases.test/latest", "v1.0.0", 1<<10, new(any)); !errors.Is(err, errTestAsksGitHub) {
			t.Errorf("redirect to %s: %v", to, err)
		}
		if local != 1 {
			t.Errorf("redirect to %s: %d local requests", to, local)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
