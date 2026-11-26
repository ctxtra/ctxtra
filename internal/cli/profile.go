package cli

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/profile"
)

func (a *app) profileCmd() *cobra.Command {
	c := &cobra.Command{
		Use: "profile", Short: "Choose the profiles for new sessions on this machine",
		Long: `Store a selection for new sessions without changing the config.
CTXTRA_PROFILES overrides it for one launch. Running sessions keep their profiles.`,
		Example: "  ctxtra profile set iam oncall\n  ctxtra profile list\n  ctxtra profile clear",
		GroupID: "config", RunE: group, DisableFlagsInUseLine: true,
	}
	set := &cobra.Command{
		Use: "set NAME...", Short: "Replace the stored selection",
		Example: "  ctxtra profile set iam oncall", Args: cobra.MinimumNArgs(1),
		RunE: a.run(func(_ *cobra.Command, names []string) error {
			return code(a.storeProfiles(names))
		}),
	}
	clear := &cobra.Command{
		Use: "clear", Short: "Empty the stored selection", Example: "  ctxtra profile clear", Args: noArgs,
		RunE: a.run(func(*cobra.Command, []string) error { return code(a.storeProfiles(nil)) }),
	}
	var asJSON bool
	list := &cobra.Command{
		Use: "list", Short: "List profiles, their bindings and the active selection",
		Example: "  ctxtra profile list\n  ctxtra profile list --json", Args: noArgs,
		RunE: a.run(func(*cobra.Command, []string) error { return code(a.listProfiles(asJSON)) }),
	}
	list.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	c.AddCommand(set, clear, list)
	return c
}

func (a *app) storeProfiles(names []string) int {
	verb := "set"
	if len(names) == 0 {
		verb = "clear"
	}
	fail := func(err error) int {
		fmt.Fprintf(a.Err, "ctxtra profile %s: %v\n", verb, err)
		return 1
	}
	used := profile.Used(loadEnv().Config)
	for _, name := range names {
		if err := config.CheckName(name); err != nil {
			return fail(fmt.Errorf("%q is not a valid profile name: %w", name, err))
		}
		if _, ok := used[name]; !ok {
			known := strings.Join(slices.Sorted(maps.Keys(used)), ", ")
			if known == "" {
				known = "none"
			}
			return fail(fmt.Errorf("no binding uses %q; profiles the config uses: %s; selection unchanged", name, known))
		}
	}
	if err := profile.Store(names); err != nil {
		return fail(err)
	}
	active, _ := profile.Given(names)
	active.Origin = profile.FromStored
	fmt.Fprintf(a.Out, "stored profiles: %s\n", active)
	return 0
}

type profileInfo struct {
	Name     string   `json:"name"`
	Active   bool     `json:"active"`
	Bindings []string `json:"bindings"`
}

func (a *app) listProfiles(asJSON bool) int {
	env := loadEnv()
	active := profile.Current()
	used := profile.Used(env.Config)
	items := []profileInfo{}
	for _, name := range slices.Sorted(maps.Keys(used)) {
		items = append(items, profileInfo{name, slices.Contains(active.Names, name), used[name]})
	}
	errs := append(errorTexts(env.Errors), errorTexts(active.Errors)...)
	for _, name := range active.Names {
		if _, ok := used[name]; !ok {
			errs = append(errs, fmt.Sprintf("profile %s: no binding uses it; check the name for a typo", name))
		}
	}
	if asJSON {
		writeJSON(a.Out, map[string]any{"profiles": profilesInfo(active), "items": items}, diagnostics("error", errs))
	} else {
		fmt.Fprintf(a.Out, "profiles: %s\n", active)
		for _, item := range items {
			state := "inactive"
			if item.Active {
				state = "active"
			}
			fmt.Fprintf(a.Out, "%s (%s): %s\n", item.Name, state, strings.Join(item.Bindings, ", "))
		}
		for _, err := range errs {
			fmt.Fprintf(a.Err, "error: %s\n", err)
		}
	}
	if len(errs) > 0 {
		return 1
	}
	return 0
}
