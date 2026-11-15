package update

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// With GODEBUG=fips140=on, the release check refuses a chain with an RSA key
// under 2048 bits (docs/spec.md, Key lengths).
func TestKeyLengthsFIPS140(t *testing.T) {
	if !testenv.FIPS140(t) {
		return
	}
	testenv.Home(t)
	for _, c := range []struct {
		keys []testenv.Key
		ok   bool
	}{
		{[]testenv.Key{"rsa2048"}, true},
		{[]testenv.Key{"p256"}, true},
		{[]testenv.Key{"rsa1024"}, false},
		{[]testenv.Key{"rsa2048", "rsa1024"}, false},
	} {
		root, cert := testenv.Chain(t, c.keys...)
		srv := testenv.TLSServer(t, cert, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"tag_name": "v0.3.0"}`)
		}))
		old, url := http.DefaultTransport, releaseURL
		// The release check uses the default transport: give it the test root.
		tr := old.(*http.Transport).Clone()
		pool := x509.NewCertPool()
		pool.AddCert(root)
		tr.TLSClientConfig = &tls.Config{RootCAs: pool}
		http.DefaultTransport, releaseURL = tr, srv.URL
		_, err := latestRelease(context.Background(), "v0.2.3")
		http.DefaultTransport, releaseURL = old, url
		if c.ok && err != nil || !c.ok && (err == nil || !strings.Contains(err.Error(), "FIPS")) {
			t.Errorf("%v: %v", c.keys, err)
		}
	}
}
