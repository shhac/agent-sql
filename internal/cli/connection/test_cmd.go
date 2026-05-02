package connection

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/output"
	"github.com/shhac/agent-sql/internal/resolve"
)

// registerTest is the only command in this package that actually
// connects to a driver -- it lives in test_cmd.go (not test.go to
// avoid any possibility of the file being mistaken for a test file).
// The other commands (add/update/list/remove/set-default) operate on
// stored config only.
func registerTest(parent *cobra.Command, globals func() (string, int)) {
	test := &cobra.Command{
		Use:   "test [alias]",
		Short: "Test connectivity for a connection",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			connFlag, timeout := globals()
			connAlias := connFlag
			if len(args) > 0 {
				connAlias = args[0]
			}

			ctx := context.Background()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
				defer cancel()
			}

			drv, err := resolve.Resolve(ctx, resolve.Opts{Connection: connAlias, Timeout: timeout})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return err
			}
			defer func() { _ = drv.Close() }()

			result, err := drv.Query(ctx, "SELECT 1", driver.QueryOpts{})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return err
			}

			displayAlias := connAlias
			if displayAlias == "" {
				displayAlias = "default"
			}
			output.PrintJSON(map[string]any{
				"ok":         true,
				"connection": displayAlias,
				"rows":       result.Rows,
			}, true)
			return nil
		},
	}
	parent.AddCommand(test)
}
