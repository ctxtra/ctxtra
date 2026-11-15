package creds

import (
	"testing"

	"github.com/ctxtra/ctxtra/internal/testenv"
)

// GODEBUG=fips140=on keeps the credentials readable: their encryption
// isn't a FIPS algorithm, which only fips140=only refuses (docs/spec.md,
// Key lengths).
func TestRoundTripFIPS140(t *testing.T) {
	if testenv.FIPS140(t) {
		TestRoundTrip(t)
	}
}
