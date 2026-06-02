package cmd

import (
	"fmt"
	"sort"

	"skl/internal/library"
	"skl/internal/picker"
	"skl/internal/style"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(bundleUpCmd)
}

var bundleUpCmd = &cobra.Command{
	Use:         "bundle-up [folder...]",
	Annotations: map[string]string{"group": "Library:"},
	Short:       "Make a bundle from an external folder's skills (fzf when no args)",
	Long: `Turn an external folder (library/external/<folder>/) into a loadable bundle
named after the folder, containing every skill inside it. Merges into the
bundle if it already exists. With no args, fzf-pick the folder(s).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		folders, err := externalFolders()
		if err != nil {
			return err
		}
		if len(folders) == 0 {
			return fmt.Errorf("no folders found in library/external/")
		}
		// resolve which folder(s) to bundle up
		chosen := args
		if len(chosen) == 0 {
			chosen, err = pickFolders(folders)
			if err != nil {
				return err
			}
		}
		bundles, err := library.Bundles()
		if err != nil {
			return err
		}
		for _, f := range chosen {
			ids, ok := folders[f]
			if !ok {
				return fmt.Errorf("no folder %q under library/external/", f)
			}
			if err := rejectReservedBundle(f); err != nil {
				return err
			}
			// merge the folder's skills into a bundle named after the folder
			merged := append([]string{}, bundles[f]...)
			merged = append(merged, ids...)
			bundles[f] = merged
			fmt.Printf("%s bundle %q  %s %d skill(s)\n", style.OK("bundled"), f, style.Faint("+"), len(ids))
		}
		if err := library.WriteBundles(bundles); err != nil {
			return err
		}
		if len(chosen) == 1 {
			fmt.Printf("%s run %s to activate\n", style.Faint("→"), style.Cmd("skl load "+chosen[0]))
		}
		return nil
	},
}

// externalFolders maps each external namespace folder to its skill IDs.
func externalFolders() (map[string][]string, error) {
	skills, err := library.Skills()
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, s := range skills {
		if s.External {
			out[s.Repo] = append(out[s.Repo], s.ID)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out, nil
}

func pickFolders(folders map[string][]string) ([]string, error) {
	var items []picker.Item
	for name, ids := range folders {
		items = append(items, picker.Item{ID: name, Display: fmt.Sprintf("%s\t(%d skills)", name, len(ids))})
	}
	chosen, err := picker.Pick(items, picker.Opts{Prompt: "bundle-up > ", Multi: true})
	if err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		return nil, ErrCancelled
	}
	return chosen, nil
}
