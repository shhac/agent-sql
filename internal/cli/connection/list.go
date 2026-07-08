package connection

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/output"
)

func registerList(parent *cobra.Command) {
	list := &cobra.Command{
		Use:   "list",
		Short: "List saved connections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conns := config.GetConnections()
			defaultAlias := config.GetDefaultAlias()

			items := make([]any, 0, len(conns))
			for alias, conn := range conns {
				items = append(items, conn.AsReceipt(alias, alias == defaultAlias))
			}

			output.PrintList(items, nil, true)
			return nil
		},
	}
	parent.AddCommand(list)
}
