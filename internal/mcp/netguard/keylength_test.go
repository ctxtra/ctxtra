package netguard

import (
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// With GODEBUG=fips140=on, a server whose chain has an RSA key under 2048 bits
// is refused, even under a root the user trusts (docs/spec.md, Key lengths).
func TestKeyLengthsFIPS140(t *testing.T) {
	if !testenv.FIPS140(t) {
		return
	}
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
		srv := testenv.TLSServer(t, cert, ok())
		f := filepath.Join(t.TempDir(), "ca.pem")
		os.WriteFile(f, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}), 0o600)
		t.Setenv("SSL_CERT_FILE", f)
		err := get(Policy{Loopback: true}.Client(srv.URL), srv.URL)
		if c.ok && err != nil || !c.ok && (err == nil || !strings.Contains(err.Error(), "FIPS")) {
			t.Errorf("%v: %v", c.keys, err)
		}
	}
}
