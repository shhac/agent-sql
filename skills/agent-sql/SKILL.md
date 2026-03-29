---
name: agent-sql
description: |
  Read-only-by-default SQL CLI for AI agents. Use when:
  - Exploring SQL databases (PostgreSQL, CockroachDB, MySQL, MariaDB, SQLite, DuckDB, Snowflake) -- tables, columns, indexes, constraints
  - Querying data (SELECT, sample rows, count, explain plans)
  - Writing data when explicitly permitted (INSERT, UPDATE, DELETE with --write)
  - Checking database connections or adjusting CLI settings
  Triggers: "sql query", "sql database", "sql table", "sql schema", "postgres", "postgresql", "cockroachdb", "cockroach", "mysql", "sqlite", "duckdb", "parquet", "snowflake", "sql connection", "query database", "sql select", "sql insert", "sql explain", "sql count", "sql sample", "database schema", "describe table", "sql columns", "sql indexes", "mariadb"
---

# SQL database exploration with `agent-sql`

`agent-sql` is a read-only-by-default SQL CLI binary on `$PATH`. Query output is JSONL to stdout (one JSON object per line, no envelope). Non-tabular output (schema, config, admin) uses JSON envelope. Errors go to stderr as `{ "error": "...", "hint": "...", "fixable_by": "agent|human|retry" }` with non-zero exit.

Supports PostgreSQL, CockroachDB, MySQL, MariaDB, SQLite, DuckDB, and Snowflake.

## Quick start

Use `-c` with a file path, URL, or saved alias -- no setup needed for ad-hoc queries:

```bash
agent-sql run -c ./data.db 'SELECT * FROM users'                   # SQLite file (zero setup)
agent-sql run -c postgres://user:pass@host/db 'SELECT * FROM users' # PG URL (zero setup)
agent-sql run -c cockroachdb://user:pass@host:26257/db 'SELECT * FROM users' # CockroachDB URL
agent-sql run -c mysql://user:pass@host/db 'SELECT * FROM users'   # MySQL URL (zero setup)
agent-sql run -c mariadb://user:pass@host/db 'SELECT * FROM users' # MariaDB URL (zero setup)
agent-sql run -c snowflake://org-acct/mydb/public?warehouse=WH 'SELECT * FROM users' # Snowflake URL
agent-sql run -c ./analytics.duckdb 'SELECT * FROM events'         # DuckDB file (zero setup)
agent-sql run -c duckdb:// "SELECT * FROM 'data/*.parquet'"        # DuckDB in-memory (query files directly)
agent-sql run -c myalias 'SELECT * FROM users'                     # saved connection alias
```

For named connections, discover what's available:

```bash
agent-sql usage                          # full reference card
agent-sql connection list                # see available connections
agent-sql connection test                # verify default connection works
```

## Exploring a database

```bash
agent-sql schema tables                              # list all tables
agent-sql schema tables --include-system              # include system tables (PG)
agent-sql schema describe users                       # columns, types, nullability, defaults
agent-sql schema describe users --detailed             # add constraints, indexes, comments
agent-sql schema describe analytics.events            # PG namespace dot notation
agent-sql schema indexes                              # all indexes across all tables (not available for Snowflake)
agent-sql schema indexes users                        # indexes for a specific table
agent-sql schema constraints users                    # PKs, FKs, unique, check constraints
agent-sql schema constraints --type fk                # filter by constraint type
agent-sql schema search user                          # search table and column names
agent-sql schema dump                                 # full schema (all tables, columns, indexes, constraints)
agent-sql schema dump --tables users,orders           # dump specific tables only
agent-sql query sample users                          # 5 sample rows (default)
agent-sql query sample users --limit 10 --where "status = 'active'"
```

## Querying data

```bash
agent-sql run "SELECT * FROM users WHERE age >= 21"                 # top-level shorthand
agent-sql query run "SELECT * FROM users WHERE age >= 21"           # equivalent
agent-sql query run "SELECT * FROM users" --limit 50                # override row limit
agent-sql query run "SELECT * FROM users" --compact                 # array-of-arrays (saves tokens)
agent-sql query explain "SELECT * FROM users WHERE email = 'a@b'"   # query plan
agent-sql query explain "SELECT * FROM users" --analyze             # EXPLAIN ANALYZE
agent-sql query count users                                         # total row count
agent-sql query count users --where "status = 'active'"             # filtered count
```

## Writing data (requires permission)

Writes are blocked by default. The user must configure a credential with write permission. Then opt in per-query:

```bash
agent-sql run "INSERT INTO logs (msg) VALUES ('hello')" --write
agent-sql run "UPDATE users SET active = true WHERE id = 1" --write
```

If writes are blocked, the error will have `"fixable_by": "human"` -- do not retry, escalate to the user.

## Truncation

Strings exceeding `truncation.maxLength` (default 200) are truncated with `...` and an `@truncated` metadata object per row showing original lengths. `@truncated` is always present (`null` when no truncation).

```bash
agent-sql --full query run "SELECT * FROM posts"             # expand all fields
agent-sql --expand body query run "SELECT * FROM posts"      # expand specific field
```

These are global flags -- place them before or after the command.

## Timeout

Default timeout is 30s (configurable via `query.timeout`). Override per-command:

```bash
agent-sql --timeout 60000 run "SELECT * FROM large_table"
```

## Configuration

```bash
agent-sql config list-keys                           # all keys with defaults/ranges
agent-sql config set defaults.limit 50
agent-sql config get query.timeout
agent-sql config reset                               # restore defaults
```

Key settings: `defaults.format` (jsonl), `defaults.limit` (20), `query.timeout` (30000ms), `query.maxRows` (10000), `truncation.maxLength` (200).

## Connection management

Connections are set up by the user. The agent can list and test but not add/remove/modify:

```bash
agent-sql connection list                            # saved connections + defaults
agent-sql connection test                            # test default connection
agent-sql connection test -c prod                    # test specific connection
# Human-only setup examples:
# connection add mydb postgres://localhost:5432/myapp --credential pg-cred
# connection add local ./data.db
```

Connection resolution: `-c` flag > `AGENT_SQL_CONNECTION` env > config default > error listing available connections. The `-c` flag accepts aliases, file paths (e.g. `./data.db`, `./data.duckdb`), or URLs (e.g. `postgres://...`, `cockroachdb://...`, `mysql://...`, `mariadb://...`, `duckdb://...`, `snowflake://...`). DuckDB requires the `duckdb` CLI installed separately (`brew install duckdb`); set `AGENT_SQL_DUCKDB_PATH` for a custom location. Use `duckdb://` with no path for in-memory mode to query Parquet, CSV, and JSON files directly. Snowflake ad-hoc URLs use `AGENT_SQL_SNOWFLAKE_TOKEN` env var for authentication.

## Safety

- **Read-only by default**: writes require `--write` flag AND a credential with write permission
- **Defense in depth**: PG/CockroachDB uses database-level read-only transactions + session guard (libpg-query); MySQL/MariaDB uses `START TRANSACTION READ ONLY` per query + protocol-level single-statement enforcement; SQLite uses OS-level SQLITE_OPEN_READONLY; DuckDB uses `-readonly` CLI flag (engine-level, like SQLite); Snowflake uses keyword allowlist + `MULTI_STATEMENT_COUNT=1`
- **Result cap**: `query.maxRows` (default 10,000)
- **Timeout**: `query.timeout` (default 30s), override per-command with `--timeout <ms>`

## Error handling

Errors include a `fixable_by` field:
- `"agent"` -- you can fix this (typo in table name, wrong syntax). Error includes valid alternatives.
- `"human"` -- requires human action (permission change, credential setup). Do not retry.
- `"retry"` -- transient error (timeout, connection lost). Worth retrying.

## Per-command usage docs

Every command group has a `usage` subcommand with detailed, LLM-optimized docs:

```bash
agent-sql usage                    # top-level overview
agent-sql connection usage         # connection commands
agent-sql schema usage             # schema exploration commands
agent-sql query usage              # query commands
agent-sql config usage             # settings keys, defaults, validation
```

Use `agent-sql <command> usage` when you need deep detail on a specific domain before acting.

## References

- [references/commands.md](references/commands.md): full command map + all flags
- [references/output.md](references/output.md): JSON output shapes + field details
