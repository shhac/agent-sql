import type { Command } from "commander";

const USAGE_TEXT = `connection — Manage SQL database connections

COMMANDS:
  connection add <alias> [connection-string] [--credential <name>] [options]
    Save a database connection. Alias is a short name (e.g. local, staging, prod).
    The optional connection-string positional argument accepts a URL or file path.
    Driver is auto-detected from the scheme; host/port/database/account/schema/warehouse/role
    are parsed from the URL. Flags override anything parsed from the connection string.
    Examples:
      connection add mydb postgres://localhost:5432/myapp --credential pg-cred
      connection add mydb mysql://localhost/myapp --credential mysql-cred
      connection add local ./data.db
      connection add sf snowflake://org-acct/DB/PUBLIC?warehouse=WH --credential sf-cred
    --driver pg|sqlite|mysql|snowflake  Database driver (auto-detected from URL if omitted).
    --host <host>             Database host (pg, mysql).
    --port <port>             Database port (pg, mysql).
    --database <db>           Database name (pg, mysql, snowflake).
    --path <path>             Path to SQLite file (resolved to absolute at add time).
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

AD-HOC: -c also accepts file paths (./data.db) and URLs (postgres://..., mysql://..., snowflake://...) without prior setup.
  Snowflake ad-hoc: snowflake://account/database/schema?warehouse=WH&role=ROLE (requires AGENT_SQL_SNOWFLAKE_TOKEN env var).

RESOLUTION ORDER: -c flag > AGENT_SQL_CONNECTION env > config default > error

CONFIG: ~/.config/agent-sql/config.json (respects XDG_CONFIG_HOME)
`;

export function registerUsage(connection: Command): void {
  connection
    .command("usage")
    .description("Print connection command documentation (LLM-optimized)")
    .action(() => {
      process.stdout.write(USAGE_TEXT);
    });
}
