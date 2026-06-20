package connection

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/output"
)

// updateFlagNames are the cobra flag names whose `Changed` status the
// RunE collects to drive buildConnectionUpdates. Listed once so adding
// a new updatable field is one entry here + one branch in
// buildConnectionUpdates.
var updateFlagNames = []string{"driver", "host", "port", "database", "url", "path", "credential"}

func registerUpdate(parent *cobra.Command) {
	var (
		driverFlag   string
		host         string
		port         string
		database     string
		path         string
		url          string
		credName     string
		optionFlags  []string
		clearOptions bool
	)

	update := &cobra.Command{
		Use:   "update <alias>",
		Short: "Update a saved connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			existing := config.GetConnection(alias)
			if existing == nil {
				err := fmt.Errorf("connection %q not found", alias)
				return err
			}

			if cmd.Flags().Changed("credential") {
				if err := validateCredentialRef(credName); err != nil {
					return err
				}
			}

			changed := map[string]bool{}
			for _, name := range updateFlagNames {
				if cmd.Flags().Changed(name) {
					changed[name] = true
				}
			}
			in := updateInputs{
				Alias: alias, DriverFlag: driverFlag,
				Host: host, Port: port, Database: database,
				Path: path, URL: url, CredName: credName,
				OptionFlags: optionFlags, ClearOptions: clearOptions,
			}

			updated, warnings, err := buildConnectionUpdates(existing, in, changed)
			if err != nil {
				return err
			}
			for _, w := range warnings {
				output.Warn("%s", w)
			}

			if err := config.StoreConnection(alias, *existing); err != nil {
				return err
			}

			output.PrintJSON(map[string]any{"ok": true, "alias": alias, "updated": updated}, true)
			return nil
		},
	}
	update.Flags().StringVar(&driverFlag, "driver", "", "Database driver: pg, cockroachdb, sqlite, duckdb, mysql, mariadb, snowflake, mssql")
	update.Flags().StringVar(&host, "host", "", "Database host")
	update.Flags().StringVar(&port, "port", "", "Database port")
	update.Flags().StringVar(&database, "database", "", "Database name")
	update.Flags().StringVar(&path, "path", "", "Path to database file")
	update.Flags().StringVar(&url, "url", "", "Connection URL")
	update.Flags().StringVar(&credName, "credential", "", "Credential alias")
	update.Flags().StringArrayVar(&optionFlags, "option", nil, "Driver-specific option as key=value (repeatable). Merged into existing options.")
	update.Flags().BoolVar(&clearOptions, "clear-options", false, "Remove all stored options before applying any --option flags.")
	parent.AddCommand(update)
}
