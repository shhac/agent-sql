# `agent-sql` command map (reference)

Run `agent-sql usage` for concise LLM-optimized docs.
Run `agent-sql <command> usage` for detailed per-command docs.

## Query

- `agent-sql run "<sql>" [--limit <n>] [--write] [--compact] [-c <alias>]` — top-level alias for `query run`
- `agent-sql query run "<sql>" [--limit <n>] [--write] [--compact] [-c <alias>]` — execute a SQL query. Default row limit from config. `--write` opts in to write mode (requires credential with writePermission); success prints a `{"result":"ok","rows_affected":N,"command":"UPDATE"}` receipt. `--compact` emits typed NDJSON lines (`{"type":"columns",...}` once, then `{"type":"row","values":[...]}` per row).
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

- `agent-sql config get <key>...` — get one or more config values (1..N keys). Default output is NDJSON: one line per key — the `{key, value}` record, or `{"@unresolved":{"id","reason","fixable_by"}}` for an unknown key. Exit 0 even if some keys are unresolved.
- `agent-sql config set <key> <value>` — set a config value (validated against type/min/max)
- `agent-sql config reset` — reset all settings to defaults
- `agent-sql config list-keys` — list all valid keys with defaults, ranges, and `allowed_values` (NDJSON: one record per key; `{"data": [...]}` envelope with `--format json|yaml`)

## Connection (read-only for agents)

- `agent-sql connection add <alias> [connection-string] [--credential <name>] [--driver --host ...] [--option k=v ...]` — (human-only) save a connection. The positional connection-string (URL or file path) auto-detects the driver and parses host/port/database. Driver-specific knobs come from URL query strings or repeated `--option key=value` flags (flag wins on conflict). Examples: `connection add mydb postgres://localhost:5432/myapp --credential pg-cred`, `connection add mydb 'postgres://h/d?sslmode=require' --credential pg-cred`, `connection add local ./data.db --option _journal_mode=wal`. URLs with embedded credentials (`user:pass@`) are rejected -- credentials must live in the keychain via `credential add`.
- `agent-sql connection update <alias> [...] [--option k=v] [--clear-options]` — (human-only) update a saved connection. Only specified fields change. `--option` merges into existing options; `--clear-options` removes them all (applied before any new `--option` flags).
- `agent-sql connection list` — list all saved connections (NDJSON: one record per connection; `{"data": [...]}` envelope with `--format json|yaml`) with `alias`, `driver`, `display_url`, plus `host`/`port`/`database`/`credential`/`options` when set, and `default: true` for the default. `display_url` is the canonical connection target (per-driver default ports applied; options appended as `?key=value`; never includes credentials). `host` and `port` are the effective values (URL-backfilled if needed; default port applied for host-port drivers). Snowflake reports its account as `host` and omits `port`; SQLite/DuckDB omit both. Raw storage fields (path/url) are not emitted.
- `agent-sql connection test [-c <alias>]` — test connectivity (no alias = test default connection)

## Credential (human-only mutation)

- `agent-sql credential add <name> --username <u> --password <p> [--write]` — store a credential with values from flags. Use this when secrets are already in the user's shell (e.g. environment variables) and not pasted into chat.
- `agent-sql credential add <name> [--username <u>] [--write] --form` — opt-in flag that pops a native OS dialog (macOS osascript, Linux zenity/kdialog, Windows Win32) so the user types secrets directly into the operating system. The LLM driving the CLI never sees the secret value — only a redacted JSON receipt is emitted on stdout. Fails cleanly with `fixable_by="human"` if the host is headless or SSH'd.
- `agent-sql credential remove <name>` — delete a stored credential.
- `agent-sql credential list` — list stored credential names (passwords always masked).

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
| `-c, --connection <alias>` | Connection alias, file path, or URL (overrides env/default). File paths (e.g. `./data.db`, `./data.duckdb`) and URLs (e.g. `postgres://...`, `cockroachdb://...`, `mysql://...`, `mariadb://...`, `duckdb://...`, `snowflake://...`, `mssql://...`, `sqlserver://...`) work without prior setup. CockroachDB default port: 26257. MariaDB uses the same port (3306) and protocol as MySQL. MSSQL default port: 1433. DuckDB: requires `duckdb` CLI (`brew install duckdb`); `duckdb://` with no path for in-memory mode (query Parquet/CSV/JSON files); set `AGENT_SQL_DUCKDB_PATH` for custom CLI location. Snowflake ad-hoc: `snowflake://account/database/schema?warehouse=WH` with `AGENT_SQL_SNOWFLAKE_TOKEN` env var. Account format: `orgname-accountname` or `account.region`. |
| `--format jsonl\|json\|yaml` | Output format (default: jsonl or config). `csv` is additionally accepted on query commands (`run`, `query ...`); elsewhere it errors with `unknown format "csv", expected: json, yaml, jsonl`. |
| `--color auto\|always\|never` | Colorize JSON/YAML/NDJSON output (default: auto — color only when the stream is a terminal; piped output stays plain) |
| `--expand <field,...>`     | Expand specific truncated fields              |
| `--full`                   | Expand all truncated fields                   |
| `-C, --compact`            | Compact query output: typed NDJSON lines — column names once, rows as arrays. Reduces token count. |
| `--timeout <ms>`           | Query timeout override                        |
| `-d, --debug`              | Log `[debug] connection: <redacted>` and `[debug] query: <sql>` to stderr before execution. Stdout stays clean NDJSON. |

## Config keys

| Key                  | Default | Range       | Description                         |
| -------------------- | ------- | ----------- | ----------------------------------- |
| `defaults.format`    | jsonl   | jsonl/json/yaml     | Default output format (all commands)|
| `query.format`       | jsonl   | jsonl/json/yaml/csv | Default format for query commands (overrides `defaults.format` there) |
| `defaults.limit`     | 20      | 1-1000      | Default row limit for queries       |
| `query.timeout`      | 30000   | 1000-300000 | Query timeout in ms                 |
| `query.maxRows`      | 10000   | 1-10000     | Max rows per query                  |
| `truncation.maxLength` | 200   | 50-100000   | Max string length before truncation |
