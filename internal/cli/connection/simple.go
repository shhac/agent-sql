package connection

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/output"
)

func registerRemove(parent *cobra.Command) {
	remove := &cobra.Command{
		Use:   "remove <alias>",
		Short: "Remove a saved connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.RemoveConnection(args[0]); err != nil {
				return err
			}
			output.PrintResult(map[string]any{"ok": true, "removed": args[0]}, true)
			return nil
		},
	}
	parent.AddCommand(remove)
}

func registerSetDefault(parent *cobra.Command) {
	setDefault := &cobra.Command{
		Use:   "set-default <alias>",
		Short: "Set the default connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.SetDefault(args[0]); err != nil {
				return err
			}
			output.PrintResult(map[string]any{"ok": true, "default": args[0]}, true)
			return nil
		},
	}
	parent.AddCommand(setDefault)
}
