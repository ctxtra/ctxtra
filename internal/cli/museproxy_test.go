package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ctxtra/ctxtra/internal/state"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMuseProxyConfig(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, override, want string
		record, missing      bool
	}{
		{name: "recorded", record: true, want: "saved__echo"},
		{name: "explicit", record: true, override: "override.yaml", want: "override__echo"},
		{name: "missing_saved", record: true, missing: true},
		{name: "old_setup", want: "default__echo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testenv.Home(t)
			write := func(path, name string) {
				t.Helper()
				testenv.Write(t, path, "schema: ctxtra/v1\nmcp-servers:\n  "+name+": {command: ['"+exe+"'], env: {CTXTRA_TEST_MCPSERVER: '1'}}\nbindings:\n  - {name: all, match: {global: true}, mcp-servers: ["+name+"]}\n")
			}
			write(filepath.Join(h, ".config/ctxtra/config.yaml"), "default")
			saved := filepath.Join(h, "saved.yaml")
			if !tc.missing {
				write(saved, "saved")
			}
			override := filepath.Join(h, "override.yaml")
			write(override, "override")
			if tc.record {
				var s state.Setups
				ap := filepath.Join(h, ".config/muse/settings.json")
				s.Install("muse", ap)
				s.SetConfig("muse", ap, saved)
				if err := state.WriteSetups(s); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(exe, "mcp", "proxy", "muse")
			cmd.Dir = h
			cmd.Env = []string{"HOME=" + h, "PATH=" + os.Getenv("PATH"), "CTXTRA_TEST_HOOK=1", "CTXTRA_LOG=" + filepath.Join(h, "proxy.log")}
			if tc.override != "" {
				cmd.Env = append(cmd.Env, "CTXTRA_CONFIG="+override)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			res, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(res.Tools) != 0 {
					t.Fatalf("missing saved config borrowed tools: %+v", res.Tools)
				}
			} else {
				var names []string
				for _, tool := range res.Tools {
					names = append(names, tool.Name)
					if !strings.HasPrefix(tool.Name, strings.TrimSuffix(tc.want, "echo")) {
						t.Errorf("tool from another config: %s", tool.Name)
					}
				}
				if !slices.Contains(names, tc.want) {
					t.Fatalf("tools %v, want %s", names, tc.want)
				}
			}
		})
	}
}
