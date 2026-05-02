package connection

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/output"
)

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
				output.WriteError(os.Stderr, err)
				return err
			}

			if cmd.Flags().Changed("credential") {
				if err := validateCredentialRef(credName); err != nil {
					output.WriteError(os.Stderr, err)
					return err
				}
			}

			updated := []string{}
			if cmd.Flags().Changed("driver") {
				existing.Driver = driverFlag
				updated = append(updated, "driver")
			}
			if cmd.Flags().Changed("host") {
				existing.Host = host
				updated = append(updated, "host")
			}
			if cmd.Flags().Changed("port") {
				n, err := strconv.Atoi(port)
				if err != nil {
					portErr := fmt.Errorf("invalid port: %s", port)
					output.WriteError(os.Stderr, portErr)
					return portErr
				}
				existing.Port = n
				updated = append(updated, "port")
			}
			if cmd.Flags().Changed("database") {
				existing.Database = database
				updated = append(updated, "database")
			}
			if cmd.Flags().Changed("url") {
				warning, err := applyURLUpdate(existing, url, alias, credName, cmd.Flags().Changed("credential"))
				if err != nil {
					output.WriteError(os.Stderr, err)
					return err
				}
				if warning != "" {
					output.Warn("%s", warning)
				}
				updated = append(updated, "url")
			}
			if cmd.Flags().Changed("path") {
				abs, err := filepath.Abs(path)
				if err != nil {
					output.WriteError(os.Stderr, err)
					return err
				}
				existing.Path = abs
				updated = append(updated, "path")
			}
			if cmd.Flags().Changed("credential") {
				existing.Credential = credName
				updated = append(updated, "credential")
			}
			optsChanged, err := applyOptionUpdates(existing, clearOptions, optionFlags)
			if err != nil {
				output.WriteError(os.Stderr, err)
				return err
			}
			if optsChanged {
				updated = append(updated, "options")
			}

			if err := config.StoreConnection(alias, *existing); err != nil {
				output.WriteError(os.Stderr, err)
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
