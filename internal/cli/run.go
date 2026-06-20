package cli

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/query"
	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/resolve"
)

func registerRunCommand(root *cobra.Command, g *GlobalFlags) {
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

			ctx, cancel := shared.MakeContext(g.TimeoutMS)
			defer cancel()

			drv, err := resolve.Resolve(ctx, resolve.Opts{
				Connection: g.Connection,
				Write:      write,
				Timeout:    g.TimeoutMS,
			})
			if err != nil {
				return err
			}
			defer func() { _ = drv.Close() }()

			return query.ExecuteRun(ctx, drv, sql, limit, write, query.RenderOpts{
				Expand:     g.Expand,
				Full:       g.Full,
				Compact:    g.Compact,
				FormatFlag: g.Format,
			})
		},
	}
	run.Flags().IntVarP(&limit, "limit", "l", 0, "Maximum rows to return")
	run.Flags().BoolVar(&write, "write", false, "Enable write mode")
	root.AddCommand(run)
}
