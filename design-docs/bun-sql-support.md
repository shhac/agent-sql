# Bun Built-in Database Support

Research date: 2026-03-26

## Summary

Bun provides **two separate APIs** for database access:

1. **`bun:sqlite`** -- A synchronous, high-performance SQLite3 driver (stable, long-standing)
2. **`Bun.SQL` (`import { sql, SQL } from "bun"`)** -- A unified async API supporting PostgreSQL, MySQL, and SQLite via tagged template literals

Both are native (implemented in Zig/C, not JavaScript wrappers around npm packages).

---

## 1. Native PostgreSQL Client

**Yes.** Available via `import { sql, SQL } from "bun"` (not a `bun:` specifier).

### API Style

Tagged template literals with automatic parameterization:

```ts
import { sql, SQL } from "bun";

// Uses DATABASE_URL or PG env vars automatically
const users = await sql`SELECT * FROM users WHERE active = ${true} LIMIT ${10}`;

// Explicit connection
const pg = new SQL("postgres://user:pass@localhost:5432/mydb");
const results = await pg`SELECT * FROM users WHERE id = ${userId}`;
```

### Key Features

- **Connection pooling** with configurable `max`, `idleTimeout`, `maxLifetime`, `connectionTimeout`
- **Transactions** via `sql.begin(async tx => { ... })`
- **Prepared statements** -- automatic, cached per query string
- **SQL injection protection** -- values interpolated in tagged templates are always parameterized
- **Binary protocol** for better performance
- **TLS/SSL** support with full certificate configuration
- **SASL auth** (SCRAM-SHA-256), MD5, and clear text
- **BigInt support**
- **Result formats**: objects (default), `.values()` (arrays), `.raw()` (Buffers)
- **SQL fragments** for dynamic queries (`sql()` helper for table names, conditional clauses, WHERE IN, bulk inserts, column picking)
- **`sql.array()`** for PostgreSQL array literals
- **`.simple()` queries** for multi-statement execution (no params)
- **`sql.file("query.sql", params)`** to execute SQL files
- **`sql.unsafe()`** for raw SQL strings
- **Query cancellation** via `.cancel()`
- **Lazy execution** -- queries only run when awaited or `.execute()` is called
- **Preconnection** via `bun --sql-preconnect` flag (establishes connection at startup before app code runs)

### Environment Variables

Checks in order: `POSTGRES_URL`, `DATABASE_URL`, `PGURL`, `PG_URL`. Falls back to individual vars: `PGHOST`, `PGPORT`, `PGUSERNAME`/`PGUSER`, `PGPASSWORD`, `PGDATABASE`.

---

## 2. Native SQLite Support

**Yes.** Two APIs available:

### A. `bun:sqlite` (Synchronous, Lower-Level)

The original SQLite API. Synchronous, modeled after `better-sqlite3`.

```ts
import { Database } from "bun:sqlite";

const db = new Database("mydb.sqlite");  // or ":memory:"
const query = db.query("SELECT * FROM users WHERE id = ?1");
const user = query.get(42);
const allUsers = query.all();
```

**Features:**
- **Synchronous API** -- no promises, no callbacks
- **Prepared statements** with `.query()` (cached) and `.prepare()` (uncached)
- **Result methods**: `.all()`, `.get()`, `.run()`, `.values()`, `.iterate()`
- **`.as(Class)`** -- map results to class instances (zero-cost, skips constructor)
- **Transactions** via `db.transaction(fn)` with `.deferred()`, `.immediate()`, `.exclusive()` variants
- **Nested transactions** become savepoints
- **WAL mode** support (`PRAGMA journal_mode = WAL`)
- **Serialize/deserialize** databases to/from `Uint8Array`
- **ES module import**: `import db from "./mydb.sqlite" with { type: "sqlite" }`
- **`using` statement** support for automatic cleanup
- **Extension loading** via `.loadExtension()`
- **Strict mode** (`strict: true`) for prefix-free parameter binding and missing-param errors
- **`safeIntegers` option** for BigInt integer handling
- **3-6x faster than better-sqlite3**, 8-9x faster than Deno SQLite

**Datatype mapping:**

| JavaScript   | SQLite             |
|--------------|--------------------|
| `string`     | `TEXT`             |
| `number`     | `INTEGER`/`DECIMAL`|
| `boolean`    | `INTEGER` (0/1)    |
| `Uint8Array` | `BLOB`            |
| `Buffer`     | `BLOB`            |
| `bigint`     | `INTEGER`          |
| `null`       | `NULL`            |

### B. `Bun.SQL` with SQLite Adapter (Async, Unified API)

Same tagged template API as PostgreSQL/MySQL:

```ts
import { SQL } from "bun";

const db = new SQL("sqlite://myapp.db");
// or: new SQL(":memory:");
// or: new SQL({ adapter: "sqlite", filename: "./app.db" });

const users = await db`SELECT * FROM users WHERE active = ${1}`;
```

Uses `bun:sqlite` under the hood but wraps it in the async tagged-template interface. Supports the same connection string formats and environment variable auto-detection.

---

## 3. Production Readiness

### `bun:sqlite`

**Production-ready.** This has been in Bun since early versions, is well-documented, extensively benchmarked, and widely used. It is a first-class Bun feature.

### `Bun.SQL` (PostgreSQL / MySQL / unified SQLite)

**Production-ready with caveats.** The docs present it as a stable feature (no "experimental" warnings). It ships as part of the `bun` package (not behind a flag). However:

- MySQL support is newer and may have edge cases
- The unified API is relatively recent compared to `bun:sqlite`
- `sql.array()` is PostgreSQL-only and "multi-dimensional arrays and NULL elements may not be supported yet"

The PostgreSQL client appears to be the most mature path in `Bun.SQL`, as PostgreSQL is the default/fallback adapter.

---

## 4. Limitations

### PostgreSQL (`Bun.SQL`)
- `sql.array()` does not support multi-dimensional arrays or NULL elements
- `.simple()` queries cannot use parameters
- Preconnection (`--sql-preconnect`) is PostgreSQL-only
- No streaming/cursor support documented

### SQLite (`bun:sqlite`)
- **macOS**: Uses Apple's system SQLite, which has persistent WAL enabled -- `-wal` and `-shm` files persist after close (workaround documented)
- **macOS**: Extension loading requires installing vanilla SQLite via Homebrew and calling `Database.setCustomSQLite(path)`
- Synchronous only -- no async API (use `Bun.SQL` SQLite adapter if async is needed)
- No FTS5 or other extension guarantees on macOS system SQLite

### SQLite (`Bun.SQL` adapter)
- Wraps the synchronous `bun:sqlite` -- actual I/O is still synchronous under the hood
- Less fine-grained control than `bun:sqlite` directly (no `.as(Class)`, no `.iterate()`, no serialize/deserialize)

### MySQL (`Bun.SQL`)
- Newer addition, fewer production reports
- Same tagged-template API but MySQL-specific features (stored procedures, multiple result sets) may have edge cases

### General
- `Bun.SQL` is **not** available under `bun:sql` -- it is `import { sql, SQL } from "bun"`
- No ORM or migration tooling built in
- No connection to external connection poolers (PgBouncer, etc.) is specifically documented

---

## 5. Relevance to agent-sql

For this project, the key takeaways:

- **PostgreSQL**: Use `import { sql, SQL } from "bun"` directly. No npm dependency needed. Tagged template literals with automatic parameterization make it safe and ergonomic. Connection pooling is built in.
- **SQLite**: Use `import { Database } from "bun:sqlite"` for synchronous access (simpler, faster, more control), or `new SQL("sqlite://...")` for the unified async API.
- **No need for `pg`, `postgres`, `better-sqlite3`, or similar npm packages** when running on Bun.
- The unified `Bun.SQL` API means the same query syntax works across PostgreSQL, MySQL, and SQLite, which could be useful for testing (SQLite) vs production (PostgreSQL).
