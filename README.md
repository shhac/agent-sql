# agent-sql

Read-only-by-default SQL CLI for AI agents.

- **Structured output** -- JSONL to stdout by default, errors to stderr as JSON
- **LLM-optimized** -- `agent-sql usage` prints concise docs for agent consumption
- **Read-only by default** -- write access requires explicit opt-in per credential and per query
- **Defense in depth** -- driver-level, parser-level, and credential-level enforcement layers
- **PostgreSQL, CockroachDB, MySQL, MariaDB, SQLite, DuckDB, Snowflake, and MSSQL** -- eight drivers, one interface
- **Single binary** -- compiled Go binary, no runtime dependencies (DuckDB requires `duckdb` CLI installed separately)

## Installation

```bash
brew install shhac/tap/agent-sql
```

### Other options

Download a binary from [GitHub releases](https://github.com/shhac/agent-sql/releases), or install via `go install`:

```bash
go install github.com/shhac/agent-sql/cmd/agent-sql@latest
```

This installs a binary with version set to `dev`. For a version-stamped build, clone and build with ldflags:

```bash
# Requires Go 1.22+
# Binary is placed in $GOPATH/bin (or $GOBIN if set)
```

Or clone and build with goreleaser:

```bash
git clone https://github.com/shhac/agent-sql.git
cd agent-sql
make build
```

### Claude Code / AI agent skill

```bash
npx skills add shhac/agent-sql
```

This installs the `agent-sql` skill so Claude Code (and other AI agents) can discover and use `agent-sql` automatically. See [skills.sh](https://skills.sh) for details.

## Quick start

### 1. Connect

The `-c` flag accepts file paths, connection URLs, or saved aliases:

```bash
# SQLite — just point at a file, no setup needed
agent-sql run -c ./data.db 'SELECT * FROM users'

# PostgreSQL / CockroachDB / MySQL — inline URL
agent-sql run -c postgres://user:pass@localhost/myapp 'SELECT * FROM users'
agent-sql run -c cockroachdb://user:pass@localhost:26257/myapp 'SELECT * FROM users'
agent-sql run -c mysql://user:pass@localhost/myapp 'SELECT * FROM orders'
agent-sql run -c mariadb://user:pass@localhost/myapp 'SELECT * FROM orders'

# DuckDB — file path or in-memory (requires duckdb CLI: brew install duckdb)
agent-sql run -c ./analytics.duckdb 'SELECT * FROM events'
agent-sql run -c duckdb:// "SELECT * FROM 'data/*.parquet'"

# Snowflake — inline URL + token via env var
AGENT_SQL_SNOWFLAKE_TOKEN=<pat> agent-sql run \
  -c 'snowflake://myorg-myaccount/MY_DB/PUBLIC?warehouse=COMPUTE_WH' 'SELECT 1'

# MSSQL / SQL Server
agent-sql run -c mssql://user:pass@localhost/myapp 'SELECT * FROM users'
agent-sql run -c sqlserver://user:pass@localhost/myapp 'SELECT * FROM users'
```

For databases you use repeatedly, save a named connection. The second argument is a connection string — driver, host, port, and database are auto-detected:

```bash
# PostgreSQL — credential first, then connection with URL
agent-sql credential add pg-cred --username app --password secret
agent-sql connection add mydb postgres://localhost:5432/myapp --credential pg-cred

# MySQL
agent-sql credential add mysql-cred --username app --password secret
agent-sql connection add mydb mysql://localhost/myapp --credential mysql-cred

# MariaDB
agent-sql credential add mariadb-cred --username app --password secret
agent-sql connection add mydb mariadb://localhost/myapp --credential mariadb-cred

# SQLite — just a file path, no credential needed
agent-sql connection add local ./data.db

# DuckDB — file path, no credential needed (requires duckdb CLI)
agent-sql connection add analytics ./analytics.duckdb

# Snowflake — PAT as password, account can look like orgname-accountname or account.region
agent-sql credential add sf-cred --password <pat_secret>
agent-sql connection add sf-prod \
  "snowflake://myorg-myaccount/MY_DB/PUBLIC?warehouse=COMPUTE_WH&role=MY_ROLE" \
  --credential sf-cred

# MSSQL / SQL Server
agent-sql credential add mssql-cred --username app --password secret
agent-sql connection add mydb mssql://localhost/myapp --credential mssql-cred

# Verify
agent-sql connection test
```

Flags (`--driver`, `--host`, `--port`, etc.) still work for explicit setup and override anything parsed from the connection string.

### 2. Explore schema

```bash
agent-sql schema tables
agent-sql schema describe users
agent-sql schema indexes users
agent-sql schema constraints users
agent-sql schema search email
```

### 3. Query data

```bash
agent-sql run "SELECT * FROM users WHERE active = true" --limit 10
agent-sql query sample users --limit 5
agent-sql query count users --where "age >= 21"
agent-sql query explain "SELECT * FROM orders JOIN users ON orders.user_id = users.id"
```

## Command map

```text
agent-sql [-c <alias>] [--format jsonl|json|yaml|csv] [--full] [--expand <fields>] [--timeout <ms>]
├── credential                                         # set up credentials first
│   ├── add <alias> --username <u> --password <p> [--write]
│   ├── remove <alias> [--force]
│   ├── list
│   └── usage
├── connection                                         # then create connections that reference them
│   ├── add <alias> [connection-string] [--credential <name>] [--driver --host --port ...]
│   ├── update <alias> [--credential <name>] [--no-credential] [--database <db>]
│   ├── remove <alias>
│   ├── list
│   ├── test [alias]
│   ├── set-default <alias>
│   └── usage
├── config
│   ├── get <key>
│   ├── set <key> <value>
│   ├── reset
│   ├── list-keys
│   └── usage
├── schema
│   ├── tables [--include-system]
│   ├── describe <table> [--detailed]
│   ├── indexes [table]
│   ├── constraints [table] [--type]
│   ├── search <pattern>
│   ├── dump [--tables] [--include-system]
│   └── usage
├── query
│   ├── run "<sql>" [--limit] [--write] [--compact]
│   ├── sample <table> [--limit] [--where]
│   ├── explain "<sql>" [--analyze]
│   ├── count <table> [--where]
│   └── usage
├── run "<sql>" [--limit] [--write] [--compact]    # top-level alias for query run
└── usage                                           # LLM-optimized docs
```

Each command group has a `usage` subcommand for detailed, LLM-friendly documentation (e.g., `agent-sql query usage`). The top-level `agent-sql usage` gives a broad overview.

## Safety model

agent-sql is read-only by default with defense in depth:

| Layer | PostgreSQL / CockroachDB | MySQL / MariaDB | SQLite | DuckDB | Snowflake | MSSQL |
| --- | --- | --- | --- | --- | --- | --- |
| **Credential** | `--write` flag on `credential add` grants write permission | Same | Credential-less connections are read-only by default | Same as SQLite | Same as PG/MySQL | Same as PG/MySQL |
| **Query flag** | `--write` required on each write query | Same | Same | Same | Same | Same |
| **SQL parser** | `libpg-query` (PG's actual parser, WASM) validates statement types against an allowlist. CockroachDB uses the PG wire protocol — guard fails closed for CRDB-specific syntax. | `START TRANSACTION READ ONLY` per query + protocol-level single-statement enforcement | N/A -- `SQLITE_OPEN_READONLY` is OS-level enforcement | N/A -- `-readonly` CLI flag is engine-level enforcement (like SQLite) | Client-side keyword allowlist + `MULTI_STATEMENT_COUNT=1` | Keyword-based read-only guard. Server-side `db_datareader` role recommended. |
| **Result cap** | `query.maxRows` (default 10,000) | Same | Same | Same | Same | Same |
| **Timeout** | `query.timeout` (default 30s), per-command `--timeout <ms>` | Same | Same | Same | Same | Same |

Write operations require both a credential with `writePermission` and the `--write` flag on the query itself. This two-gate design prevents accidental writes even when credentials allow them.

## Output

- Default output is JSONL to stdout -- one JSON object per line, no envelope. Use `--format json`, `--format yaml`, or `--format csv` for alternate formats.
- JSONL applies to tabular results (`query run`, `query sample`). Each line is `{"col": val, ..., "@truncated": null}`. When more rows exist, the last line is `{"@pagination": {"hasMore": true, "rowCount": N}}`.
- Non-tabular output (schema, config, explain, count, connection/credential admin) uses JSON envelope regardless of format setting
- CSV applies to tabular results only; non-tabular commands fall back to JSON
- Errors always go to stderr as JSON `{ "error": "...", "fixable_by": "agent"|"human" }` with non-zero exit code
- NULLs preserved in query results, empty fields pruned in admin output
- Long strings truncated with per-row `@truncated` metadata showing original lengths
- `--compact` mode uses parallel arrays (column names + row arrays) for reduced token count

```bash
agent-sql run "SELECT * FROM users"                        # JSONL output (default)
agent-sql --format json run "SELECT * FROM users"          # JSON envelope output
agent-sql --full run "SELECT * FROM users"                 # expand all fields
agent-sql --expand name,bio run "SELECT * FROM users"      # expand specific fields
agent-sql --format yaml run "SELECT * FROM users"          # YAML output
agent-sql --format csv run "SELECT * FROM users"           # CSV output
agent-sql config set defaults.format json                  # persistent format default
```

## Configuration

Persistent settings stored in `~/.config/agent-sql/config.json`:

| Key | Default | Description |
| --- | --- | --- |
| `defaults.format` | jsonl | Default output format (jsonl/json/yaml/csv) |
| `defaults.limit` | 20 | Default row limit for queries |
| `query.timeout` | 30000 | Query timeout in milliseconds |
| `query.maxRows` | 10000 | Maximum rows per query |
| `truncation.maxLength` | 200 | String truncation threshold |

```bash
agent-sql config set defaults.limit 50
agent-sql config get query.timeout
agent-sql config list-keys           # all keys with defaults and ranges
agent-sql config reset               # reset all to defaults
```

## Connection resolution

Resolution order: `-c` flag > `AGENT_SQL_CONNECTION` env > config default. The `-c` flag accepts saved aliases, file paths (e.g. `./data.db`, `./data.duckdb`), and connection URLs (e.g. `postgres://user:pass@host/db`, `mariadb://user:pass@host/db`, `duckdb://`, `mssql://user:pass@host/db`, `sqlserver://user:pass@host/db`).

## Environment variables

| Variable | Description |
| --- | --- |
| `AGENT_SQL_CONNECTION` | Default connection alias |
| `AGENT_SQL_DUCKDB_PATH` | Path to `duckdb` CLI binary (default: found via `$PATH`) |
| `AGENT_SQL_SNOWFLAKE_TOKEN` | PAT for ad-hoc Snowflake connections |
| `XDG_CONFIG_HOME` | Override config directory (default: `~/.config`) |

## Development

```bash
make build                   # build binary
make dev ARGS="--help"       # run in dev mode
make test                    # run tests
make lint                    # lint
```

## License

MIT
