# Subprocess Drivers

Design for supporting databases via external CLI tools instead of native drivers.

## Motivation

Some databases have large native libraries (DuckDB: 60-107 MB) or no Bun-compatible drivers (MSSQL, Oracle). CLI subprocess drivers avoid binary bloat and Bun compatibility issues by delegating to external CLI tools that users install independently.

## Pattern

A subprocess driver implements the same `DriverConnection` interface as native drivers. Instead of linking a database library, it spawns the database's CLI tool with JSON output mode, parses the results, and maps them to the shared types.

```
agent-sql → spawn CLI subprocess → parse JSON stdout → DriverConnection interface
```

### Detection

On connection, check if the CLI tool is on PATH. If not, throw an actionable error:

```
DuckDB CLI not found. Install with: brew install duckdb
```

### Query execution

Each query spawns a subprocess with the SQL passed via `-c` flag. The CLI runs in JSON output mode, and the result is parsed into `QueryResult`.

### Schema discovery

Schema methods (`getTables`, `describeTable`, etc.) use `information_schema` queries or database-specific introspection, executed through the same subprocess mechanism.

### Read-only enforcement

Depends on the CLI tool's capabilities:
- **DuckDB**: `-readonly` flag — engine-level, no guard needed (like SQLite)
- **Others**: may need `SET TRANSACTION READ ONLY` prefix or parser-based guard

### Error handling

- Non-zero exit code → parse stderr for error message
- Map to `fixable_by` classification where possible
- CLI not found → `fixable_by: "human"` with install hint

### Timeouts

External `timeout` via `Bun.spawn` AbortSignal, since CLI tools may not have native timeout flags.

## Candidates

| Database | CLI Tool | JSON Output | Read-Only Flag | Status |
|---|---|---|---|---|
| DuckDB | `duckdb` | Yes (array + NDJSON) | `-readonly` | **Implementing** |
| Oracle | SQLcl (`sql`) | Yes (`SET SQLFORMAT JSON`) | No (use `SET TRANSACTION READ ONLY`) | Future — 2-5s JVM startup |
| MSSQL | `sqlcmd` (Go) | No | No | Blocked — no structured output |

## Future: sibling binaries

The subprocess pattern opens the door for shipping optional companion binaries (e.g. `agent-sql-duckdb`) that wrap database CLIs with JSON output for databases that lack it (like MSSQL). These would be separate installs via brew or npm.

---

# DuckDB Driver (subprocess)

## Connection modes

DuckDB supports two distinct use cases:

1. **Database file**: `duckdb -readonly /path/to/db.duckdb` — traditional database access
2. **Direct file query**: `duckdb -c "SELECT * FROM 'data.parquet'"` — in-memory, no database file needed

Both use the same subprocess mechanism. For direct file queries, no `-readonly` flag is used (in-memory mode rejects it), but the query itself is inherently read-only since there's no database to write to.

## URL scheme

```
duckdb:///path/to/database.duckdb     # database file (absolute)
duckdb://./relative/path.duckdb       # database file (relative)
```

File extensions for auto-detection: `.duckdb`

Direct file queries (Parquet, CSV, JSON) use SQLite-style file path detection or explicit SQL:
```bash
agent-sql run -c duckdb:// "SELECT * FROM '/path/to/data.parquet'"
```

## CLI invocation

```bash
# Query with database file
duckdb -json -readonly /path/to/db.duckdb -c "SELECT * FROM users"

# Query without database (in-memory, for file queries)
duckdb -json -c "SELECT * FROM 'data.parquet'"
```

## JSON output format

DuckDB outputs a JSON array:
```json
[{"id":1,"name":"Alice"},
{"id":2,"name":"Bob"}]
```

NULLs are preserved as JSON `null`. Types map naturally (integers → numbers, strings → strings, booleans → booleans).

## Error handling

- Exit code 1 on error
- Errors go to stderr as plain text
- Error format: `Error Type: message\nLINE N: ...\n        ^`
- Map DuckDB error types to `fixable_by`:
  - `Catalog Error` (table/column not found) → `"agent"`
  - `Parser Error` (syntax error) → `"agent"`
  - `Permission Error` / readonly violation → `"human"`
  - `IO Error` (file not found) → `"agent"`

## Schema introspection

DuckDB supports `information_schema` and has its own system functions:

```sql
-- Tables
SELECT table_name, table_type
FROM information_schema.tables
WHERE table_schema = 'main'
ORDER BY table_name

-- Columns
SELECT column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_schema = 'main' AND table_name = ?
ORDER BY ordinal_position

-- Indexes
SELECT index_name, table_name, is_unique, sql
FROM duckdb_indexes()

-- Constraints
SELECT constraint_type, table_name, constraint_column_names
FROM duckdb_constraints()
```

## Read-only enforcement

- Database file mode: `-readonly` flag, engine-level enforcement (like SQLite's `SQLITE_OPEN_READONLY`)
- In-memory mode: inherently read-only (no persistent database to write to)
- No parser/guard needed

## Write mode

DuckDB supports writes when `-readonly` is not set. The subprocess driver can simply omit `-readonly` when `write: true` is requested.

## Default port

N/A — DuckDB is file-based, not networked.

## Timeout

Use `Bun.spawn` with `AbortSignal.timeout(ms)` to enforce query timeouts externally.

## File format support

When connected to DuckDB (even in-memory), users can query:
- Parquet files: `SELECT * FROM 'data.parquet'`
- CSV files: `SELECT * FROM 'data.csv'` or `read_csv('data.csv')`
- JSON/NDJSON: `SELECT * FROM 'data.json'` or `read_json('data.json')`
- Glob patterns: `SELECT * FROM 'data/*.parquet'`
- Remote files: `SELECT * FROM 'https://example.com/data.parquet'`

This is a key differentiator — no other driver supports direct file querying.
