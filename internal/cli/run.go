package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/query"
	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/output"
	"github.com/shhac/agent-sql/internal/resolve"
)

func registerRunCommand(root *cobra.Command) {
	var (
		limit int
		write bool
	)

	run := &cobra.Command{
		Use:   `run "<sql>"`,
		Short: "Execute a SQL query (top-level alias for query run)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sql := args[0]

			ctx, cancel := shared.MakeContext(flagTimeout)
			defer cancel()

			drv, err := resolve.Resolve(ctx, resolve.Opts{
				Connection: flagConnection,
				Write:      write,
				Timeout:    flagTimeout,
			})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}
			defer drv.Close()

			return query.ExecuteRun(ctx, drv, sql, limit, write, flagExpand, flagFull, flagCompact)
		},
	}
	run.Flags().IntVarP(&limit, "limit", "l", 0, "Maximum rows to return")
	run.Flags().BoolVar(&write, "write", false, "Enable write mode")
	root.AddCommand(run)
}
