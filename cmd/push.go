package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"skl/internal/library"
	"skl/internal/live"
	"skl/internal/style"

	"github.com/spf13/cobra"
)

func init() {
	pushCmd.Flags().BoolP("yes", "y", false, "Skip overwrite confirmation")
	rootCmd.AddCommand(pushCmd)
}

var pushCmd = &cobra.Command{
	Use:         "push <skill>",
	Annotations: map[string]string{"group": "Library:"},
	Short:       "Copy a live ~/.skills/<skill> back into the library (capture edits)",
	Args:        cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		dirName := args[0]

		liveRoot, err := live.LocateSkill(dirName)
		if err != nil {
			return err
		}
		if liveRoot == "" {
			return fmt.Errorf("%s is not loaded in any live dir", dirName)
		}
		src := filepath.Join(liveRoot, dirName)
		if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
			return fmt.Errorf("%s has no SKILL.md", src)
		}

		skillsDir, _ := library.SkillsPath()
		dst := filepath.Join(skillsDir, dirName)

		if _, err := os.Stat(dst); err == nil {
			if !yes && !confirm(fmt.Sprintf("Overwrite library/skills/%s?", dirName)) {
				return ErrCancelled
			}
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("removing existing library copy: %w", err)
			}
		}

		if err := copyDir(src, dst); err != nil {
			return fmt.Errorf("copying %s into library: %w", dirName, err)
		}
		fmt.Printf("%s %s -> library\n", style.OK("pushed"), dirName)
		return nil
	},
}
