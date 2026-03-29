# agent-sql

Read-only-by-default SQL CLI for AI agents. Go, compiled to standalone binaries.

## Design docs

Design docs live in `design-docs/` (gitignored, local-only). If present:

- `design-docs/go-rewrite.md` — Go rewrite plan with phased migration strategy
- `design-docs/subprocess-drivers.md` — subprocess driver pattern design
- `design-docs/TASKS.md` — implementation task tracker

## Runtime

- **Go** — single compiled binary, no runtime dependencies
- **pgx** — native PostgreSQL driver (also used by CockroachDB)
- **go-sql-driver/mysql** — native MySQL driver (also used by MariaDB)
- **modernc.org/sqlite** — pure Go SQLite driver (no CGo)
- **go-mssqldb** — native MSSQL driver (pure Go, SQL auth only — no Azure SDK)
- **DuckDB** — subprocess driver, spawns `duckdb` CLI with NDJSON output (requires CLI installed separately)
- **Snowflake** — HTTP REST API v2 with PAT authentication (lightweight, no gosnowflake)

## Key design decisions

- **Read-only by default** — credentials stored in macOS Keychain, not in config file. Config has zero sensitive data.
- **PG/CockroachDB read-only** — keyword-based guard + `SET default_transaction_read_only = on` + `BEGIN READ ONLY` per query. Defense in depth.
- **SQLite readonly** — `SQLITE_OPEN_READONLY` via `?mode=ro` DSN. OS-level enforcement.
- **MySQL/MariaDB readonly** — `START TRANSACTION READ ONLY` per query + session-level enforcement. MariaDB uses `max_statement_time` for timeout (vs MySQL's `MAX_EXECUTION_TIME`).
- **DuckDB readonly** — `-readonly` CLI flag, engine-level enforcement (like SQLite). No guard needed.
- **Snowflake readonly** — client-side keyword allowlist + `MULTI_STATEMENT_COUNT=1`.
- **MSSQL readonly** — keyword-based guard. Server-side `db_datareader` role recommended for production.
- **Subprocess drivers** — pattern for databases without lightweight native drivers. Spawns CLI tool with NDJSON output. See `design-docs/subprocess-drivers.md`.
- **Streaming output** — NDJSON written row-by-row via `ResultWriter` interface. Never buffers full result sets for streaming formats.
- **Output** — NDJSON to stdout (default), errors to stderr as JSON. NULLs preserved. `@truncated` per row. `fixable_by` on errors.
- **Skill boundary** — query, schema, config, connection list/test, usage exposed to LLMs. Credential and connection mutation are human-only.
- **Pure Go** — no CGo dependencies. Keyword guard instead of `pg_query_go`. Cross-compilation is trivial.

## Dev tools

- **Build**: `make build`
- **Test**: `make test` (full) / `make test-short` (skip integration)
- **Lint**: `make lint` (golangci-lint)
- **Format**: `make fmt` (gofmt + goimports)
- **Dev runner**: `make dev ARGS="run -c ./data.db 'SELECT 1'"`
- **Vet**: `make vet`

## Architecture

```
cmd/
  agent-sql/                  # CLI entry point (main.go)
internal/
  cli/                        # cobra commands
    root.go                   # global flags (-c, --format, --expand, --full, --timeout)
    run.go                    # top-level `run` alias
    usage.go                  # LLM reference card
    query/                    # query run/sample/explain/count/usage
    schema/                   # schema tables/describe/indexes/constraints/search/dump/usage
    connection/               # connection add/remove/update/list/test/set-default/usage
    credential/               # credential add/remove/list/usage
    config/                   # config get/set/reset/list-keys/usage
  driver/
    driver.go                 # Connection interface, QueryResult, schema types
    resolve.go                # Driver resolution from config / URL / file path
    detect.go                 # URL scheme and file extension detection
    guard.go                  # Keyword-based read-only guard (shared by PG, MSSQL)
    pg/                       # PostgreSQL (pgx) — also used by CockroachDB
    cockroachdb/              # Thin wrapper over pg (port 26257, db defaultdb)
    mysql/                    # MySQL (go-sql-driver/mysql) — also used by MariaDB
    mariadb/                  # Thin wrapper over mysql (max_statement_time)
    sqlite/                   # SQLite (modernc.org/sqlite, pure Go)
    duckdb/                   # DuckDB (subprocess — spawns duckdb CLI with NDJSON output)
    snowflake/                # Snowflake (HTTP REST API v2)
    mssql/                    # MSSQL (go-mssqldb)
  config/                     # Config file I/O (connections + settings)
  credential/                 # Credential storage (Keychain on macOS, file fallback)
  output/                     # ResultWriter interface, NDJSON/JSON/YAML/CSV formatters
  truncation/                 # @truncated decorator (ResultWriter wrapper)
  errors/                     # QueryError type, per-driver error classification
```

## Key patterns

- **Connection interface**: Every driver implements `driver.Connection`. CLI commands call interface methods — zero driver-specific branching.
- **context.Context**: All driver methods take `context.Context` for timeout/cancellation (except `QuoteIdent` and `Close`).
- **ResultWriter**: Streaming output interface. NDJSON writes rows as they arrive. JSON/YAML buffer internally. Truncation is a decorator wrapping the inner writer.
- **Error classification**: `errors.QueryError` with `FixableBy` field (`agent`/`human`/`retry`). Drivers pre-classify; `errors.Classify()` handles the rest.
- **Connection resolution**: `-c` accepts aliases, file paths (`.db`, `.duckdb`), or URLs (postgres://, cockroachdb://, mysql://, mariadb://, duckdb://, snowflake://, mssql://, sqlserver://). Chain: `-c` flag > `AGENT_SQL_CONNECTION` env > config default.
- **Keyword guard**: Shared read-only guard for PG and MSSQL. Blocks statements starting with INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, TRUNCATE, MERGE, GRANT, REVOKE. PG also uses server-side `BEGIN READ ONLY`.
- **Thin wrappers**: CockroachDB wraps PG (different defaults). MariaDB wraps MySQL (different timeout syntax).

## Releasing

Use `/release <patch|minor|major>` to build, tag, and publish.
The Homebrew tap lives at `../homebrew-tap`. Always `cd` back after updating it.

## After making changes

When changing CLI behavior, flags, output shape, or commands, also update the applicable docs:
- `internal/cli/usage.go` — top-level LLM reference card
- `internal/cli/*/usage.go` — per-command usage text
- `skills/agent-sql/SKILL.md` — Claude Code skill definition
- `skills/agent-sql/references/commands.md` — full command reference
- `skills/agent-sql/references/output.md` — output format reference
- `README.md` — user-facing documentation

### Adding a new driver — checklist

Every file below must be updated. Use the existing drivers as a model.

**Core:**
- [ ] `internal/driver/driver.go` — add to `Driver` constants
- [ ] `internal/driver/<name>/` — driver implementation
- [ ] `internal/driver/detect.go` — URL pattern and/or file extension detection
- [ ] `internal/driver/resolve.go` — builder entry, write permission check, driver name in error messages

**CLI text (every place that lists drivers):**
- [ ] `internal/cli/connection/add.go` — `--driver` flag description, connection string parsing
- [ ] `internal/cli/connection/update.go` — `--driver` flag description
- [ ] `internal/cli/connection/usage.go` — examples, AD-HOC section
- [ ] `internal/cli/credential/add.go` — hint text
- [ ] `internal/cli/usage.go` — driver list, ad-hoc examples, CONNECTION section

**Documentation:**
- [ ] `skills/agent-sql/SKILL.md` — description, triggers, driver list, quick start, safety section
- [ ] `skills/agent-sql/references/commands.md` — `-c` flag description
- [ ] `README.md` — tagline, quick start, safety table, connection resolution
- [ ] `AGENTS.md` — this file (runtime, design decisions, architecture)

**Tests:**
- [ ] `internal/driver/<name>/<name>_test.go` — query, schema, readonly, data types, error classification
- [ ] `internal/driver/detect_test.go` — URL detection, file extension detection
- [ ] `internal/driver/resolve_test.go` — write permission check

