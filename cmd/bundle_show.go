package cmd

import (
	"fmt"

	"skl/internal/library"
	"skl/internal/state"
	"skl/internal/style"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

func init() {
	bundleCmd.AddCommand(bundleShowCmd)
}

var bundleShowCmd = &cobra.Command{
	Use:   "show [name]",
	Short: "Show the skills in a bundle (fzf-picks when no name)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return renderBundle(args)
	},
}

// renderBundle prints one or more bundles' skills with loaded status and source.
// With no name it fzf-picks; the synthetic `inbox` bundle is supported.
func renderBundle(args []string) error {
	bundles, err := library.Bundles()
	if err != nil {
		return err
	}
	skills, err := library.Skills()
	if err != nil {
		return err
	}
	mgr, err := state.NewManager()
	if err != nil {
		return err
	}
	st, err := mgr.Load()
	if err != nil {
		return err
	}

	// resolve which bundle(s) to show
	chosen := args
	if len(chosen) == 0 {
		chosen, err = pickBundles(bundles, "show > ")
		if err != nil {
			return err
		}
	}

	byID := make(map[string]library.Skill, len(skills))
	for _, s := range skills {
		byID[s.ID] = s
	}
	for _, name := range chosen {
		ids, ok := bundles[name]
		if !ok {
			return fmt.Errorf("bundle %q not found", name)
		}
		printBundleSkills(name, ids, byID, st)
	}
	return nil
}

func printBundleSkills(name string, ids []string, byID map[string]library.Skill, st *state.State) {
	loaded := style.Faint("—")
	if isBundleLoaded(name, st) {
		loaded = style.OK("loaded")
	}
	fmt.Printf("%s %s  %s\n", style.Header("Bundle:"), name, loaded)
	if len(ids) == 0 {
		fmt.Println(style.Faint("  (empty)"))
		return
	}

	var rows [][]string
	for _, id := range ids {
		mark := style.Faint("—")
		if _, ok := st.Loaded[id]; ok {
			mark = style.OK("loaded")
		}
		src := style.Faint("local")
		if s, ok := byID[id]; ok && s.External {
			src = style.Faint("ext: " + s.Repo)
		} else if !ok {
			src = style.Faint("missing")
		}
		rows = append(rows, []string{id, mark, src})
	}
	t := table.New().
		Border(lipgloss.HiddenBorder()).
		Headers("SKILL", "STATUS", "SOURCE").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle().PaddingRight(2)
			if row == table.HeaderRow {
				return s.Bold(true).Faint(true)
			}
			return s
		})
	fmt.Println(t)
}

func isBundleLoaded(name string, st *state.State) bool {
	for _, b := range st.LoadedBundles() {
		if b == name {
			return true
		}
	}
	return false
}
