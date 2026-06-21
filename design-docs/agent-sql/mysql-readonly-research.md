# MySQL Read-Only Enforcement Research

Research findings on how easy it is for an LLM constructing arbitrary SQL to escape from read-only mode in MySQL.

---

## 1. Transaction-Level Read-Only: `START TRANSACTION READ ONLY`

### What It Blocks

When a transaction is started with `START TRANSACTION READ ONLY`, MySQL blocks:

| Category | Statements |
|---|---|
| DML | `INSERT`, `UPDATE`, `DELETE`, `REPLACE` on permanent tables |
| DDL | All `CREATE`, `ALTER`, `DROP` on permanent and temporary tables |
| Locking | Locking permanent tables |

Error returned: **ERROR 1792 (25006): Cannot execute statement in a READ ONLY transaction**

### What It Allows

- `SELECT` on all tables
- DML (`INSERT`, `UPDATE`, `DELETE`) on `TEMPORARY` tables
- Locking temporary tables

### Can It Be Escaped?

**No.** Once a transaction is started with `READ ONLY`, the access mode cannot be changed for that transaction. `SET TRANSACTION READ WRITE` inside an active transaction produces:

```
ERROR 1568 (25001): Transaction characteristics can't be changed while a transaction is in progress
```

`SET SESSION TRANSACTION READ WRITE` is permitted mid-transaction, but only affects **subsequent** transactions, not the current one.

**Verdict: Solid enforcement for the duration of a single transaction.**

---

## 2. Session-Level Read-Only: `SET SESSION TRANSACTION READ ONLY`

### How It Works

```sql
SET SESSION TRANSACTION READ ONLY;
-- Equivalent:
SET @@SESSION.transaction_read_only = ON;
```

This sets the default access mode for all subsequent transactions in the session.

### Can It Be Overridden?

**Yes, trivially.** A session is free to change its own session characteristics at any time:

```sql
SET SESSION TRANSACTION READ ONLY;
-- LLM can immediately do:
SET SESSION TRANSACTION READ WRITE;
-- Or:
SET @@SESSION.transaction_read_only = OFF;
-- Or start a specific transaction:
START TRANSACTION READ WRITE;
```

All of these override the session-level read-only setting. The `START TRANSACTION READ WRITE` explicitly overrides the session default for that transaction (though if the `read_only` system variable is enabled globally, this requires `CONNECTION_ADMIN` or `SUPER` privilege).

**Verdict: Completely bypassable by the LLM. Cannot be relied upon alone.**

---

## 3. `read_only` System Variable

### How It Works

```sql
SET GLOBAL read_only = ON;
```

This is a **server-level** setting (typically used on replicas). When enabled:

- Non-privileged users cannot execute DML or DDL on permanent tables
- **Users with `SUPER` or `CONNECTION_ADMIN` privilege can still write**
- Temporary table operations are always allowed regardless of `read_only`

### Is It Relevant for a Client Connection?

Only if the application connects with a non-SUPER, non-CONNECTION_ADMIN user. Most application users would not have these privileges, so `read_only = ON` would effectively block writes.

However, this is a **global server setting** -- not something a client sets per-connection. It affects all connections. Not appropriate for a tool that may connect to databases where writes should be allowed for other users.

**Verdict: Not useful for per-connection read-only enforcement. Requires server-level configuration.**

---

## 4. `super_read_only`

### How It Works

```sql
SET GLOBAL super_read_only = ON;
```

This extends `read_only` to also block users with `SUPER` and `CONNECTION_ADMIN` privileges. Setting `super_read_only = ON` implicitly sets `read_only = ON`. Setting `read_only = OFF` implicitly sets `super_read_only = OFF`.

### Differences from `read_only`

| Behavior | `read_only` | `super_read_only` |
|---|---|---|
| Blocks non-privileged users | Yes | Yes |
| Blocks SUPER/CONNECTION_ADMIN users | **No** | **Yes** |
| Allows temp table writes | Yes | Yes |
| Scope | Global | Global |

### Relevance

Same limitation as `read_only` -- this is a global server setting, not per-connection. Useful for replicas, not for a CLI tool that wants per-session read-only.

**Verdict: Not useful for our use case.**

---

## 5. Escape Vectors

### 5a. `SET SESSION TRANSACTION READ WRITE` after `SET SESSION TRANSACTION READ ONLY`

**Can bypass: YES.**

Any session can change its own session-level transaction characteristics at any time. The LLM can simply issue `SET SESSION TRANSACTION READ WRITE` and all subsequent transactions will be read-write.

### 5b. `START TRANSACTION READ WRITE` after `SET SESSION TRANSACTION READ ONLY`

**Can bypass: YES.**

`START TRANSACTION READ WRITE` explicitly overrides the session default for that specific transaction. The only exception is if the global `read_only` system variable is enabled, which requires `CONNECTION_ADMIN`/`SUPER` to use `READ WRITE`.

### 5c. Multi-Statement Queries with `;`

**Risk level: LOW (with proper driver configuration).**

MySQL's client protocol requires **explicit opt-in** for multi-statement queries via the `CLIENT_MULTI_STATEMENTS` flag. The Node.js `mysql2` driver has `multipleStatements: false` by default.

If `multipleStatements` is disabled (the default), the server rejects any attempt to send multiple semicolon-separated statements in a single query. This means an LLM cannot inject `; SET SESSION TRANSACTION READ WRITE; INSERT INTO ...` as a single query.

**Mitigation: Ensure `multipleStatements` is never enabled.** This is the default.

### 5d. Function Calls That Modify Data (Triggers, Stored Procedures)

**Risk level: LOW within read-only transactions.**

If the connection is within a `START TRANSACTION READ ONLY` block:
- Stored procedures (`CALL proc()`) that attempt writes will fail with ERROR 1792
- Triggers fire on DML which is already blocked
- User-defined functions that attempt writes will also fail

The read-only transaction enforcement applies to all statements executed within it, including those inside stored procedures.

**However**, if using only session-level `SET SESSION TRANSACTION READ ONLY` (without wrapping in an explicit read-only transaction), a stored procedure could potentially change session state.

### 5e. `LOAD DATA INFILE`

**Blocked in read-only transactions.** This is a write operation and produces ERROR 1792 in a `READ ONLY` transaction.

Note: `LOAD DATA INFILE` also causes an implicit commit in some contexts, but within an explicit `START TRANSACTION READ ONLY`, the write is blocked before it can commit.

### 5f. `CREATE TEMPORARY TABLE`

**Blocked in read-only transactions.** DDL (including on temporary tables) is blocked even in read-only transactions. Only DML on *already existing* temporary tables is permitted.

```sql
START TRANSACTION READ ONLY;
CREATE TEMPORARY TABLE t (id INT);  -- ERROR: blocked (DDL)
```

However, if a temporary table was created *before* the read-only transaction started, DML on it is allowed:

```sql
CREATE TEMPORARY TABLE t (id INT);  -- Outside transaction, succeeds
START TRANSACTION READ ONLY;
INSERT INTO t VALUES (1);           -- Allowed (DML on temp table)
```

### 5g. `SET` Statements to Change Variables

**The most dangerous escape vector.**

Within an active read-only transaction, `SET SESSION TRANSACTION READ WRITE` does not affect the current transaction (it affects future ones). But between transactions, the LLM could:

1. Let the read-only transaction complete (COMMIT/ROLLBACK)
2. Issue `SET SESSION TRANSACTION READ WRITE`
3. Start a new read-write transaction

If we wrap each LLM query in its own `START TRANSACTION READ ONLY`, the SET statement changes the session for future transactions but cannot escape the current one. But if we rely solely on session-level enforcement without wrapping each query, `SET` is a complete bypass.

---

## 6. Comparison with PostgreSQL

### Session-Level Bypass

| Aspect | PostgreSQL | MySQL |
|---|---|---|
| Session-level read-only | `SET default_transaction_read_only = on` | `SET SESSION TRANSACTION READ ONLY` |
| Can LLM override session? | Yes: `SET default_transaction_read_only = off` | Yes: `SET SESSION TRANSACTION READ WRITE` |
| Variable name for override | `default_transaction_read_only` | `transaction_read_only` |
| `set_config()` function bypass | Yes: `SELECT set_config('default_transaction_read_only', 'off', false)` | **No equivalent** -- MySQL has no `set_config()` function |

MySQL does **not** have an equivalent to PostgreSQL's `set_config()` function. This means one fewer escape vector to worry about. In PostgreSQL, `set_config()` can change GUC parameters from within a `SELECT` statement, which is particularly dangerous because it looks like a read-only query. MySQL's `SET` statement is a distinct statement type that can be blocked by a lexical validator.

### Transaction-Level Enforcement

Both databases enforce `START TRANSACTION READ ONLY` similarly:
- Neither allows changing the access mode of an active read-only transaction
- Both allow temporary table DML within read-only transactions
- Both block DDL entirely

### Overall Assessment

MySQL's read-only enforcement is **slightly easier to secure** than PostgreSQL's because:
1. No `set_config()` equivalent hiding in SELECT statements
2. No dollar-quoted strings complicating lexical analysis
3. `SET` is always a distinct statement keyword, not embeddable in expressions

---

## 7. MySQL-Specific Syntax Concerns

### Identifier Quoting

| Style | Syntax | When Used |
|---|---|---|
| Backticks | `` `column name` `` | Default MySQL identifier quoting |
| Double quotes | `"column name"` | Only when `ANSI_QUOTES` SQL mode is enabled |
| Single quotes | `'string value'` | String literals only |

Backticks are simpler to parse than PostgreSQL's dollar-quoted strings. A backtick-quoted identifier is terminated by a closing backtick (`` ` ``), with escaped backticks represented as double backticks (``` `` ```).

**No dollar-quoted strings.** MySQL does not have PostgreSQL's `$$...$$` syntax, which is the main source of lexical analysis complexity in PostgreSQL.

### Comment Styles

MySQL supports **four** comment styles (one more than PostgreSQL):

| Style | Syntax | Notes |
|---|---|---|
| Hash | `# comment` | MySQL-specific, not in standard SQL |
| Double-dash | `-- comment` | Requires space/control char after `--` |
| C-style block | `/* comment */` | Standard multi-line comment |
| Executable/conditional | `/*! code */` | **Dangerous: executed by MySQL as SQL** |

### Executable Comments: A Unique Concern

MySQL's conditional comment syntax is a **significant lexical analysis concern**:

```sql
SELECT 1 /*! , DROP TABLE users */;
-- MySQL EXECUTES the content inside /*! ... */
```

Version-conditional comments execute only on specific MySQL versions:

```sql
/*!50000 DROP TABLE users */
-- Executes on MySQL >= 5.0.0
```

**This means a lexical analyzer cannot simply strip comments.** Content inside `/*! ... */` must be parsed and validated as SQL, because MySQL treats it as executable code.

### Comparison with PostgreSQL Syntax Concerns

| Concern | PostgreSQL | MySQL |
|---|---|---|
| Dollar-quoted strings | Yes: `$$`, `$tag$` | **No** |
| Executable comments | No | **Yes: `/*! ... */`** |
| Identifier quoting | `"identifier"` | `` `identifier` `` (or `"identifier"` in ANSI mode) |
| String escaping | `E'...'`, `'...'` | `'...'`, `"..."` (without ANSI_QUOTES) |
| Nested comments | Yes (PG supports) | No (deprecated, avoid) |

**Net assessment:** MySQL's syntax is simpler to parse overall. The main unique concern is executable comments, which must be treated as code, not ignored.

---

## 8. SQL Parser Options for MySQL

### Option A: Full Parser (node-sql-parser)

[node-sql-parser](https://www.npmjs.com/package/node-sql-parser) is a JavaScript SQL parser that produces an AST.

| Property | Details |
|---|---|
| MySQL support | Yes, default dialect |
| Bundle size | ~150KB (MySQL-only build) |
| Multi-statement | Supports splitting on `;` |
| Output | AST with statement type, table list, column list |
| Pure JS | Yes, no native/WASM dependencies |
| Maintenance | Active, regularly updated |

This would allow robust statement type detection from the AST rather than keyword matching.

### Option B: CST Parser (sql-parser-cst)

[sql-parser-cst](https://github.com/nene/sql-parser-cst) produces a Concrete Syntax Tree preserving all syntax elements.

| Property | Details |
|---|---|
| MySQL support | Experimental |
| Output | CST (preserves formatting, comments, etc.) |

Not recommended due to experimental MySQL support.

### Option C: Lightweight Lexical Scanner (Custom)

A custom lexer similar to the PostgreSQL approach in the existing codebase, adapted for MySQL syntax:

| Property | Details |
|---|---|
| Bundle size | ~0KB (no dependency) |
| Complexity | Lower than PG (no dollar-quoting) |
| Concerns | Must handle executable comments, backtick identifiers |
| Performance | Fastest option |

### Option D: No Parser Needed (libpg-query for MySQL?)

**There is no `libpg-query` equivalent for MySQL.** MySQL's server-side parser is written in C++ but has never been extracted as a standalone library compiled to WASM. The `@casual-simulation/sql-parser` mentions MySQL support but is not widely adopted.

### Recommendation

**A lightweight lexical scanner (Option C) is the best approach for MySQL**, for the same reasons as PostgreSQL:

1. MySQL's syntax is simpler than PostgreSQL's (no dollar-quoting)
2. The scanner only needs to identify statement types, not fully parse SQL
3. The main addition over PostgreSQL is handling executable comments (`/*! ... */`)
4. Zero dependency overhead
5. Combined with `START TRANSACTION READ ONLY`, the scanner only needs to catch escape attempts, not comprehensively block all writes

---

## Recommended Strategy for MySQL

### Defense in Depth (Three Layers)

```
User/LLM SQL Input
    |
    v
[Layer 1: SQL Lexical Validator]
    - Identify statement type from first keyword
    - Block: INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, TRUNCATE,
             GRANT, REVOKE, LOAD, REPLACE, RENAME, CALL
    - Block: SET SESSION TRANSACTION READ WRITE
    - Block: SET @@SESSION.transaction_read_only
    - Block: SET @@transaction_read_only
    - Block: START TRANSACTION READ WRITE
    - Block: START TRANSACTION (without READ ONLY -- force our wrapper)
    - Handle executable comments: parse /*! ... */ content as SQL
    - Handle backtick identifiers, string literals, all comment styles
    - Reject multi-statement input (split on ; outside strings/comments)
    |
    v
[Layer 2: Session-Level Default]
    - On connection: SET SESSION TRANSACTION READ ONLY
    - Belt-and-suspenders: if the lexer misses something, the session
      default provides a safety net for the next transaction
    |
    v
[Layer 3: Transaction-Level Wrapping]
    - Wrap each query execution in:
        START TRANSACTION READ ONLY;
        <user query>
        COMMIT;
    - Even if session-level is bypassed, each query runs in a
      read-only transaction that cannot be escaped mid-transaction
    |
    v
[Layer 4: Error Handling]
    - Catch ERROR 1792 (SQLSTATE 25006)
    - Message: "Cannot execute statement in a READ ONLY transaction"
    - Surface clear error to user/LLM
```

### Why This Works

1. **Layer 3 is the strongest defense.** An active `START TRANSACTION READ ONLY` cannot be escaped. Even if the LLM issues `SET SESSION TRANSACTION READ WRITE`, it only affects future transactions -- and Layer 3 wraps *every* query in its own read-only transaction.

2. **Layer 1 catches escape attempts early** with clear error messages, avoiding round-trips to the database.

3. **Layer 2 is a safety net** for any code path that might accidentally execute a query outside of a Layer 3 transaction wrapper.

4. **Multi-statement injection is blocked at two levels:**
   - Driver level: `multipleStatements: false` (default in mysql2)
   - Lexer level: reject input containing real semicolons

### Key Differences from PostgreSQL Strategy

| Aspect | PostgreSQL | MySQL |
|---|---|---|
| Session-level variable | `default_transaction_read_only` | `transaction_read_only` |
| Escape via function call | `set_config()` in SELECT | Not possible |
| Connection-level option | `options=-c default_transaction_read_only=on` | No equivalent connection param |
| Transaction wrapping | `BEGIN READ ONLY` | `START TRANSACTION READ ONLY` |
| Executable comments | No | Yes -- must parse `/*! */` content |
| Dollar-quoted strings | Must handle | Not applicable |
| Error code | SQLSTATE 25006 | ERROR 1792 / SQLSTATE 25006 |

### Lexer Additions for MySQL (vs PostgreSQL)

The MySQL lexer needs these adaptations compared to the PostgreSQL lexer:

1. **Executable comments:** When encountering `/*!`, extract the content and validate it as SQL (not just skip it as a comment)
2. **Version-conditional comments:** `/*!NNNNN ... */` -- extract content, validate as SQL
3. **Hash comments:** Recognize `#` as line-comment start (in addition to `--`)
4. **Backtick identifiers:** Track `` `...` `` as identifier quoting (skip content)
5. **No dollar-quoting:** Remove dollar-quote handling (simplification)
6. **`REPLACE` statement:** MySQL-specific write operation to block
7. **`LOAD DATA`:** MySQL-specific write operation to block
8. **`CALL` statement:** Block stored procedure calls (they could have write side effects; read-only transaction catches this too, but fail-fast is better)

### Driver Configuration

```typescript
import mysql from 'mysql2/promise';

const connection = await mysql.createConnection({
  host: '...',
  user: '...',
  password: '...',
  database: '...',
  multipleStatements: false,  // DEFAULT -- never enable this
});

// Set session default (Layer 2)
await connection.execute('SET SESSION TRANSACTION READ ONLY');

// For each LLM query (Layer 3)
await connection.execute('START TRANSACTION READ ONLY');
const [rows] = await connection.execute(userQuery);
await connection.execute('COMMIT');
```

---

## Summary of Findings

| Mechanism | Strength | Bypassable by LLM? |
|---|---|---|
| `START TRANSACTION READ ONLY` | **Strong** -- cannot be escaped mid-transaction | No (for current transaction) |
| `SET SESSION TRANSACTION READ ONLY` | Weak -- session default only | Yes, trivially |
| `read_only` global variable | Strong but global scope | Not by SQL (requires SUPER) |
| `super_read_only` global variable | Strongest but global scope | Not by SQL |
| Lexical validation | Defense in depth | Depends on implementation quality |
| `multipleStatements: false` | Strong driver-level protection | No (protocol-level enforcement) |

**Bottom line:** MySQL's `START TRANSACTION READ ONLY` is a reliable enforcement mechanism that cannot be escaped within the transaction. Combined with wrapping every LLM query in a read-only transaction and a lexical validator to catch escape attempts (especially `SET` and `START TRANSACTION READ WRITE` between transactions), MySQL can be made robustly read-only for LLM-generated SQL. MySQL is slightly easier to secure than PostgreSQL due to the absence of `set_config()` and dollar-quoted strings.
