# Go Rewrite Plan

**Status: COMPLETE** — Released as v1.2.0. All phases done, Bun implementation removed.

## Why now (historical context)

The project was 3 days old (~5,800 lines of source, ~6,200 lines of tests). Rewriting cost far less than it would later. Go eliminated every driver blocker (MSSQL, Oracle) and cut binary size 4-5x.

## Principles

1. **Red-green migration** — write Go tests first (based on existing TS test expectations), then implement until green
2. **Shared config** — Go reads the same `~/.config/agent-sql/config.json` and `credentials.json`; both tools work during migration
3. **Streaming from day one** — NDJSON output via `io.Writer`, never buffer full result sets in memory
4. **Bun project stays alive** — moved to `agent-sql-bun/` until Go reaches parity, then removed
5. **Feature parity, not feature creep** — match existing behavior exactly before adding MSSQL/Oracle

## Scope

| Area | TS Lines | Go Estimate | Notes |
|---|---|---|---|
| Drivers (7 types) | ~1,100 | ~1,500 | Go is more verbose; add MSSQL natively |
| CLI (25 subcommands) | ~900 | ~1,200 | cobra framework |
| Lib (output, errors, config, creds) | ~1,200 | ~1,400 | Streaming NDJSON, keychain via `security` CLI |
| Driver resolution | ~500 | ~500 | Same logic, same URL patterns |
| **Total source** | **~5,800** | **~4,600** | Less code with `database/sql` unification |
| Tests | ~6,200 | ~4,000 | Go table-driven tests are more compact |

## Directory structure

```
agent-sql/
  cmd/
    agent-sql/              # main entry point
      main.go
  internal/
    cli/                    # cobra commands
      root.go               # global flags (-c, --format, --expand, --full, --timeout)
      run.go                # top-level `run` alias
      query/                # query run/sample/explain/count/usage
      schema/               # schema tables/describe/indexes/constraints/search/dump/usage
      connection/           # connection add/remove/update/list/test/set-default/usage
      credential/           # credential add/remove/list/usage
      config/               # config get/set/reset/list-keys/usage
      usage.go              # top-level LLM reference card
    driver/
      driver.go             # DriverConnection interface + shared types
      resolve.go            # alias/URL/file path → driver
      detect.go             # URL scheme + file extension detection
      pg/                   # pgx — also used by cockroachdb
      cockroachdb/          # thin wrapper over pg (port 26257, db defaultdb)
      mysql/                # go-sql-driver/mysql — also used by mariadb
      mariadb/              # thin wrapper over mysql (max_statement_time)
      sqlite/               # modernc.org/sqlite (pure Go, no CGo)
      duckdb/               # subprocess (same pattern as TS)
      snowflake/            # gosnowflake driver
      mssql/                # go-mssqldb (new — pure Go, no companion binary needed)
    config/                 # config.json + credentials.json I/O
    credential/             # keychain (macOS) + file fallback
    output/                 # NDJSON streaming, JSON, YAML, CSV formatters
    truncation/             # @truncated metadata
    errors/                 # error classification + fixable_by
    guard/                  # PG session guard via pg_query_go
  go.mod
  go.sum
  Makefile                  # build targets: build, test, lint, release
  agent-sql-bun/            # original TS project (removed after parity)
    src/
    test/
    package.json
    ...
```

## Go dependencies

| Package | Purpose | CGo? |
|---|---|---|
| `github.com/spf13/cobra` | CLI framework | No |
| `github.com/jackc/pgx/v5` | PostgreSQL (+ CockroachDB) | No |
| `github.com/go-sql-driver/mysql` | MySQL (+ MariaDB) | No |
| `modernc.org/sqlite` | SQLite | **No** (pure Go!) |
| `github.com/microsoft/go-mssqldb` | MSSQL | No |
| `github.com/snowflakedb/gosnowflake` | Snowflake | No |
| `github.com/pganalyze/pg_query_go/v5` | PG parser for read-only guard | **Yes** (CGo) |
| `gopkg.in/yaml.v3` | YAML output | No |

Only the PG read-only guard needs CGo. Everything else is pure Go → easy cross-compilation for non-PG-guard platforms. Consider making the guard optional at build time.

## Migration phases

### Phase 0: Prep (current repo)
- [ ] Move Bun project to `agent-sql-bun/`
- [ ] `go mod init github.com/shhac/agent-sql`
- [ ] Set up `cmd/agent-sql/main.go` with cobra root command
- [ ] Set up `Makefile` with build/test/lint targets
- [ ] Verify `agent-sql-bun/` still builds and tests pass from its new location

### Phase 1: Config + output (no drivers)
Red-green: port the config and output tests first, then implement.
- [ ] `internal/config/` — read/write config.json (same format, same path)
- [ ] `internal/credential/` — read credentials.json + macOS keychain via `security` CLI
- [ ] `internal/output/` — streaming NDJSON writer, JSON, YAML, CSV formatters
- [ ] `internal/truncation/` — @truncated metadata with streaming support
- [ ] `internal/errors/` — error classification with fixable_by
- [ ] Tests: config round-trip, credential read/write, output format parity, truncation

### Phase 2: Driver interface + SQLite
SQLite first — no network, fast tests, validates the full vertical slice.
- [ ] `internal/driver/driver.go` — DriverConnection interface (same shape as TS)
- [ ] `internal/driver/resolve.go` — connection resolution chain
- [ ] `internal/driver/detect.go` — URL schemes + file extensions
- [ ] `internal/driver/sqlite/` — modernc.org/sqlite driver
- [ ] CLI: `query run`, `schema tables/describe/indexes/constraints/search/dump`
- [ ] CLI: `connection add/list/test`, `config get/set`
- [ ] Integration test: full CLI → SQLite → NDJSON output
- [ ] **Parity check**: run TS integration tests against Go binary, compare output

### Phase 3: PostgreSQL + CockroachDB
- [ ] `internal/driver/pg/` — pgx driver with read-only guard
- [ ] `internal/guard/` — pg_query_go allowlist (port of pg-session-guard.ts)
- [ ] `internal/driver/cockroachdb/` — thin wrapper (port 26257, db defaultdb)
- [ ] Tests: PG contract tests, guard tests, CockroachDB URL detection

### Phase 4: MySQL + MariaDB
- [ ] `internal/driver/mysql/` — go-sql-driver/mysql with START TRANSACTION READ ONLY
- [ ] `internal/driver/mariadb/` — thin wrapper (max_statement_time)
- [ ] Tests: MySQL contract tests, MariaDB timeout variant

### Phase 5: DuckDB (subprocess)
- [ ] `internal/driver/duckdb/` — port subprocess + NDJSON parsing
- [ ] Same pattern: spawn `duckdb` CLI, parse jsonlines, -readonly flag
- [ ] Tests: data type tests, NDJSON edge cases, file queries

### Phase 6: Snowflake
- [ ] `internal/driver/snowflake/` — gosnowflake driver (or keep HTTP API approach)
- [ ] Read-only guard (keyword allowlist + single-statement enforcement)
- [ ] Tests: Snowflake contract tests

### Phase 7: MSSQL (new)
- [ ] `internal/driver/mssql/` — go-mssqldb driver
- [ ] Read-only guard (keyword-based, since MSSQL has no SET TRANSACTION READ ONLY)
- [ ] URL scheme: `mssql://` and `sqlserver://`
- [ ] Tests: MSSQL contract tests, read-only guard tests
- [ ] Update all docs, SKILL.md, README for 8th driver

### Phase 8: Remaining CLI + parity
- [ ] All remaining CLI commands (credential, connection update/remove/set-default, config reset/list-keys)
- [ ] All `usage` subcommands (copy LLM-optimized text from TS)
- [ ] `--format`, `--expand`, `--full`, `--timeout`, `--compact` flags
- [ ] `--write` mode across all drivers
- [ ] **Full parity test**: run every TS integration test against Go binary

### Phase 9: Cleanup
- [ ] Remove `agent-sql-bun/`
- [ ] Update build scripts, release process, Homebrew formula
- [ ] Update CLAUDE.md, skills, docs for Go
- [ ] Release as v1.0.0

## Streaming design

TS buffers results then formats. Go should stream from the start:

```go
// Writer interface — each formatter implements this
type ResultWriter interface {
    WriteHeader(columns []string) error
    WriteRow(row map[string]any) error
    WritePagination(hasMore bool, count int) error
    Flush() error
}

// NDJSON writer streams directly to stdout
type NdjsonWriter struct {
    w       *bufio.Writer
    trunc   *Truncator
}

func (n *NdjsonWriter) WriteRow(row map[string]any) error {
    truncated := n.trunc.Apply(row)
    row["@truncated"] = truncated
    return json.NewEncoder(n.w).Encode(row)
}
```

Each driver's `Query()` returns a `RowIterator` instead of a full `[]map[string]any`:

```go
type RowIterator interface {
    Columns() []string
    Next() bool
    Row() (map[string]any, error)
    Close() error
}
```

This lets us stream 10,000 rows without buffering them all in memory.

## Config compatibility

Both tools read the same files:
- `~/.config/agent-sql/config.json` — connections + settings
- `~/.config/agent-sql/credentials.json` — credential index
- macOS Keychain entries — same service name format

The Go binary is a drop-in replacement. Users don't need to reconfigure anything.

## Red-green workflow

For each phase:
1. **Red**: Write Go tests based on the existing TS test expectations (same inputs, same expected outputs)
2. **Green**: Implement until tests pass
3. **Parity**: Run `agent-sql-bun` and `agent-sql` (Go) against the same inputs, diff outputs
4. **Move on**: Only proceed to next phase when parity is confirmed

The TS test suite is the specification. Every test file maps to a Go test file.

## Binary size target

| Platform | TS/Bun (current) | Go (estimated) |
|---|---|---|
| darwin-arm64 | 59 MB | ~15 MB |
| darwin-x64 | 63 MB | ~18 MB |
| linux-x64 | 95 MB | ~20 MB |
| windows-x64 | 110 MB | ~18 MB |

With CGo (PG guard): add ~2-3 MB. Without CGo: pure Go, easy cross-compile.

## What we gain

- MSSQL native support (no companion binary)
- 4-5x smaller binaries
- Faster startup (~5ms vs ~50ms)
- No more "blocked by Bun" for any database
- Single language for everything
- Better cross-compilation story

## What we lose

- Bun's developer experience (fast iteration, native SQL tagged templates)
- Some development velocity (Go is more verbose)
- `bun test` speed (Go tests are fast but `bun test` is exceptionally fast)
