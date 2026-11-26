package config

import (
	"bytes"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

func TestProgramSettingsFromTheCommandLine(t *testing.T) {
	for _, tt := range []struct{ name, before, key, value, want string }{
		{"turning off release checks", NewFile, "check-releases", "false", NewFile + "check-releases: false\n"},
		{"keep a replaced key's comment", NewFile + "check-releases: true # keep\n# end\n", "check-releases", "false", NewFile + "check-releases: false # keep\n# end\n"},
		{"keep other settings byte for byte", NewFile + "session-message:\n  max-skills: 3 # first\n  # preserve\n  max-instructions: 9 # second\n", "session-message.max-skills", "0", NewFile + "session-message:\n  max-skills: 0 # first\n  # preserve\n  max-instructions: 9 # second\n"},
		{"unset brings back the default", NewFile + "session-message:\n  max-skills: 3\n  max-instructions: 9\n", "session-message.max-skills", "", NewFile + "session-message:\n  max-instructions: 9\n"},
		{"unset the last nested setting", NewFile + "session-message:\n  max-skills: 3\n", "session-message.max-skills", "", NewFile},
		{"unset a missing setting", NewFile, "check-releases", "", NewFile},
		{"same value is unchanged", NewFile + "max-ctx-chars: 004 # keep\n", "max-ctx-chars", "004", NewFile + "max-ctx-chars: 004 # keep\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ed, err := NewEditor([]byte(tt.before))
			if err != nil {
				t.Fatal(err)
			}
			var value *yaml.Node
			if tt.value != "" {
				d, err := yamlx.ParseEntry([]byte(tt.value))
				if err != nil {
					t.Fatal(err)
				}
				value = d.Root
			}
			if err := ed.Setting(tt.key, value); err != nil {
				t.Fatal(err)
			}
			if string(ed.Bytes()) != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", ed.Bytes(), tt.want)
			}
			if err := ed.Check(Options{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSettingsRefuseAMistakeWithoutChangingText(t *testing.T) {
	for _, tt := range []struct{ name, before, key, value string }{
		{"unknown key", NewFile, "sources", "1"},
		{"wrong type", NewFile, "check-releases", "0"},
		{"text instead of a number", NewFile, "max-ctx-chars", "'1.10'"},
		{"negative number", NewFile, "session-message.max-skills", "-1"},
		{"number too large", NewFile, "max-ctx-chars", "999999999999999999999"},
		{"a flow root", "{schema: ctxtra/config/v1, check-releases: true}\n", "check-releases", "false"},
		{"a flow section", NewFile + "session-message: {max-skills: 3}\n", "session-message.max-skills", "0"},
		{"duplicate key", NewFile + "check-releases: true\ncheck-releases: true\n", "check-releases", "false"},
		{"duplicate nested key", NewFile + "session-message:\n  max-skills: 1\n  max-skills: 2\n", "session-message.max-skills", "0"},
		{"oversized result", NewFile + "#" + strings.Repeat("x", MaxFileBytes-len(NewFile)-2) + "\n", "check-releases", "false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ed, err := NewEditor([]byte(tt.before))
			if err != nil {
				t.Fatal(err)
			}
			d, perr := yamlx.ParseEntry([]byte(tt.value))
			if perr != nil {
				t.Fatal(perr)
			}
			if err := ed.Setting(tt.key, d.Root); err == nil {
				t.Fatal("accepted the mistake")
			}
			if !bytes.Equal(ed.Bytes(), []byte(tt.before)) {
				t.Fatal("changed the text")
			}
		})
	}
}
