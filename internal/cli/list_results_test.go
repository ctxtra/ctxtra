package cli

import (
	"encoding/json"
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"strings"
	"testing"
)

func TestEveryListReportsMistakes(t *testing.T) {
	h := testenv.Home(t)
	testenv.Config(t, h, config.NewFile+"sources:\n  broken: {repo: https://example.com/repo, dir: /tmp/no}\n")
	for _, c := range commands(tree()) {
		if c.Name() != "list" {
			continue
		}
		for _, asJSON := range []bool{false, true} {
			args := strings.Fields(c.CommandPath())[1:]
			if asJSON {
				args = append(args, "--json")
			}
			n, out, e := run(args...)
			if n != 1 {
				t.Errorf("%v: %d, want 1\n%s%s", args, n, out, e)
			}
			if asJSON && (!json.Valid([]byte(out)) || !strings.Contains(out, `"severity": "error"`)) {
				t.Errorf("%v: missing JSON diagnostic: %s", args, out)
			}
		}
	}
}
