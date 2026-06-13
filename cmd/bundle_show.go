package cmd

import (
	"fmt"

	"skl/internal/bundle"
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
	bundleEntries, err := library.BundleEntries()
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

	for _, name := range chosen {
		entries, ok := bundleEntries[name]
		if !ok {
			return fmt.Errorf("bundle %q not found", name)
		}
		plan, err := bundle.PlanLoadEntries(name, entries, skills, st)
		if err != nil {
			return err
		}
		printBundleSkills(name, plan.Actions, st)
	}
	return nil
}

func printBundleSkills(name string, actions []bundle.LoadAction, st *state.State) {
	loaded := style.Faint("—")
	if isBundleLoaded(name, st) {
		loaded = style.OK("loaded")
	}
	fmt.Printf("%s %s  %s\n", style.Header("Bundle:"), name, loaded)
	if len(actions) == 0 {
		fmt.Println(style.Faint("  (empty)"))
		return
	}

	rows := bundleSkillRows(actions, st)
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

func bundleSkillRows(actions []bundle.LoadAction, st *state.State) [][]string {
	rows := make([][]string, 0, len(actions))
	loadedSources := loadedSourcePaths(st)
	for _, action := range actions {
		s := action.Skill
		mark := style.Faint("—")
		if skillLoaded(s, st, loadedSources) {
			mark = style.OK("loaded")
		}
		src := style.Faint("local")
		if s.External {
			src = style.Faint("pack: " + s.Repo)
		}
		rows = append(rows, []string{s.ID, mark, src})
	}
	return rows
}

func isBundleLoaded(name string, st *state.State) bool {
	for _, b := range st.LoadedBundles() {
		if b == name {
			return true
		}
	}
	return false
}
