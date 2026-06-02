package cmd

import (
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(showCmd)
}

var showCmd = &cobra.Command{
	Use:         "show [bundle]",
	Annotations: map[string]string{"group": "Inspect:"},
	Short:       "Show the skills in a bundle (fzf-picks when no name)",
	Args:        cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return renderBundle(args)
	},
}
