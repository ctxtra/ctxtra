package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

func (a *app) configCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Change program settings", GroupID: "config", RunE: group,
		Example: "  ctxtra config set check-releases false\n  ctxtra config unset check-releases"}
	set := &cobra.Command{Use: "set KEY VALUE", Short: "Set a program setting", Args: exactly("KEY", "VALUE"),
		Long: `Set check-releases (true or false), max-ctx-chars, session-message.max-skills
or session-message.max-instructions (whole numbers of 0 or more).`,
		Example: "  ctxtra config set session-message.max-skills 0",
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			s := newSources(a.IO)
			d, err := yamlx.ParseEntry([]byte(args[1]))
			if err != nil {
				return code(s.fail("config set", err))
			}
			return code(settingWrite(s, "set", args[0], d.Root))
		})}
	unset := &cobra.Command{Use: "unset KEY", Short: "Remove a setting so its default applies", Args: exactly("KEY"),
		Example: "  ctxtra config unset check-releases",
		RunE: a.run(func(_ *cobra.Command, args []string) error {
			return code(settingWrite(newSources(a.IO), "unset", args[0], nil))
		})}
	c.AddCommand(set, unset)
	return c
}

func settingWrite(s *sources, verb, key string, value *yaml.Node) int {
	if err := s.editEntry(false, func(ed *config.Editor) error { return ed.Setting(key, value) }); err != nil {
		return s.fail("config "+verb, err)
	}
	fmt.Fprintf(s.Out, "%s %s\n", verb, key)
	return 0
}
