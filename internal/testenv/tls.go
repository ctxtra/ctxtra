package testenv

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/fips140"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// FIPS140 reports whether the test runs with GODEBUG=fips140=on. When it
// doesn't, it runs the test again in a new process that does, fails the test
// when that run fails, and reports false: the caller returns. Only for
// top-level tests.
func FIPS140(t *testing.T) bool {
	t.Helper()
	if fips140.Enabled() {
		return true
	}
	args := []string{"-test.run=^" + t.Name() + "$", "-test.count=1", "-test.v"}
	// The child writes its coverage where go test collects the parent's.
	if f := flag.Lookup("test.gocoverdir"); f != nil && f.Value.String() != "" {
		args = append(args, "-test.gocoverdir="+f.Value.String())
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "GODEBUG=fips140=on")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: "+t.Name()) {
		t.Fatalf("with GODEBUG=fips140=on: %v\n%s", err, out)
	}
	return false
}

// Key is the kind of a certificate's key: "rsa1024", "rsa2048" or "p256".
type Key string

func (k Key) generate(t *testing.T) crypto.Signer {
	t.Helper()
	var (
		key crypto.Signer
		err error
	)
	switch k {
	case "rsa1024":
		key, err = rsa.GenerateKey(rand.Reader, 1024)
	case "rsa2048":
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	case "p256":
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	default:
		t.Fatalf("unknown key %q", k)
	}
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// Chain is a certificate chain for 127.0.0.1 under a new P-256 root. keys
// are the keys of the certificates below the root, the server's first and
// then its intermediates. It returns the root and the server's certificate
// with its intermediates.
func Chain(t *testing.T, keys ...Key) (*x509.Certificate, tls.Certificate) {
	t.Helper()
	if len(keys) == 0 {
		t.Fatal("no keys")
	}
	issue := func(tmpl, parent *x509.Certificate, pub crypto.PublicKey, signer crypto.Signer) *x509.Certificate {
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
		if err != nil {
			t.Fatal(err)
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	n := int64(0)
	ca := func(name string) *x509.Certificate {
		n++
		return &x509.Certificate{SerialNumber: big.NewInt(n), Subject: pkix.Name{CommonName: name},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	}
	rootKey := Key("p256").generate(t)
	root := issue(ca("test root"), ca("test root"), rootKey.Public(), rootKey)
	parent, parentKey := root, rootKey
	var inter [][]byte
	for i := len(keys) - 1; i > 0; i-- {
		key := keys[i].generate(t)
		c := issue(ca("test intermediate"), parent, key.Public(), parentKey)
		inter = append([][]byte{c.Raw}, inter...)
		parent, parentKey = c, key
	}
	key := keys[0].generate(t)
	n++
	leaf := issue(&x509.Certificate{SerialNumber: big.NewInt(n), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, parent, key.Public(), parentKey)
	return root, tls.Certificate{Certificate: append([][]byte{leaf.Raw}, inter...), PrivateKey: key, Leaf: leaf}
}

// TLSServer is an HTTPS server on 127.0.0.1 with cert.
func TLSServer(t *testing.T, cert tls.Certificate, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}
