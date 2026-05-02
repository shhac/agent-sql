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

	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/output"
	"github.com/shhac/agent-sql/internal/resolve"
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
      connection add mydb 'postgres://h/d?sslmode=require' --credential pg-cred
      connection add mydb mysql://localhost/myapp --option parseTime=true --credential mysql-cred
      connection add mydb mariadb://localhost/myapp --credential mariadb-cred
      connection add crdb cockroachdb://localhost:26257/myapp --credential crdb-cred
      connection add local ./data.db --option _journal_mode=wal
      connection add sf snowflake://org-acct/DB/PUBLIC?warehouse=WH --credential sf-cred
      connection add analytics ./analytics.duckdb --option memory_limit=4GB
      connection add ms 'mssql://host/mydb?encrypt=true' --credential mssql-cred
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
    --option key=value        Driver-specific knob (repeatable). Pass-through to the driver
                              (sslmode, parseTime, encrypt, memory_limit, query_tag, ...).
                              URL query params merge with --option flags; flag wins on conflict.
                              Unknown keys surface as the driver's own error at "connection test".
    --default                 Set as default connection.
    First connection added automatically becomes the default.

    NOTE: URLs with embedded credentials (user:pass@) are rejected -- secrets must
    live in the OS keychain via "credential add", referenced by --credential.

  connection update <alias> [options]
    Update a saved connection. Only specified fields are changed.
    Same flags as add (all optional). --option merges into existing options;
    --clear-options removes them all (apply before any new --option flags).

  connection remove <alias>
    Remove a saved connection. If it was the default, the next available becomes default.

  connection list
    List saved connections. Each row: alias, driver, display_url, plus
    host/port/database/credential/options when set, and default status.
    display_url applies per-driver default ports (5432, 26257, 3306, 1433)
    and appends options as ?key=value&... so it reflects the effective
    connect-time URL. SQLite/DuckDB omit host/port; Snowflake reports its
    account as host and omits port.

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

	shared.RegisterUsage(connection, "connection", usageText)

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
		optionFlags []string
		setDefault  bool
	)

	add := &cobra.Command{
		Use:   "add <alias> [connection-string]",
		Short: "Add a SQL connection",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]

			var options map[string]string
			if len(args) > 1 {
				parsed := parseConnectionString(args[1])
				// Explicit flag wins over connection-string parse on conflict.
				if driverFlag == "" {
					driverFlag = parsed.Driver
				}
				if host == "" {
					host = parsed.Host
				}
				if port == "" {
					port = parsed.Port
				}
				if database == "" {
					database = parsed.Database
				}
				if path == "" {
					path = parsed.Path
				}
				if url == "" {
					url = parsed.URL
				}
				if account == "" {
					account = parsed.Account
				}
				if warehouse == "" {
					warehouse = parsed.Warehouse
				}
				if role == "" {
					role = parsed.Role
				}
				if schema == "" {
					schema = parsed.Schema
				}
				options = parsed.Options
			}
			optsFromFlags, err := parseOptionFlags(optionFlags)
			if err != nil {
				output.WriteError(os.Stderr, err)
				return err
			}
			for k, v := range optsFromFlags {
				if options == nil {
					options = make(map[string]string)
				}
				options[k] = v
			}

			cleanedURL, warning, err := rejectEmbeddedCreds(url, alias, credName, "connection string")
			if err != nil {
				output.WriteError(os.Stderr, err)
				return err
			}
			url = cleanedURL
			if warning != "" {
				fmt.Fprint(os.Stderr, warning)
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
						"credential %q not found. Available: %s. Run: agent-sql credential add <alias> --username <user> --password <pass>",
						credName, listing,
					))
					return nil
				}
			}

			resolvedDriver := resolveDriver(driverFlag, url, path)
			if resolvedDriver == "" {
				output.WriteError(os.Stderr, fmt.Errorf(
					"cannot determine driver. Use --driver pg|cockroachdb|sqlite|duckdb|mysql|mariadb|snowflake|mssql, a connection URL, or a file path for SQLite",
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
					output.WriteError(os.Stderr, fmt.Errorf("invalid port: %s", port))
					return nil
				}
			}

			conn := config.Connection{
				Driver:     resolvedDriver,
				Host:       host,
				Port:       portNum,
				Database:   database,
				Path:       absPath,
				URL:        url,
				Credential: credName,
				Account:    account,
				Warehouse:  warehouse,
				Role:       role,
				Schema:     schema,
				Options:    options,
			}

			if err := config.StoreConnection(alias, conn); err != nil {
				output.WriteError(os.Stderr, err)
				return nil
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
				output.WriteError(os.Stderr, fmt.Errorf("connection %q not found", alias))
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
						"credential %q not found. Available: %s", credName, listing,
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
					output.WriteError(os.Stderr, fmt.Errorf("invalid port: %s", port))
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
				effectiveCred := credName
				if !cmd.Flags().Changed("credential") {
					effectiveCred = existing.Credential
				}
				cleanedURL, warning, err := rejectEmbeddedCreds(url, alias, effectiveCred, "--url")
				if err != nil {
					output.WriteError(os.Stderr, err)
					return err
				}
				if warning != "" {
					fmt.Fprint(os.Stderr, warning)
				}
				existing.URL = cleanedURL
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
			if clearOptions {
				existing.Options = nil
				updated = append(updated, "options")
			}
			if cmd.Flags().Changed("option") {
				optsFromFlags, err := parseOptionFlags(optionFlags)
				if err != nil {
					output.WriteError(os.Stderr, err)
					return err
				}
				if existing.Options == nil {
					existing.Options = make(map[string]string)
				}
				for k, v := range optsFromFlags {
					existing.Options[k] = v
				}
				updated = append(updated, "options")
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
	update.Flags().StringArrayVar(&optionFlags, "option", nil, "Driver-specific option as key=value (repeatable). Merged into existing options.")
	update.Flags().BoolVar(&clearOptions, "clear-options", false, "Remove all stored options before applying any --option flags.")
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

// renderConnection builds the per-row map for `connection list`. Keeps only
// the fields a human/agent needs to identify a connection; raw storage fields
// (path/url) are intentionally omitted -- display_url is the canonical view.
// Defensive: a panic while rendering one row reduces to a minimal entry so
// the rest of the list still prints.
func renderConnection(alias string, conn config.Connection, isDefault bool) (out map[string]any) {
	out = map[string]any{
		"alias":   alias,
		"driver":  conn.Driver,
		"default": isDefault,
	}
	defer func() {
		if r := recover(); r != nil {
			out = map[string]any{
				"alias":   alias,
				"driver":  conn.Driver,
				"default": isDefault,
				"error":   fmt.Sprintf("failed to render: %v", r),
			}
		}
	}()
	out["display_url"] = conn.DisplayURL()
	if host := conn.EffectiveHost(); host != "" {
		out["host"] = host
	}
	if port := conn.EffectivePort(); port != 0 {
		out["port"] = port
	}
	if conn.Database != "" {
		out["database"] = conn.Database
	}
	if conn.Credential != "" {
		out["credential"] = conn.Credential
	}
	if len(conn.Options) > 0 {
		out["options"] = conn.Options
	}
	return out
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
				items = append(items, renderConnection(alias, conn, alias == defaultAlias))
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
			defer func() { _ = drv.Close() }()

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
