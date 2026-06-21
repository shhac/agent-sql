# Read-Only Enforcement Strategies

Research findings for building a CLI tool that is read-only by default with optional write access.

## PostgreSQL

### Transaction-Level Read-Only (`BEGIN READ ONLY` / `SET TRANSACTION READ ONLY`)

PostgreSQL supports declaring a transaction as read-only at the start:

```sql
-- All equivalent:
BEGIN READ ONLY;
BEGIN TRANSACTION READ ONLY;
START TRANSACTION READ ONLY;

-- Or after BEGIN:
BEGIN;
SET TRANSACTION READ ONLY;
```

When a transaction is read-only, these SQL commands are **blocked by the server**:

| Category | Statements |
|---|---|
| DML | `INSERT`, `UPDATE`, `DELETE`, `MERGE` |
| COPY | `COPY FROM` (unless target is a temp table) |
| DDL | All `CREATE`, `ALTER`, `DROP` commands |
| Metadata | `COMMENT`, `GRANT`, `REVOKE` |
| Truncation | `TRUNCATE` |
| Indirect | `EXPLAIN ANALYZE` and `EXECUTE` (if the inner command is blocked) |

**Exception:** Operations on temporary tables are permitted even in read-only transactions.

**COPY TO is allowed** -- it reads data out, not in.

### Connection-Level Read-Only (`default_transaction_read_only`)

PostgreSQL can enforce read-only at the session/connection level via the `default_transaction_read_only` GUC parameter. Every new transaction inherits the current value.

```sql
-- Set for the current session (connection):
SET default_transaction_read_only = on;

-- Set for a specific role permanently:
ALTER ROLE myuser SET default_transaction_read_only = on;
```

For driver-level setup, pass it as a connection option:

```
# libpq options parameter:
options=-c default_transaction_read_only=on

# JDBC:
jdbc:postgresql://host:5432/db?options=-c%20default_transaction_read_only%3Don

# Environment variable:
PGOPTIONS='-c default_transaction_read_only=on'
```

**Important caveat:** A session can override `default_transaction_read_only` by issuing `SET default_transaction_read_only = off` or `BEGIN READ WRITE`. This means connection-level enforcement alone is not tamper-proof against crafted SQL. For a CLI tool, this needs to be combined with SQL-level validation.

### Error on Write Attempt

PostgreSQL returns SQLSTATE `25006` with the message pattern:

```
ERROR:  cannot execute <STATEMENT> in a read-only transaction
```

Examples:
- `ERROR: cannot execute INSERT in a read-only transaction`
- `ERROR: cannot execute CREATE TABLE in a read-only transaction`
- `ERROR: cannot execute DROP TABLE in a read-only transaction`
- `ERROR: cannot execute TRUNCATE TABLE in a read-only transaction`

### Recommended Strategy for PostgreSQL

Use **defense in depth** -- two layers:

1. **Connection level:** Set `default_transaction_read_only=on` via connection options so every transaction starts read-only by default.
2. **SQL validation (pre-execution):** Parse/validate SQL before sending to block `SET default_transaction_read_only`, `BEGIN READ WRITE`, and other escape attempts.
3. **Transaction level (belt and suspenders):** Wrap each execution in `BEGIN READ ONLY` ... `COMMIT`.

---

## SQLite

### `bun:sqlite` `{ readonly: true }` (Database Constructor Option)

Bun's built-in SQLite driver accepts a `readonly` option on the `Database` constructor:

```typescript
import { Database } from "bun:sqlite";

const db = new Database("mydb.sqlite", { readonly: true });
```

This maps directly to SQLite's `SQLITE_OPEN_READONLY` flag passed to `sqlite3_open_v2()`. The database is opened at the OS level without write permissions. This is enforced at the SQLite engine level -- no SQL statement can bypass it.

The higher-level `SQL` adapter also supports it:

```typescript
import { SQL } from "bun";

const sql = new SQL({
  adapter: "sqlite",
  filename: "app.db",
  readonly: true,
});
```

Additional related options:
- `readonly: true` -- open read-only (`SQLITE_OPEN_READONLY`)
- `readwrite: true` -- open read-write (`SQLITE_OPEN_READWRITE`)
- `create: true` -- create if not exists (`SQLITE_OPEN_CREATE`)

### Error on Write Attempt (readonly connection)

SQLite returns error code `SQLITE_READONLY` (8) with the message:

```
attempt to write a readonly database
```

This applies to any write operation: `INSERT`, `UPDATE`, `DELETE`, `CREATE TABLE`, etc.

### Transaction-Level Read-Only Mode

**SQLite does not have a `BEGIN READ ONLY` statement.** There is no transaction-level read-only mode.

However, there are two alternatives:

1. **`PRAGMA query_only = ON`** -- Session-level toggle that blocks data modification:
   - Blocks: `CREATE`, `DELETE`, `DROP`, `INSERT`, `UPDATE`
   - Returns `SQLITE_READONLY` error on violation
   - Caveat: Does **not** make the database truly read-only. `COMMIT`, checkpoints, and WAL operations still work. `sqlite3_db_readonly()` is not affected.
   - Can be toggled off by executing `PRAGMA query_only = OFF`

2. **`SQLITE_OPEN_READONLY` flag** (connection-level) -- Cannot be bypassed by SQL. This is the strongest enforcement.

### Recommended Strategy for SQLite

Use **`SQLITE_OPEN_READONLY`** via `{ readonly: true }` as the primary mechanism. It cannot be bypassed by any SQL statement. No additional transaction-level wrapping is needed.

When write mode is enabled, open the database with `{ readwrite: true, create: false }` to allow writes but prevent creating new database files accidentally.

---

## SQL Lexical Analysis

Even with database-level enforcement, pre-execution SQL validation provides defense in depth (fail fast with a clear error, prevent round-trips, block escape attempts like `SET default_transaction_read_only`).

### Safe Statements (Read-Only Allowlist)

| Statement | Notes |
|---|---|
| `SELECT` | Core read operation |
| `WITH ... SELECT` | CTEs that only select (see edge cases below) |
| `EXPLAIN` | Query plan inspection |
| `EXPLAIN ANALYZE` | PG blocks this in read-only tx anyway if the inner statement writes |
| `SHOW` | PostgreSQL: show configuration |
| `DESCRIBE` / `\d` | Not standard SQL; `\d` is psql-specific |
| `SET` | Mostly safe, but must block `SET default_transaction_read_only` |
| `BEGIN` / `COMMIT` / `ROLLBACK` | Transaction control (must block `BEGIN READ WRITE`) |
| `COPY TO` | PostgreSQL: export data (read operation) |
| `PRAGMA` (read-only) | SQLite: e.g., `PRAGMA table_info(...)` |

### Blocked Statements (Write Denylist)

| Category | Statements |
|---|---|
| DML | `INSERT`, `UPDATE`, `DELETE`, `MERGE`, `UPSERT` |
| DDL | `CREATE`, `ALTER`, `DROP` |
| Data load | `COPY FROM` (PG), `LOAD` |
| Truncation | `TRUNCATE` |
| Permissions | `GRANT`, `REVOKE` |
| Metadata | `COMMENT` |
| Index maint. | `REINDEX`, `VACUUM` (writes to disk), `CLUSTER` |
| Sequences | `NEXTVAL()`, `SETVAL()` (PG function calls that mutate state) |
| Misc | `DISCARD`, `PREPARE` (if wrapping writes), `EXECUTE` (if wrapping writes) |

### Edge Cases

#### 1. Writable CTEs (`WITH ... INSERT/UPDATE/DELETE`)

PostgreSQL supports data-modifying statements inside CTEs:

```sql
-- This is a WRITE, not a read:
WITH deleted AS (
  DELETE FROM orders WHERE status = 'cancelled' RETURNING *
)
SELECT * FROM deleted;

-- Also a write:
WITH new_rows AS (
  INSERT INTO logs (msg) VALUES ('hello') RETURNING *
)
SELECT * FROM new_rows;
```

**Detection strategy:** After identifying a `WITH` clause, check each CTE body for `INSERT`, `UPDATE`, `DELETE`, `MERGE` keywords. The outer statement after the CTE definitions must also be checked.

#### 2. `SELECT ... INTO` (PostgreSQL)

```sql
-- Creates a new table:
SELECT * FROM users INTO new_users_table;
-- Equivalent to CREATE TABLE AS
```

**Detection strategy:** Look for `INTO` after `SELECT` that is not inside a subquery and not `INTO` for variable assignment in PL/pgSQL. In practice, block `SELECT ... INTO` unless it follows `INSERT INTO`.

#### 3. `SELECT ... FOR UPDATE / FOR SHARE`

```sql
SELECT * FROM accounts WHERE id = 1 FOR UPDATE;
```

Takes row-level locks. While it doesn't modify data, it can block other transactions and is inappropriate for a read-only tool.

**Detection strategy:** Check for `FOR UPDATE`, `FOR NO KEY UPDATE`, `FOR SHARE`, `FOR KEY SHARE` at the end of SELECT statements.

#### 4. `COPY` Directionality (PostgreSQL)

```sql
COPY table_name TO STDOUT;     -- READ: safe
COPY table_name FROM STDIN;    -- WRITE: blocked
COPY (SELECT ...) TO STDOUT;   -- READ: safe
```

**Detection strategy:** `COPY ... FROM` is a write; `COPY ... TO` is a read.

#### 5. `CREATE TEMP TABLE AS SELECT` (PostgreSQL)

```sql
CREATE TEMP TABLE results AS SELECT * FROM users;
```

PostgreSQL read-only transactions actually **block** this (all CREATE is blocked). Some applications rely on temp table exceptions, but the standard read-only mode does not permit it.

**Detection strategy:** Block all `CREATE` statements regardless of `TEMP`/`TEMPORARY` qualifier.

#### 6. Function Calls That Modify Data (PostgreSQL)

```sql
SELECT my_function_that_deletes_stuff();
SELECT pg_terminate_backend(pid);
SELECT dblink_exec('DELETE FROM users');
SELECT lo_unlink(12345);  -- Deletes large object
```

Functions can have side effects. This is the hardest category to police via lexical analysis alone. The database-level read-only transaction is essential here -- PostgreSQL will block writes even inside functions when the transaction is read-only.

**Practical approach:** Cannot fully solve via parsing. Rely on the database's transaction-level read-only enforcement for this case.

#### 7. Multi-Statement Injection (Semicolons)

```sql
SELECT 1; DROP TABLE users; --
```

**Detection strategy:** Split on semicolons (respecting string literals and comments), then validate each statement independently. Alternatively, reject multi-statement input entirely unless the tool explicitly supports it.

**Complications for splitting:**
- String literals: `'it''s a semicolon: ;'`
- Dollar-quoted strings (PG): `$$contains ; semicolons$$`, `$tag$also ; here$tag$`
- Comments: `-- semicolons in line comments ;` and `/* block ; comments */`
- Identifiers: `"column;name"` (quoted identifiers)

#### 8. Dollar-Quoted Strings Hiding Writes (PostgreSQL)

```sql
-- Dollar-quoting in function bodies:
CREATE FUNCTION evil() RETURNS void AS $$
  DELETE FROM users;
$$ LANGUAGE sql;

-- With custom tags:
CREATE FUNCTION evil() RETURNS void AS $body$
  DELETE FROM users;
$body$ LANGUAGE sql;
```

**Detection strategy:** The outer statement is `CREATE FUNCTION`, which is already blocked. Dollar-quoted strings are relevant for the multi-statement parser -- it must correctly skip `$$..$$` content to find real semicolons. Regex for dollar-quoting: `\$[a-zA-Z_]*\$`.

#### 9. `ATTACH DATABASE` (SQLite)

```sql
ATTACH DATABASE '/path/to/other.db' AS other;
-- Can then write to `other` even if main is readonly via PRAGMA query_only
```

**Detection strategy:** Block `ATTACH` statements entirely in read-only mode. Note: if using `SQLITE_OPEN_READONLY`, writes to attached databases also fail at the engine level.

#### 10. Writable PRAGMAs (SQLite)

Many PRAGMAs modify database state:

| Writable PRAGMA | Effect |
|---|---|
| `PRAGMA journal_mode = ...` | Changes journaling mode |
| `PRAGMA auto_vacuum = ...` | Changes vacuum strategy |
| `PRAGMA schema_version = N` | Modifies schema version (corruption risk) |
| `PRAGMA user_version = N` | Modifies user version |
| `PRAGMA page_size = N` | Changes page size |
| `PRAGMA secure_delete = ON` | Changes delete behavior |
| `PRAGMA synchronous = ...` | Changes fsync behavior |
| `PRAGMA wal_checkpoint` | Triggers WAL checkpoint |
| `PRAGMA query_only = OFF` | Disables query_only protection |

**Detection strategy:** For SQLite read-only mode, either:
- Allowlist specific read-only PRAGMAs (e.g., `table_info`, `table_list`, `database_list`, `index_list`, `foreign_key_list`, `compile_options`)
- Block any PRAGMA with an `=` assignment (heuristic; some read PRAGMAs use `=` syntax but this catches most writes)

#### 11. `.import` and Dot-Commands (SQLite CLI)

Dot-commands (`.import`, `.dump`, `.output`) are **SQLite CLI features**, not SQL. They are not sent through the SQL parser and are not relevant for a programmatic driver like `bun:sqlite`. No action needed for the CLI tool.

---

## Recommended Architecture

### Layered Enforcement

```
User SQL Input
    |
    v
[Layer 1: SQL Lexical Validator]
    - Fast keyword-based pre-check
    - Rejects obvious writes before touching the database
    - Handles multi-statement detection
    - Catches escape attempts (SET, BEGIN READ WRITE, ATTACH, writable PRAGMAs)
    |
    v
[Layer 2: Database-Level Enforcement]
    - PostgreSQL: default_transaction_read_only=on + BEGIN READ ONLY
    - SQLite: SQLITE_OPEN_READONLY (via { readonly: true })
    - Catches function side effects and anything the lexer misses
    |
    v
[Layer 3: Error Handling]
    - Catch database read-only errors and surface clear messages
    - PG: SQLSTATE 25006 "cannot execute X in a read-only transaction"
    - SQLite: error code 8 "attempt to write a readonly database"
```

### Write Mode Toggle

When the user opts into write mode:
- Skip Layer 1 (or switch to a permissive validator)
- PostgreSQL: Use `default_transaction_read_only=off`, no `BEGIN READ ONLY`
- SQLite: Open with `{ readonly: false, readwrite: true }`

### SQL Lexer Implementation Notes

A full SQL parser is overkill. A lightweight lexical scanner is sufficient:

1. **Tokenize** the input, handling string literals (single-quoted, dollar-quoted for PG), quoted identifiers, and comments
2. **Split** on real semicolons (outside strings/comments) to isolate statements
3. **Identify** the first keyword of each statement against the allowlist/denylist
4. **Check** for edge cases: `WITH` + write verb, `SELECT ... INTO`, `SELECT ... FOR UPDATE`, `COPY FROM` vs `COPY TO`
5. **Reject** or **allow** based on results

This approach is fast, has no dependencies, and covers the vast majority of cases. The database-level enforcement handles the remainder (especially function side effects).
