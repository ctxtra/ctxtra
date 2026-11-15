package proxy

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

type failedGeneration struct {
	Tokens
	err error
}

func (f failedGeneration) CredentialGeneration(upstream.Server) (int64, error) {
	return 0, f.err
}

type generationRequestBody struct {
	io.Reader
	closed bool
}

func (b *generationRequestBody) Close() error { b.closed = true; return nil }

func TestGenerationFailureClosesRequestBeforeReadingCredentials(t *testing.T) {
	failure := errors.New("credential read failed")
	body := &generationRequestBody{Reader: strings.NewReader("request")}
	req, err := http.NewRequest(http.MethodPost, "https://example.com/mcp", body)
	if err != nil {
		t.Fatal(err)
	}
	transport := &authTransport{tokens: failedGeneration{err: failure}}
	if _, err := transport.RoundTrip(req); !errors.Is(err, failure) || !body.closed {
		t.Fatalf("failed generation read: %v, body closed: %v", err, body.closed)
	}
}
