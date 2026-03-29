package connection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/resolve"
	"github.com/shhac/agent-sql/internal/output"
)

const usageText = `connection — Manage SQL database connections

COMMANDS:
  connection add <alias> [connection-string] [--credential <name>] [options]
    Save a database connection. Alias is a short name (e.g. local, staging, prod).
    The optional connection-string positional argument accepts a URL or file path.
    Driver is auto-detected from the scheme; host/port/database/account/schema/warehouse/role
    are parsed from the URL. Flags override anything parsed from the connection string.
    Examples:
      connection add mydb postgres://localhost:5432/myapp --credential pg-cred
      connection add mydb mysql://localhost/myapp --credential mysql-cred
      connection add mydb mariadb://localhost/myapp --credential mariadb-cred
      connection add crdb cockroachdb://localhost:26257/myapp --credential crdb-cred
      connection add local ./data.db
      connection add sf snowflake://org-acct/DB/PUBLIC?warehouse=WH --credential sf-cred
      connection add analytics ./analytics.duckdb
      connection add ms mssql://user:pass@host/mydb --credential mssql-cred
    --driver pg|cockroachdb|sqlite|mysql|mariadb|duckdb|snowflake|mssql  Database driver (auto-detected from URL/extension if omitted).
    --host <host>             Database host (pg, cockroachdb, mysql, mariadb, mssql).
    --port <port>             Database port (pg, cockroachdb, mysql, mariadb, mssql).
    --database <db>           Database name (pg, mysql, mariadb, snowflake, mssql).
    --path <path>             Path to SQLite or DuckDB file (resolved to absolute at add time).
    --url <url>               Connection URL (alternative to positional connection-string).
    --account <id>            Snowflake account identifier (orgname-accountname or account.region).
    --warehouse <wh>          Snowflake warehouse.
    --role <role>             Snowflake role.
    --schema <schema>         Snowflake schema.
    --credential <name>       Reference a stored credential for authentication.
    --default                 Set as default connection.
    First connection added automatically becomes the default.

  connection update <alias> [options]
    Update a saved connection. Only specified fields are changed.
    Same flags as add (all optional). --no-credential removes the credential.

  connection remove <alias>
    Remove a saved connection. If it was the default, the next available becomes default.

  connection list
    List all saved connections with driver, host, database, credential, and default status.

  connection test [alias]
    Connect and run SELECT 1 to verify connectivity. Uses default connection if alias omitted.

  connection set-default <alias>
    Set which connection is used when -c is not specified.

  connection usage
    Print this reference.

SETUP ORDER: Create a credential first ("credential add"), then reference it in "connection add --credential <name>".

AD-HOC: -c also accepts file paths (./data.db, ./data.duckdb) and URLs (postgres://..., cockroachdb://..., mysql://..., mariadb://..., duckdb://..., snowflake://..., mssql://...) without prior setup.
  DuckDB ad-hoc: duckdb:// for in-memory mode (query Parquet/CSV/JSON files directly). Requires duckdb CLI (brew install duckdb). Set AGENT_SQL_DUCKDB_PATH for custom location.
  Snowflake ad-hoc: snowflake://account/database/schema?warehouse=WH&role=ROLE (requires AGENT_SQL_SNOWFLAKE_TOKEN env var).

RESOLUTION ORDER: -c flag > AGENT_SQL_CONNECTION env > config default > error

CONFIG: ~/.config/agent-sql/config.json (respects XDG_CONFIG_HOME)
`

// Register adds the connection command group to root.
func Register(root *cobra.Command, globals func() (string, int)) {
	connection := &cobra.Command{
		Use:   "connection",
		Short: "Manage SQL connections",
	}

	registerAdd(connection)
	registerUpdate(connection)
	registerRemove(connection)
	registerList(connection)
	registerTest(connection, globals)
	registerSetDefault(connection)

	connection.AddCommand(&cobra.Command{
		Use:   "usage",
		Short: "Print connection command documentation (LLM-optimized)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(usageText)
		},
	})

	root.AddCommand(connection)
}

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
		setDefault  bool
	)

	add := &cobra.Command{
		Use:   "add <alias> [connection-string]",
		Short: "Add a SQL connection",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]

			if len(args) > 1 {
				parseConnectionString(args[1], &driverFlag, &host, &port, &database, &path, &url, &account, &warehouse, &role, &schema)
			}

			if credName != "" {
				cred := credential.Get(credName)
				if cred == nil {
					names := credential.List()
					listing := "(none)"
					if len(names) > 0 {
						listing = strings.Join(names, ", ")
					}
					output.WriteError(os.Stderr, fmt.Errorf(
						"Credential %q not found. Available: %s. Run: agent-sql credential add <alias> --username <user> --password <pass>",
						credName, listing,
					))
					return nil
				}
			}

			resolvedDriver := resolveDriver(driverFlag, url, path)
			if resolvedDriver == "" {
				output.WriteError(os.Stderr, fmt.Errorf(
					"Cannot determine driver. Use --driver pg|cockroachdb|sqlite|duckdb|mysql|mariadb|snowflake|mssql, a connection URL, or a file path for SQLite",
				))
				return nil
			}

			absPath := path
			if absPath != "" {
				var err error
				absPath, err = filepath.Abs(absPath)
				if err != nil {
					output.WriteError(os.Stderr, err)
					return nil
				}
			}

			portNum := 0
			if port != "" {
				var err error
				portNum, err = strconv.Atoi(port)
				if err != nil {
					output.WriteError(os.Stderr, fmt.Errorf("Invalid port: %s", port))
					return nil
				}
			}

			conn := config.Connection{
				Driver:    resolvedDriver,
				Host:      host,
				Port:      portNum,
				Database:  database,
				Path:      absPath,
				URL:       url,
				Credential: credName,
				Account:   account,
				Warehouse: warehouse,
				Role:      role,
				Schema:    schema,
			}

			if err := config.StoreConnection(alias, conn); err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}

			if setDefault {
				config.SetDefault(alias)
			}

			output.PrintJSON(map[string]any{
				"ok":        true,
				"alias":     alias,
				"driver":    conn.Driver,
				"host":      conn.Host,
				"port":      conn.Port,
				"database":  conn.Database,
				"path":      conn.Path,
				"url":       conn.URL,
				"credential": conn.Credential,
				"account":   conn.Account,
				"warehouse": conn.Warehouse,
				"role":      conn.Role,
				"schema":    conn.Schema,
				"isDefault": setDefault,
				"hint":      "Test with: agent-sql connection test",
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
	add.Flags().BoolVar(&setDefault, "default", false, "Set as default connection")
	parent.AddCommand(add)
}

func registerUpdate(parent *cobra.Command) {
	var (
		driverFlag string
		host       string
		port       string
		database   string
		path       string
		url        string
		credName   string
	)

	update := &cobra.Command{
		Use:   "update <alias>",
		Short: "Update a saved connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			existing := config.GetConnection(alias)
			if existing == nil {
				output.WriteError(os.Stderr, fmt.Errorf("Connection %q not found", alias))
				return nil
			}

			if credName != "" {
				cred := credential.Get(credName)
				if cred == nil {
					names := credential.List()
					listing := "(none)"
					if len(names) > 0 {
						listing = strings.Join(names, ", ")
					}
					output.WriteError(os.Stderr, fmt.Errorf(
						"Credential %q not found. Available: %s", credName, listing,
					))
					return nil
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
					output.WriteError(os.Stderr, fmt.Errorf("Invalid port: %s", port))
					return nil
				}
				existing.Port = n
				updated = append(updated, "port")
			}
			if cmd.Flags().Changed("database") {
				existing.Database = database
				updated = append(updated, "database")
			}
			if cmd.Flags().Changed("url") {
				existing.URL = url
				updated = append(updated, "url")
			}
			if cmd.Flags().Changed("path") {
				abs, err := filepath.Abs(path)
				if err != nil {
					output.WriteError(os.Stderr, err)
					return nil
				}
				existing.Path = abs
				updated = append(updated, "path")
			}
			if cmd.Flags().Changed("credential") {
				existing.Credential = credName
				updated = append(updated, "credential")
			}

			if err := config.StoreConnection(alias, *existing); err != nil {
				output.WriteError(os.Stderr, err)
				return nil
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
	parent.AddCommand(update)
}

func registerRemove(parent *cobra.Command) {
	remove := &cobra.Command{
		Use:   "remove <alias>",
		Short: "Remove a saved connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.RemoveConnection(args[0]); err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}
			output.PrintJSON(map[string]any{"ok": true, "removed": args[0]}, true)
			return nil
		},
	}
	parent.AddCommand(remove)
}

func registerList(parent *cobra.Command) {
	list := &cobra.Command{
		Use:   "list",
		Short: "List saved connections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conns := config.GetConnections()
			defaultAlias := config.GetDefaultAlias()

			items := make([]map[string]any, 0, len(conns))
			for alias, conn := range conns {
				items = append(items, map[string]any{
					"alias":      alias,
					"driver":     conn.Driver,
					"host":       conn.Host,
					"port":       conn.Port,
					"database":   conn.Database,
					"path":       conn.Path,
					"url":        conn.URL,
					"credential": conn.Credential,
					"default":    alias == defaultAlias,
				})
			}

			output.PrintJSON(map[string]any{"connections": items}, true)
			return nil
		},
	}
	parent.AddCommand(list)
}

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
				return nil
			}
			defer drv.Close()

			result, err := drv.Query(ctx, "SELECT 1", driver.QueryOpts{})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
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

func registerSetDefault(parent *cobra.Command) {
	setDefault := &cobra.Command{
		Use:   "set-default <alias>",
		Short: "Set the default connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.SetDefault(args[0]); err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}
			output.PrintJSON(map[string]any{"ok": true, "default": args[0]}, true)
			return nil
		},
	}
	parent.AddCommand(setDefault)
}

// helpers

func resolveDriver(driverFlag, url, path string) string {
	if driverFlag != "" {
		return driverFlag
	}
	if url != "" {
		detected := driver.DetectDriverFromURL(url)
		if detected != "" {
			return string(detected)
		}
	}
	if path != "" {
		detected := driver.DetectDriverFromURL(path)
		if detected != "" {
			return string(detected)
		}
		return string(driver.DriverSQLite)
	}
	return ""
}

func parseConnectionString(connStr string, driverFlag, host, port, database, path, url, account, warehouse, role, schema *string) {
	lower := strings.ToLower(connStr)

	// DuckDB file extensions
	if strings.HasSuffix(lower, ".duckdb") {
		*path = connStr
		return
	}

	// SQLite file extensions
	for _, ext := range []string{".sqlite", ".db", ".sqlite3", ".db3"} {
		if strings.HasSuffix(lower, ext) {
			*path = connStr
			return
		}
	}

	// File path
	if driver.IsFilePath(connStr) {
		*path = connStr
		return
	}

	// URL detection
	detected := driver.DetectDriverFromURL(connStr)
	if detected == "" {
		return
	}

	switch detected {
	case driver.DriverSQLite:
		*path = strings.TrimPrefix(connStr, "sqlite://")
	case driver.DriverDuckDB:
		*path = strings.TrimPrefix(connStr, "duckdb://")
	case driver.DriverSnowflake:
		*url = connStr
		// Parse snowflake-specific fields from the URL
		// snowflake://account/database/schema?warehouse=WH&role=ROLE
		trimmed := strings.TrimPrefix(connStr, "snowflake://")
		parts := strings.SplitN(trimmed, "?", 2)
		pathParts := strings.Split(parts[0], "/")
		if *account == "" && len(pathParts) > 0 {
			*account = pathParts[0]
		}
		if *database == "" && len(pathParts) > 1 {
			*database = pathParts[1]
		}
		if *schema == "" && len(pathParts) > 2 {
			*schema = pathParts[2]
		}
		if len(parts) > 1 {
			for _, param := range strings.Split(parts[1], "&") {
				kv := strings.SplitN(param, "=", 2)
				if len(kv) != 2 {
					continue
				}
				switch strings.ToLower(kv[0]) {
				case "warehouse":
					if *warehouse == "" {
						*warehouse = kv[1]
					}
				case "role":
					if *role == "" {
						*role = kv[1]
					}
				}
			}
		}
	default:
		*url = connStr
		// Parse host/port/database from standard URLs
		trimmed := connStr
		for _, prefix := range []string{"postgres://", "postgresql://", "cockroachdb://", "mysql://", "mariadb://", "mssql://", "sqlserver://"} {
			trimmed = strings.TrimPrefix(trimmed, prefix)
		}
		// After removing scheme: [user:pass@]host[:port]/database
		atIdx := strings.LastIndex(trimmed, "@")
		hostPart := trimmed
		if atIdx >= 0 {
			hostPart = trimmed[atIdx+1:]
		}
		slashIdx := strings.Index(hostPart, "/")
		if slashIdx >= 0 {
			if *database == "" {
				db := hostPart[slashIdx+1:]
				if qIdx := strings.Index(db, "?"); qIdx >= 0 {
					db = db[:qIdx]
				}
				if db != "" {
					*database = db
				}
			}
			hostPart = hostPart[:slashIdx]
		}
		colonIdx := strings.LastIndex(hostPart, ":")
		if colonIdx >= 0 {
			if *host == "" {
				*host = hostPart[:colonIdx]
			}
			if *port == "" {
				*port = hostPart[colonIdx+1:]
			}
		} else {
			if *host == "" && hostPart != "" {
				*host = hostPart
			}
		}
	}
}
