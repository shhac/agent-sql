# `agent-sql` command map (reference)

Run `agent-sql usage` for concise LLM-optimized docs.
Run `agent-sql <command> usage` for detailed per-command docs.

## Query

- `agent-sql run "<sql>" [--limit <n>] [--write] [--compact] [-c <alias>]` — top-level alias for `query run`
- `agent-sql query run "<sql>" [--limit <n>] [--write] [--compact] [-c <alias>]` — execute a SQL query. Default row limit from config. `--write` opts in to write mode (requires credential with writePermission). `--compact` returns array-of-arrays format.
- `agent-sql query sample <table> [--limit <n>] [--where "<condition>"] [--compact] [-c <alias>]` — sample rows from a table (default 5). Dot notation for PG/Snowflake namespaces (`schema.table`). MySQL scopes to the connected database.
- `agent-sql query explain "<sql>" [--analyze] [-c <alias>]` — run EXPLAIN on a query. `--analyze` for EXPLAIN ANALYZE (read-only queries only).
- `agent-sql query count <table> [--where "<condition>"] [-c <alias>]` — count rows. `--where` to filter. Dot notation supported.

## Schema

- `agent-sql schema tables [--include-system] [-c <alias>]` — list all tables with namespace (PG/Snowflake: `public.users`). `--include-system` for `pg_catalog`/`information_schema`. MySQL scopes to the connected database.
- `agent-sql schema describe <table> [--detailed] [-c <alias>]` — show columns, types, nullability, defaults. Dot notation: `schema describe analytics.events`. `--detailed` adds constraints, indexes, comments.
- `agent-sql schema indexes [table] [-c <alias>]` — show indexes. All tables if no table specified. Dot notation supported. Not available for Snowflake (uses micro-partitioning instead of indexes).
- `agent-sql schema constraints [table] [--type pk|fk|unique|check] [-c <alias>]` — show constraints (PKs, FKs, unique, check). `--type` to filter. All tables if no table specified. Dot notation supported.
- `agent-sql schema search <pattern> [-c <alias>]` — search table and column names by pattern.
- `agent-sql schema dump [--tables <list>] [--include-system] [-c <alias>]` — full schema dump (all tables, columns, indexes, constraints). `--tables analytics.events,public.users` to filter.

## Config

- `agent-sql config get <key>` — get a config value
- `agent-sql config set <key> <value>` — set a config value (validated against type/min/max)
- `agent-sql config reset` — reset all settings to defaults
- `agent-sql config list-keys` — list all valid keys with defaults and ranges

## Connection (read-only for agents)

- `agent-sql connection add <alias> [connection-string] [--credential <name>] [--driver --host ...]` — (human-only) save a connection. The optional connection-string positional argument (URL or file path) auto-detects driver and parses host/port/database. Flags override parsed values. Examples: `connection add mydb postgres://localhost:5432/myapp --credential pg-cred`, `connection add local ./data.db`.
- `agent-sql connection list` — list all saved connections with driver, host/path, credential, and default status
- `agent-sql connection test [-c <alias>]` — test connectivity (no alias = test default connection)

## Usage

- `agent-sql usage` — LLM-optimized top-level docs
- `agent-sql <command> usage` — detailed per-command docs:
  - `agent-sql connection usage`
  - `agent-sql schema usage`
  - `agent-sql query usage`
  - `agent-sql config usage`

## Global flags

| Flag                       | Description                                  |
| -------------------------- | -------------------------------------------- |
| `-c, --connection <alias>` | Connection alias, file path, or URL (overrides env/default). File paths (e.g. `./data.db`) and URLs (e.g. `postgres://...`, `mysql://...`, `snowflake://...`) work without prior setup. Snowflake ad-hoc: `snowflake://account/database/schema?warehouse=WH` with `AGENT_SQL_SNOWFLAKE_TOKEN` env var. Account format: `orgname-accountname` or `account.region`. |
| `--format jsonl\|json\|yaml\|csv` | Output format (default: jsonl or config)       |
| `--expand <field,...>`     | Expand specific truncated fields              |
| `--full`                   | Expand all truncated fields                   |
| `--timeout <ms>`           | Query timeout override                        |

## Config keys

| Key                  | Default | Range       | Description                         |
| -------------------- | ------- | ----------- | ----------------------------------- |
| `defaults.format`    | jsonl   | jsonl/json/yaml/csv | Default output format               |
| `defaults.limit`     | 20      | 1-1000      | Default row limit for queries       |
| `query.timeout`      | 30000   | 1000-300000 | Query timeout in ms                 |
| `query.maxRows`      | 10000   | 1-10000     | Max rows per query                  |
| `truncation.maxLength` | 200   | 50-100000   | Max string length before truncation |
