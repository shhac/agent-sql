package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const usageText = `agent-sql — Read-only-by-default SQL CLI for AI agents (NDJSON output)

Supports PostgreSQL, CockroachDB, MySQL, MariaDB, SQLite, DuckDB, Snowflake, and MSSQL.
Output formats: NDJSON (default), JSON, YAML, CSV.

AD-HOC USAGE (zero setup):
  agent-sql run -c ./data.db "SELECT * FROM users"                     # SQLite file path
  agent-sql run -c postgres://user:pass@host/db "SELECT 1"            # PostgreSQL URL
  agent-sql run -c cockroachdb://user:pass@host:26257/db "SELECT 1"   # CockroachDB URL
  agent-sql run -c mysql://user:pass@host/db "SELECT 1"               # MySQL URL
  agent-sql run -c mariadb://user:pass@host/db "SELECT 1"             # MariaDB URL
  agent-sql run -c ./analytics.duckdb "SELECT * FROM events"          # DuckDB file path
  agent-sql run -c duckdb:// "SELECT * FROM 'data.parquet'"           # DuckDB in-memory
  agent-sql run -c snowflake://acct/db/schema?warehouse=WH "SELECT 1" # Snowflake URL
  agent-sql run -c mssql://user:pass@host/db "SELECT 1"               # MSSQL URL
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
  -c accepts connection aliases, file paths (./data.db, ./analytics.duckdb), or URLs.
  PG, CockroachDB, MySQL, MariaDB, MSSQL, and Snowflake require a stored credential for named connections.
  SQLite and DuckDB use file paths (credential optional).

SAFETY: Read-only by default. Use --write to opt in to writes.

OUTPUT: NDJSON to stdout (default) — one JSON object per line, no envelope.
  Each row: {"col": val, ..., "@truncated": null}. Last line when more rows: {"@pagination": {...}}.
  Errors always JSON to stderr: { "error": "...", "fixable_by": "agent"|"human" }.

DETAIL: Run "<command> usage" for per-command docs.
`

func registerUsageCommand(root *cobra.Command) {
	usage := &cobra.Command{
		Use:   "usage",
		Short: "Print concise documentation (LLM-optimized)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(usageText)
		},
	}
	root.AddCommand(usage)
}
