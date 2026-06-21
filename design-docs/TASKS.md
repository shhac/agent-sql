# Go Rewrite — Task Tracker

Migrating agent-sql from TypeScript/Bun to Go. Red-green pattern: write tests first, then implement.
See `design-docs/go-rewrite.md` for full plan and design decisions.

**Status: COMPLETE** — Released as v1.2.0. 467 tests, 8 drivers, full parity verified.

## Phase 0: Prep
- [x] Move Bun project to `agent-sql-bun/`
- [x] Update `.gitignore` for Go
- [x] Commit the move
- [x] `go mod init github.com/shhac/agent-sql`
- [x] Set up `cmd/agent-sql/main.go` with cobra root command
- [x] Set up `Makefile` with build/test/lint targets
- [x] Create `AGENTS.md` (Go-focused), symlink `CLAUDE.md` -> `AGENTS.md`
- [x] Verify `agent-sql-bun/` still builds and tests pass

## Phase 1: Config + Output (no drivers)
- [x] `internal/config/config.go` — read/write config.json (same format, same path)
- [x] `internal/config/config_test.go` — round-trip, settings get/set/reset
- [x] `internal/credential/credential.go` — read credentials.json + macOS keychain
- [x] `internal/credential/credential_test.go`
- [x] `internal/output/ndjson.go` — streaming NDJSON ResultWriter
- [x] `internal/output/json.go` — JSON envelope ResultWriter
- [x] `internal/output/yaml.go` — YAML ResultWriter
- [x] `internal/output/csv.go` — CSV ResultWriter
- [x] `internal/output/compact.go` — typed NDJSON compact mode
- [x] `internal/output/output.go` — WriteError, PrintJSON, PrintYAML, format resolution
- [x] `internal/output/output_test.go` + `compact_test.go` — format parity + edge cases
- [x] `internal/truncation/truncation.go` — @truncated decorator (ResultWriter wrapper)
- [x] `internal/truncation/truncation_test.go`
- [x] `internal/errors/errors.go` — QueryError type, Classify function
- [x] `internal/errors/errors_test.go`
- [x] `internal/errors/hints.go` — shared error hint constants

## Phase 2: Driver Interface + SQLite + CLI
- [x] `internal/driver/driver.go` — Connection interface, QueryResult, RowIterator, StreamingQuerier
- [x] `internal/driver/helpers.go` — NormalizeValue, QuoteIdentDot, SplitSchemaTable, MapConstraintType
- [x] `internal/driver/helpers_test.go`
- [x] `internal/driver/sqlrows.go` — ScanAllRows, SQLRowsIterator
- [x] `internal/driver/detect.go` — URL schemes + file extensions
- [x] `internal/driver/detect_test.go`
- [x] `internal/driver/guard.go` — keyword-based read-only guard (shared by PG, MSSQL)
- [x] `internal/driver/guard_test.go` — including bypass vector documentation
- [x] `internal/driver/iterator_test.go` — SliceIterator, Collect, RowIterator error handling
- [x] `internal/driver/sqlite/sqlite.go` + `schema.go` — modernc.org/sqlite driver
- [x] `internal/driver/sqlite/sqlite_test.go` — query, schema, readonly, data types, write mode
- [x] `internal/resolve/resolve.go` — connection resolution (separated from driver to avoid import cycle)
- [x] `internal/resolve/resolve_test.go` — checkWritePermission, parseURL, parsePort
- [x] CLI: root command with global flags (-c, --format, --expand, --full, --timeout, --compact)
- [x] CLI: `run` (top-level alias), `usage`
- [x] CLI: `query run/sample/explain/count/usage`
- [x] CLI: `schema tables/describe/indexes/constraints/search/dump/usage`
- [x] CLI: `connection add/remove/update/list/test/set-default/usage`
- [x] CLI: `credential add/remove/list/usage`
- [x] CLI: `config get/set/reset/list-keys/usage`
- [x] CLI: `internal/cli/shared/shared.go` — MakeContext, WithConnection, GlobalFlags
- [x] CLI: `internal/cli/connection/parse.go` + `parse_test.go` — URL parsing extracted + tested
- [x] Integration test: full CLI -> SQLite -> NDJSON output
- [x] **Parity check**: compare Go output vs Bun output for SQLite queries

## Phases 3-7: Drivers (built in parallel)

### Phase 3: PostgreSQL + CockroachDB
- [x] `internal/driver/pg/pg.go` + `schema.go` + `errors.go` — pgx driver with keyword guard + BEGIN READ ONLY + QueryStream
- [x] `internal/driver/pg/pg_test.go`
- [x] `internal/driver/cockroachdb/cockroachdb.go` — thin wrapper (port 26257, db defaultdb)
- [x] `internal/driver/cockroachdb/cockroachdb_test.go`

### Phase 4: MySQL + MariaDB
- [x] `internal/driver/mysql/mysql.go` + `schema.go` + `errors.go` — go-sql-driver/mysql + START TRANSACTION READ ONLY + QueryStream
- [x] `internal/driver/mysql/mysql_test.go`
- [x] `internal/driver/mariadb/mariadb.go` — thin wrapper (max_statement_time)
- [x] `internal/driver/mariadb/mariadb_test.go`

### Phase 5: DuckDB (subprocess)
- [x] `internal/driver/duckdb/duckdb.go` + `exec.go` + `errors.go` — subprocess + NDJSON parsing
- [x] `internal/driver/duckdb/duckdb_test.go` — query, schema, readonly, data types, NDJSON edge cases, file queries

### Phase 6: Snowflake (HTTP REST API)
- [x] `internal/driver/snowflake/snowflake.go` + `client.go` + `parse.go` + `errors.go` + `guard.go` — HTTP REST API v2
- [x] `internal/driver/snowflake/snowflake_test.go`
- [x] `internal/testutil/mockservers/snowflake.go` + `snowflake_test.go` — mock server for testing

### Phase 7: MSSQL (new driver)
- [x] `internal/driver/mssql/mssql.go` + `schema.go` + `errors.go` — go-mssqldb with keyword guard + QueryStream
- [x] `internal/driver/mssql/mssql_test.go`
- [x] URL schemes: mssql://, sqlserver://

## Phase 8: Full Parity + Polish
- [x] `--write` mode verified across all drivers
- [x] All `usage` subcommands with LLM-optimized text
- [x] Full parity test suite — 57/57 across SQLite, DuckDB, PG, MySQL, MariaDB
- [x] goreleaser config for cross-compilation
- [x] Update README, SKILL.md, commands.md, output.md for Go + MSSQL
- [x] True streaming (RowIterator + QueryStream) for SQL drivers
- [x] All 5 output formats: NDJSON, JSON, YAML, CSV, Compact
- [x] 2 rounds of /improve-code-structure

## Phase 9: Cleanup
- [x] Remove `agent-sql-bun/`
- [x] Final AGENTS.md/CLAUDE.md update
- [x] Release v1.0.0
- [x] Release v1.1.0 (output formats)
- [x] Release v1.2.0 (streaming + compact + structural improvements)
