// Package connection registers the `connection` command group with
// cobra. The package is split per-command:
//
//   - register.go: Register + usage text.
//   - add.go: `connection add`.
//   - update.go: `connection update`.
//   - list.go: `connection list` + the per-row renderer.
//   - simple.go: `connection remove` and `connection set-default`.
//   - test_cmd.go: `connection test` (the only command needing resolve).
//   - build.go: pure helpers used by add/update (parse, merge, strip,
//     validate); has its own _test.go.
//   - parse.go: positional connection-string parser + URL helpers.
package connection

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/shared"
	libcli "github.com/shhac/lib-agent-cli/cli"
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
func Register(root *cobra.Command, globals func() *shared.GlobalFlags) {
	connection := &cobra.Command{
		Use:   "connection",
		Short: "Manage SQL connections",
	}
	libcli.HandleUnknownCommand(connection, "run 'agent-sql connection usage' to see the available commands")

	registerAdd(connection, globals)
	registerUpdate(connection, globals)
	registerRemove(connection, globals)
	registerList(connection, globals)
	registerTest(connection, globals)
	registerSetDefault(connection, globals)

	shared.RegisterUsage(connection, "connection", usageText)

	root.AddCommand(connection)
}
