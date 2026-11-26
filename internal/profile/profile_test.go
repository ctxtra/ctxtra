package profile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/testenv"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// store sets the stored selection, or fails the test.
func store(t *testing.T, names ...string) {
	t.Helper()
	if err := Store(names); err != nil {
		t.Fatal(err)
	}
}

func TestActiveProfilesForANewSession(t *testing.T) {
	t.Run("the variable overrides the stored selection", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam")
		t.Setenv(Env, "observability")
		if a := Current(); !slices.Equal(a.Names, []string{"observability"}) || a.Origin != FromEnv || len(a.Errors) > 0 {
			t.Errorf("%+v", a)
		}
	})
	t.Run("an empty variable means no profiles", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam")
		t.Setenv(Env, "")
		if a := Current(); len(a.Names) != 0 || a.Origin != FromEnv || a.String() != "none (from CTXTRA_PROFILES)" {
			t.Errorf("%+v", a)
		}
	})
	t.Run("stored selection", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam", "observability")
		a := Current()
		if !slices.Equal(a.Names, []string{"iam", "observability"}) || a.Origin != FromStored {
			t.Errorf("%+v", a)
		}
		if s := a.String(); s != "iam, observability (from stored selection)" {
			t.Errorf("%q", s)
		}
	})
	t.Run("neither", func(t *testing.T) {
		testenv.Home(t)
		if a := Current(); len(a.Names) != 0 || a.Origin != None || a.Shown() || a.String() != "none" {
			t.Errorf("%+v", a)
		}
	})
}

func TestTheEnvironmentVariablesFormat(t *testing.T) {
	t.Run("several names", func(t *testing.T) {
		testenv.Home(t)
		t.Setenv(Env, " iam, Observability,,iam ")
		if a := Current(); !slices.Equal(a.Names, []string{"iam", "Observability"}) || len(a.Errors) > 0 {
			t.Errorf("%+v", a)
		}
	})
	t.Run("one invalid name", func(t *testing.T) {
		testenv.Home(t)
		t.Setenv(Env, "iam,on call")
		a := Current()
		if !slices.Equal(a.Names, []string{"iam"}) || len(a.Errors) != 1 || a.Errors[0].Err.Mistake != yamlx.BadProfileName {
			t.Fatalf("%+v", a)
		}
		if msg := a.Errors[0].Error(); !strings.HasPrefix(msg, `CTXTRA_PROFILES: "on call" is not a valid profile name: `) ||
			!strings.HasSuffix(msg, "; the entry is left out") {
			t.Errorf("%q", msg)
		}
	})
}

func TestABrokenStoredSelection(t *testing.T) {
	t.Run("unreadable selection in a session", func(t *testing.T) {
		for name, content := range map[string]string{
			"not JSON":      "iam",
			"unknown key":   `{"profiles": ["iam"], "active": true}`,
			"invalid name":  `{"profiles": ["on call"]}`,
			"two values":    `{"profiles": ["iam"]} {}`,
			"not a list":    `{"profiles": "iam"}`,
			"not an object": `["iam"]`,
		} {
			t.Run(name, func(t *testing.T) {
				h := testenv.Home(t)
				testenv.Write(t, filepath.Join(h, ".local/state/ctxtra/profiles.json"), content)
				a := Current()
				if len(a.Names) != 0 || len(a.Errors) != 1 || a.Errors[0].Err.Mistake != yamlx.SelectionUnreadable || !a.Shown() {
					t.Fatalf("%+v", a)
				}
				if msg := a.Errors[0].Error(); !strings.HasPrefix(msg, "~/.local/state/ctxtra/profiles.json is not a valid profile selection: ") ||
					!strings.HasSuffix(msg, "; nothing in the file is used") {
					t.Errorf("%q", msg)
				}
			})
		}
	})
	t.Run("set and clear don't overwrite it", func(t *testing.T) {
		h := testenv.Home(t)
		path := filepath.Join(h, ".local/state/ctxtra/profiles.json")
		testenv.Write(t, path, "iam")
		for _, names := range [][]string{{"observability"}, nil} {
			if err := Store(names); err == nil || !strings.Contains(err.Error(), "~/.local/state/ctxtra/profiles.json") {
				t.Errorf("%v: %v", names, err)
			}
		}
		if b, _ := os.ReadFile(path); string(b) != "iam" {
			t.Errorf("changed: %q", b)
		}
	})
}

func TestStoringTheSelection(t *testing.T) {
	t.Run("set replaces", func(t *testing.T) {
		h := testenv.Home(t)
		store(t, "iam")
		store(t, "observability", "oncall", "observability")
		if names, ok, err := Stored(); !ok || err != nil || !slices.Equal(names, []string{"observability", "oncall"}) {
			t.Errorf("%v %v %v", names, ok, err)
		}
		st, err := os.Stat(filepath.Join(h, ".local/state/ctxtra/profiles.json"))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%v %v", st.Mode(), err)
		}
	})
	t.Run("clear", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam")
		store(t)
		if a := Current(); len(a.Names) != 0 || a.Origin != FromStored || a.Shown() {
			t.Errorf("%+v", a)
		}
	})
	t.Run("an invalid name changes nothing", func(t *testing.T) {
		testenv.Home(t)
		store(t, "iam")
		if err := Store([]string{"observability", "on call"}); err == nil {
			t.Error("no error")
		}
		if names, _, _ := Stored(); !slices.Equal(names, []string{"iam"}) {
			t.Errorf("%v", names)
		}
	})
}

func TestPreviewAnotherProfile(t *testing.T) {
	testenv.Home(t)
	store(t, "iam")
	a, err := Given([]string{"observability", "oncall", "observability"})
	if err != nil || !slices.Equal(a.Names, []string{"observability", "oncall"}) || a.Origin != FromFlag {
		t.Errorf("%+v %v", a, err)
	}
	if a.String() != "observability, oncall (from --profile)" {
		t.Errorf("%q", a.String())
	}
	if _, err := Given([]string{"on call"}); err == nil || !strings.HasPrefix(err.Error(), `--profile "on call": `) {
		t.Errorf("%v", err)
	}
	if names, _, _ := Stored(); !slices.Equal(names, []string{"iam"}) {
		t.Errorf("stored selection changed: %v", names)
	}
}

// A name that a binding uses only after ! counts as used.
func TestUsed(t *testing.T) {
	c := config.Parse([]byte(`schema: ctxtra/config/v1
bindings:
  - {name: a, profiles: [iam, observability], instructions: [a.md]}
  - {name: b, profiles: ['!oncall', iam], instructions: [b.md]}
  - {name: c, profiles: ['on call'], instructions: [c.md]}
`), config.Options{})
	got := Used(c)
	want := map[string][]string{"iam": {"a", "b"}, "observability": {"a"}, "oncall": {"b"}}
	if len(got) != len(want) {
		t.Errorf("%v", got)
	}
	for k, v := range want {
		if !slices.Equal(got[k], v) {
			t.Errorf("%s: %v", k, got[k])
		}
	}
}
