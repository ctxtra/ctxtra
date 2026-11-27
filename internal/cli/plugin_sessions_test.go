package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/agent"
	"github.com/ctxtra/ctxtra/internal/agent/registry"
	"github.com/ctxtra/ctxtra/internal/profile"
	"github.com/ctxtra/ctxtra/internal/setup"
	"github.com/ctxtra/ctxtra/internal/testenv"
)

func TestDoctorReportsPluginsWithoutSessionForwarding(t *testing.T) {
	tested := 0
	for _, client := range testenv.Clients(t) {
		if client.ID != "current" || !slices.Contains([]string{"opencode", "hermes-agent", "dsh"}, client.Agent) {
			continue
		}
		tested++
		t.Run(client.Agent, func(t *testing.T) {
			h := testenv.Home(t)
			t.Setenv("PATH", t.TempDir())
			client.Load(t, h, fakeBin(t, filepath.Join(h, "real", "ctxtra")))
			bin := filepath.Join(h, "bin", "ctxtra")
			running = func() string { return bin }
			binary = func() (string, []string, error) { return bin, nil, nil }
			t.Cleanup(func() { running, binary = setup.Running, setup.Binary })
			if client.Agent == "hermes-agent" {
				testenv.Write(t, filepath.Join(h, ".hermes", "config.yaml"), "plugins:\n  enabled: [ctxtra]\n")
			}
			testenv.Config(t, h, `schema: ctxtra/config/v1
check-releases: false
bindings:
  - {name: iam, profiles: [iam], instructions: [~/iam.md]}
  - {name: obs, profiles: [observability], instructions: [~/obs.md]}
`)
			testenv.Write(t, filepath.Join(h, "iam.md"), "IAM-RULES")
			testenv.Write(t, filepath.Join(h, "obs.md"), "OBS-RULES")
			// Older plugins still get the current selection on each event.
			events := client.Events[len(client.Events)-2:]
			for i, selection := range []string{"iam", "observability"} {
				if err := profile.Store([]string{selection}); err != nil {
					t.Fatal(err)
				}
				fields := map[string]any{"cwd": h, "ctxtra_plugin": client.Protocol}
				if client.Slots > 0 {
					fields["ctxtra_slots"] = client.Slots
				}
				in, _ := json.Marshal(fields)
				var out, errOut strings.Builder
				n := Run([]string{"hook", client.Agent, events[i], agent.SetupVersionArg},
					IO{In: strings.NewReader(string(in)), Out: &out, Err: &errOut}, "test")
				want, absent := "IAM-RULES", "OBS-RULES"
				if i == 1 {
					want, absent = absent, want
				}
				if n != 0 || !strings.Contains(out.String(), want) || strings.Contains(out.String(), absent) {
					t.Fatalf("old plugin: %d %s %s", n, out.String(), errOut.String())
				}
			}
			check := func(wantCode int) {
				t.Helper()
				n, out, errOut := run("doctor", h, "--json")
				var doc struct{ Data struct{ Hooks []hookInfo } }
				if err := json.Unmarshal([]byte(out), &doc); err != nil || n != wantCode {
					t.Fatalf("doctor: %d %v %s %s", n, err, out, errOut)
				}
				idx := slices.IndexFunc(doc.Data.Hooks, func(h hookInfo) bool { return h.Agent == client.Agent })
				if idx < 0 {
					t.Fatal("agent missing from doctor")
				}
				st := doc.Data.Hooks[idx]
				note := "doesn't pass session and parent IDs"
				if st.OK != (wantCode == 0) || strings.Contains(strings.Join(st.Details, "\n"), note) != (wantCode == 1) {
					t.Fatalf("doctor status: %+v", st)
				}
				if wantCode == 1 && !strings.Contains(strings.Join(st.Details, "\n"), "ctxtra setup --agent "+client.Agent) {
					t.Fatalf("missing setup command: %+v", st)
				}
			}
			check(1)
			if n, out, _ := run("doctor", h); n != 1 || !strings.Contains(out, "doesn't pass session and parent IDs") {
				t.Fatalf("doctor text: %d %s", n, out)
			}
			if n, out, errOut := run("setup", "--agent", client.Agent); n != 0 {
				t.Fatalf("setup: %d %s %s", n, out, errOut)
			}
			check(0)
			a, _ := registry.Get(client.Agent)
			path := a.ConfigPath()
			if plugin, ok := a.(interface{ PluginPath() string }); ok {
				path = plugin.PluginPath()
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			v := a.(agent.PluginClient).Protocol()
			testenv.Write(t, path, strings.Replace(string(b), fmt.Sprintf("protocol: %d\n", v), fmt.Sprintf("protocol: %d\n", v+1), 1))
			check(0)
		})
	}
	if tested != 3 {
		t.Fatalf("tested %d older plugins, want 3", tested)
	}
}
