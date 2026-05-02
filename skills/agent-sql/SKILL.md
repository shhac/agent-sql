---
name: agent-sql
description: |
  Read-only-by-default SQL CLI for AI agents. Supports 8 databases: PostgreSQL, CockroachDB, MySQL, MariaDB, SQLite, DuckDB, Snowflake, MSSQL. Use when:
  - Exploring SQL databases -- tables, columns, indexes, constraints
  - Querying data (SELECT, sample rows, count, explain plans)
  - Writing data when explicitly permitted (--write flag)
  - Checking database connections or adjusting CLI settings
  Triggers: "sql query", "sql database", "sql table", "sql schema", "postgres", "postgresql", "cockroachdb", "cockroach", "mysql", "sqlite", "duckdb", "parquet", "snowflake", "mssql", "sql server", "sqlserver", "sql connection", "query database", "sql select", "sql insert", "sql explain", "sql count", "sql sample", "database schema", "describe table", "sql columns", "sql indexes", "mariadb", "maria db"
allowed-tools: Bash(agent-sql *) Read Grep Glob
---

# SQL database exploration with `agent-sql`

`agent-sql` is a read-only-by-default SQL CLI on `$PATH`. Supports PostgreSQL, CockroachDB, MySQL, MariaDB, SQLite, DuckDB, Snowflake, and MSSQL.

Query output goes to stdout as JSONL (one JSON object per line). Non-tabular output (schema, config, admin) uses a JSON envelope. Errors go to stderr as `{ "error": "...", "hint": "...", "fixable_by": "agent|human|retry" }` with non-zero exit.

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
agent-sql run -c mssql://user:pass@host/db 'SELECT * FROM users'  # MSSQL URL (zero setup)
agent-sql run -c myalias 'SELECT * FROM users'                     # saved connection alias
```

Ad-hoc URLs accept driver-specific options as query-string params (pgx, gomysql, go-mssqldb, snowflake all parse them):

```bash
agent-sql run -c 'postgres://h/d?sslmode=require&application_name=foo' 'SELECT 1'
agent-sql run -c 'mysql://h/d?parseTime=true&tls=skip-verify' 'SELECT 1'
agent-sql run -c 'mssql://h/d?encrypt=true' 'SELECT 1'
```

For named connections, discover what's available:

```bash
agent-sql usage                          # full reference card
agent-sql connection list                # saved connections + display URLs + defaults
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
agent-sql connection list                            # saved connections + display URLs + defaults
agent-sql connection test                            # test default connection
agent-sql connection test -c prod                    # test specific connection
# Human-only setup examples:
# connection add mydb postgres://localhost:5432/myapp --credential pg-cred
# connection add mydb 'postgres://h/d?sslmode=require' --credential pg-cred  # URL options preserved
# connection add mydb mysql://h/d --option parseTime=true --credential mysql-cred
# connection add local ./data.db --option _journal_mode=wal
```

URLs with embedded credentials (`postgres://user:pass@host/db`) are rejected at add time — secrets must live in the keychain via `credential add`, not in the plaintext config file. Driver-specific options from URL query strings or repeated `--option key=value` flags are stored on the connection and threaded into the driver at connect time. Unknown options surface as the driver's own error on `connection test`.

Connection resolution: `-c` flag > `AGENT_SQL_CONNECTION` env > config default > error listing available connections. The `-c` flag accepts aliases, file paths (`.db`, `.duckdb`), or URLs (`postgres://`, `cockroachdb://`, `mysql://`, `mariadb://`, `duckdb://`, `snowflake://`, `mssql://`, `sqlserver://`). DuckDB requires the `duckdb` CLI (`brew install duckdb`); `duckdb://` with no path for in-memory mode (query Parquet/CSV/JSON files). Snowflake ad-hoc URLs use `AGENT_SQL_SNOWFLAKE_TOKEN` env var.

## Credential entry — never paste secrets

If a user pastes a database password, PAT, or other secret into chat, **do not** put it into `--password`. The secret would land in your context window, transcripts, and any downstream telemetry. Instead, instruct the user to run the credential setup themselves so the secret stays out of the LLM:

```bash
# User runs this in their own terminal — a native OS popup appears for them to type into.
agent-sql credential add <name> [--username <u>] [--write] --form
```

`--form` opens a native dialog (macOS osascript, Linux zenity/kdialog, Windows Win32). The user types directly into the OS; the LLM only sees a redacted JSON receipt:

```json
{"ok":true,"credential":"acme","username":"deploy","writePermission":false,"storage":"keychain","hint":"..."}
```

If `--form` cannot run (e.g. the user is SSH'd into a remote machine, or the host is headless), the CLI errors with `fixable_by="human"` and a hint pointing at the non-interactive fallback. Do not retry; surface the hint to the user.

The agent may set `--username` and `--write` on the user's behalf, but secret values must always come through `--form` or be typed by the user directly into their own terminal.

## Safety

- **Read-only by default**: writes require `--write` flag AND a credential with write permission
- **Defense in depth**: PG/CockroachDB uses read-only transactions + keyword guard; MySQL/MariaDB uses `START TRANSACTION READ ONLY` + single-statement enforcement; SQLite uses OS-level `SQLITE_OPEN_READONLY`; DuckDB uses `-readonly` CLI flag; Snowflake uses keyword allowlist + `MULTI_STATEMENT_COUNT=1`; MSSQL uses keyword-based guard (server-side `db_datareader` role recommended)
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
