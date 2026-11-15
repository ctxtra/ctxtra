package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ctxtra/ctxtra/internal/mcp/creds"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
)

func TestUnapprovedServerNeverStarts(t *testing.T) {
	var requests atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer remote.Close()
	store := &creds.Store{Dir: t.TempDir()}
	marker := filepath.Join(t.TempDir(), "started")
	for _, srv := range []upstream.Server{
		{ID: "work/web", Name: "web", Pinned: true, URL: remote.URL},
		{ID: "work/local", Name: "local", Pinned: true, Command: []string{"sh", "-c", "touch \"$MARKER\""}, Env: map[string]string{"MARKER": marker}},
	} {
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		d := Dialer{Approval: store.RequireApproval}
		var needed *creds.ApprovalError
		if _, err := d.Dial(context.Background(), srv, t.TempDir(), client); !errors.As(err, &needed) {
			t.Fatalf("unapproved %s: %v", srv.ID, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("an unauthenticated request reached the server before approval")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("local command ran before approval")
	}
}
