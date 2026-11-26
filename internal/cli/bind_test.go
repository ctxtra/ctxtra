package cli

import (
	"strings"
	"testing"

	"github.com/ctxtra/ctxtra/internal/config"
)

func TestFindSource(t *testing.T) {
	c := config.Parse([]byte(config.NewFile+`sources:
  acme-iam:
    repo: git@github.com:acme/iam-ctx.git
    branch: main
  agent-context:
    repo: git@github.com:acme/agent-context.git
    branch: main
    subdir: teams/iam
  agent-context-obs:
    repo: git@github.com:acme/agent-context.git
    branch: main
    subdir: teams/obs/
  cat:
    repo: https://github.com/org/catalog
    branch: main
`), config.Options{})
	tests := []struct {
		name, arg, want, err string
	}{
		{name: "URLs compared by repository", arg: "https://github.com/acme/iam-ctx", want: "acme-iam"},
		{name: "another case and an ssh URL", arg: "ssh://git@GitHub.com/acme/IAM-ctx.git", want: "acme-iam"},
		{name: "a catalog without a manifest", arg: "https://github.com/org/catalog", want: "cat"},
		{name: "a source name", arg: "cat", want: "cat"},
		{name: "two teams' folders in one repository", arg: "git@github.com:acme/agent-context.git//teams/obs", want: "agent-context-obs"},
		{name: "a folder in an https URL", arg: "https://github.com/acme/agent-context//teams/iam", want: "agent-context"},
		{name: "one repository, two sources", arg: "git@github.com:acme/agent-context.git",
			err: "the sources agent-context, agent-context-obs all have git@github.com:acme/agent-context.git"},
		{name: "a folder no source has", arg: "git@github.com:acme/agent-context.git//teams/billing",
			err: "add it first: ctxtra source add --repo git@github.com:acme/agent-context.git --subdir teams/billing"},
		{name: "the source isn't added", arg: "https://github.com/acme/other", err: "add it first: ctxtra source add --repo https://github.com/acme/other"},
		{name: "an unknown name", arg: "iam", err: "the config has no source iam"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findSource(c, tt.arg)
			switch {
			case tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)):
				t.Errorf("err %v, want %q", err, tt.err)
			case tt.err == "" && (err != nil || got != tt.want):
				t.Errorf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestAppliesIn(t *testing.T) {
	tests := []struct{ binding, want string }{
		{"skills: [a]", "every session"},
		{"profiles: [iam]\n    skills: [a]", "every session with iam active"},
		{"profiles: ['!oncall']\n    skills: [a]", "every session without oncall active"},
		{"agents: [claude-code]\n    skills: [a]", "every session of claude-code"},
		{"agents: ['!cursor']\n    profiles: [iam, obs, '!oncall']\n    skills: [a]",
			"every session of every agent but cursor, with iam or obs active and without oncall"},
		{"location: {dirs: [~/dev/**]}\n    agents: [codex]\n    profiles: [go]\n    skills: [a]",
			"sessions of codex where its location matches, with go active"},
	}
	for _, tt := range tests {
		c := config.Parse([]byte(config.NewFile+"bindings:\n  - name: b\n    "+tt.binding+"\n"), config.Options{Agents: []string{"claude-code", "codex", "cursor"}})
		if got := appliesIn(c.Binding("b")); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.binding, got, tt.want)
		}
	}
}
