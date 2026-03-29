import type { Command } from "commander";

const USAGE_TEXT = `agent-sql — Read-only-by-default SQL CLI for AI agents (JSONL output)

Supports PostgreSQL, CockroachDB, MySQL, MariaDB, SQLite, DuckDB, and Snowflake. Output formats: JSONL (default), JSON, YAML, CSV.

AD-HOC USAGE (zero setup):
  agent-sql run -c ./data.db "SELECT * FROM users"                     # SQLite file path
  agent-sql run -c postgres://user:pass@host/db "SELECT 1"            # PostgreSQL URL
  agent-sql run -c cockroachdb://user:pass@host:26257/db "SELECT 1"   # CockroachDB URL
  agent-sql run -c mysql://user:pass@host/db "SELECT 1"               # MySQL URL
  agent-sql run -c mariadb://user:pass@host/db "SELECT 1"            # MariaDB URL
  agent-sql run -c snowflake://acct/db/schema?warehouse=WH "SELECT 1" # Snowflake URL (needs AGENT_SQL_SNOWFLAKE_TOKEN)
  agent-sql run -c ./data.duckdb "SELECT * FROM users"                 # DuckDB file path
  agent-sql run -c duckdb:// "SELECT * FROM 'data/*.parquet'"          # DuckDB in-memory (query files directly)
  agent-sql schema tables -c ./mydb.sqlite                             # schema from file

NAMED CONNECTIONS (human-only setup):
  credential add <alias> --username <u> --password <p> [--write]
  connection add <alias> [connection-string] [--credential <name>] [--driver --host --port --database --path --url]
  connection test [alias]

COMMANDS:
  credential add|remove|list                           Manage stored credentials (set up first)
  connection add|remove|update|list|test|set-default   Manage SQL connections
  config get|set|reset|list-keys                       Persistent settings

  run "<sql>" [--limit] [--write] [--compact]          Execute SQL (top-level alias)
  query run "<sql>" [--limit] [--write] [--compact]    Execute SQL
  query sample <table> [--limit] [--where]             Sample rows
  query explain "<sql>" [--analyze]                    EXPLAIN a query
  query count <table> [--where]                        Count rows

  schema tables [--include-system]                     List tables
  schema describe <table> [--detailed]                  Columns, types, nullability
  schema indexes [table]                               Index details
  schema constraints [table] [--type]                  PKs, FKs, unique, check
  schema search <pattern>                              Search table/column names
  schema dump [--tables] [--include-system]            Full schema dump

GLOBAL FLAGS: -c <connection> (alias, file path, or URL), --format jsonl|json|yaml|csv, --expand <fields>, --full, --timeout <ms>

CONNECTION: -c flag > AGENT_SQL_CONNECTION env > config default.
  -c accepts connection aliases, file paths (./data.db, ./data.duckdb), or URLs (postgres://..., cockroachdb://..., mysql://..., mariadb://..., duckdb://..., snowflake://...).
  PG, MySQL/MariaDB, and Snowflake require a stored credential for named connections. SQLite and DuckDB use file path (credential optional).
  DuckDB: requires duckdb CLI installed separately (brew install duckdb). Set AGENT_SQL_DUCKDB_PATH for custom location. duckdb:// with no path for in-memory mode (query Parquet/CSV/JSON files directly).
  Snowflake ad-hoc URLs: snowflake://account/database/schema?warehouse=WH&role=ROLE (requires AGENT_SQL_SNOWFLAKE_TOKEN env var).

SAFETY: Read-only by default. Use --write to opt in to writes.
  --write requires a credential with writePermission (or credential-less SQLite).
  Results capped at query.maxRows (default 10,000). Timeout: query.timeout (default 30s).

OUTPUT: JSONL to stdout (default) — one JSON object per line, no envelope.
  Each row: {"col": val, ..., "@truncated": null}. Last line when more rows: {"@pagination": {...}}.
  Non-tabular output (schema, config, explain, count, admin) uses JSON envelope regardless of format.
  Use --format json for envelope format, --format yaml or --format csv for alternates.
  Errors always JSON to stderr: { "error": "...", "fixable_by": "agent"|"human" }.
  Long strings truncated with @truncated metadata. Use --full or --expand <field> to expand.

DETAIL: Run "<command> usage" for per-command docs.
`;

export function registerUsageCommand({ program }: { program: Command }): void {
  program
    .command("usage")
    .description("Print concise documentation (LLM-optimized)")
    .action(() => {
      process.stdout.write(USAGE_TEXT);
    });
}
