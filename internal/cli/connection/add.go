package connection

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/output"
)

func registerAdd(parent *cobra.Command) {
	var (
		driverFlag  string
		host        string
		port        string
		database    string
		path        string
		url         string
		credName    string
		account     string
		warehouse   string
		role        string
		schema      string
		optionFlags []string
		setDefault  bool
	)

	add := &cobra.Command{
		Use:   "add <alias> [connection-string]",
		Short: "Add a SQL connection",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			in := addInputs{
				Alias: alias, DriverFlag: driverFlag,
				Host: host, Port: port, Database: database,
				Path: path, URL: url,
				Account: account, Warehouse: warehouse, Role: role, Schema: schema,
				CredName: credName, OptionFlags: optionFlags,
			}
			if len(args) > 1 {
				in.ConnString = args[1]
			}

			conn, warnings, err := buildConnectionFromAddArgs(in)
			if err != nil {
				output.WriteError(os.Stderr, err)
				return err
			}
			for _, w := range warnings {
				output.Warn("%s", w)
			}

			if err := validateCredentialRef(credName); err != nil {
				output.WriteError(os.Stderr, err)
				return err
			}

			if err := config.StoreConnection(alias, conn); err != nil {
				output.WriteError(os.Stderr, err)
				return err
			}

			if setDefault {
				_ = config.SetDefault(alias)
			}

			output.PrintJSON(map[string]any{
				"ok":         true,
				"alias":      alias,
				"driver":     conn.Driver,
				"host":       conn.Host,
				"port":       conn.Port,
				"database":   conn.Database,
				"path":       conn.Path,
				"url":        conn.URL,
				"credential": conn.Credential,
				"account":    conn.Account,
				"warehouse":  conn.Warehouse,
				"role":       conn.Role,
				"schema":     conn.Schema,
				"options":    conn.Options,
				"isDefault":  setDefault,
				"hint":       "Test with: agent-sql connection test",
			}, true)
			return nil
		},
	}
	add.Flags().StringVar(&driverFlag, "driver", "", "Database driver: pg, cockroachdb, sqlite, duckdb, mysql, mariadb, snowflake, mssql")
	add.Flags().StringVar(&host, "host", "", "Database host")
	add.Flags().StringVar(&port, "port", "", "Database port")
	add.Flags().StringVar(&database, "database", "", "Database name")
	add.Flags().StringVar(&path, "path", "", "Path to SQLite or DuckDB file")
	add.Flags().StringVar(&url, "url", "", "Connection URL")
	add.Flags().StringVar(&credName, "credential", "", "Credential alias for authentication")
	add.Flags().StringVar(&account, "account", "", "Snowflake account identifier")
	add.Flags().StringVar(&warehouse, "warehouse", "", "Snowflake warehouse")
	add.Flags().StringVar(&role, "role", "", "Snowflake role")
	add.Flags().StringVar(&schema, "schema", "", "Default schema")
	add.Flags().StringArrayVar(&optionFlags, "option", nil, "Driver-specific option as key=value (repeatable). Pass-through to the driver -- unknown keys surface at connect time.")
	add.Flags().BoolVar(&setDefault, "default", false, "Set as default connection")
	parent.AddCommand(add)
}
