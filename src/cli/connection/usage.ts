import type { Command } from "commander";

const USAGE_TEXT = `connection — Manage SQL database connections

COMMANDS:
  connection add <alias> --driver pg|sqlite|mysql [options]
    Save a database connection. Alias is a short name (e.g. local, staging, prod).
    --driver pg|sqlite|mysql  Database driver (auto-detected from --url if omitted).
    --host <host>             Database host (pg, mysql).
    --port <port>             Database port (pg, mysql).
    --database <db>           Database name (pg, mysql).
    --path <path>             Path to SQLite file (resolved to absolute at add time).
    --url <url>               Connection URL. Driver auto-detected from scheme:
                              postgres:// → pg, mysql:// → mysql, sqlite:// → sqlite.
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

CREDENTIALS: Use "credential add" to store reusable auth. Reference via --credential.

RESOLUTION ORDER: -c flag > AGENT_SQL_CONNECTION env > config default > error

CONFIG: ~/.config/agent-sql/config.json (respects XDG_CONFIG_HOME)
`;

export function registerUsage(connection: Command): void {
  connection
    .command("usage")
    .description("Print connection command documentation (LLM-optimized)")
    .action(() => {
      console.log(USAGE_TEXT.trim());
    });
}
