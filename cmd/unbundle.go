package cmd

import (
	"fmt"

	"skl/internal/bundle"
	"skl/internal/library"
	"skl/internal/state"
	"skl/internal/style"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(unbundleCmd)
}

var unbundleCmd = &cobra.Command{
	Use:         "unbundle [bundle...]",
	Annotations: map[string]string{"group": "Library:"},
	Short:       "Dissolve a bundle; its skills unload and return to inbox (fzf when no args)",
	Long: `Split a bundle back out: unload any of its skills from ~/.skills/ and remove
the bundle definition, so the skills fall back into the inbox catch-all. The
skill files stay in the library. With no args, fzf-pick the bundle(s).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		bundles, err := library.Bundles()
		if err != nil {
			return err
		}
		// resolve and validate the bundle(s) to dissolve
		chosen := args
		if len(chosen) == 0 {
			chosen, err = pickBundles(bundles, "unbundle > ")
			if err != nil {
				return err
			}
		}
		for _, name := range chosen {
			if err := rejectReservedBundle(name); err != nil {
				return err
			}
			if _, ok := bundles[name]; !ok {
				return fmt.Errorf("bundle %q does not exist", name)
			}
		}

		mgr, err := state.NewManager()
		if err != nil {
			return err
		}
		if err := mgr.Lock(); err != nil {
			return err
		}
		defer mgr.Unlock()
		st, err := mgr.Load()
		if err != nil {
			return err
		}

		for _, name := range chosen {
			// unload any live skills this bundle claims, then drop the definition
			removed := 0
			plan := bundle.PlanUnload(name, st)
			if len(plan.Actions) > 0 {
				removed, _ = applyUnloadPlan(plan, st)
			}
			delete(bundles, name)
			fmt.Printf("%s bundle %q  %s %d unloaded → inbox\n", style.OK("unbundled"), name, style.Faint("-"), removed)
		}
		if err := mgr.Save(st); err != nil {
			return err
		}
		return library.WriteBundles(bundles)
	},
}
