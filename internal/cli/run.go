package cli

import (
	"github.com/spf13/cobra"
)

func registerRunCommand(root *cobra.Command) {
	run := &cobra.Command{
		Use:   `run "<sql>"`,
		Short: "Execute a SQL query (top-level alias for query run)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: implement query execution
			return nil
		},
	}
	run.Flags().IntP("limit", "l", 0, "Maximum rows to return")
	run.Flags().Bool("write", false, "Enable write mode")
	root.AddCommand(run)
}
