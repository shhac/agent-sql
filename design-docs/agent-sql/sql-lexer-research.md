# SQL Lexer/Tokenizer Research

**Date:** 2026-03-26
**Goal:** Find a dependency for PostgreSQL SQL tokenizing/lexing to detect multi-statement queries, SET statements, and BEGIN READ WRITE -- without building our own lexer.

## Requirements

1. Multi-statement detection (semicolons outside strings/comments)
2. SET statement detection (`SET default_transaction_read_only`, `SET transaction_read_only`)
3. BEGIN READ WRITE detection
4. Must handle: dollar-quoted strings (`$$...$$`, `$tag$...$tag$`), single-quoted strings, double-quoted identifiers, SQL comments (`--`, `/* */`)
5. Bun-compatible, including `bun build --compile`
6. Lightweight -- we want tokenizing/lexing, not a full ORM or query builder

## Package Comparison

### libpg-query (RECOMMENDED)

| Attribute | Value |
|---|---|
| **Package** | `libpg-query` |
| **Version** | 17.7.3 |
| **Weekly downloads** | 128,769 |
| **Install size** | 1.2 MB (WASM module is 1.1 MB) |
| **Binary size impact** | ~0 (Bun baseline is 58 MB, with libpg-query still 58 MB) |
| **License** | MIT |
| **Last updated** | 2025-12-11 |
| **PostgreSQL dialect** | Yes -- it IS PostgreSQL's actual parser compiled to WASM |
| **Dollar-quoted strings** | Yes -- handles all PG syntax by definition |
| **Tokenize without full parse** | No public scan/tokenize API in the lightweight package; `parse()` returns full AST |
| **Bun compatible** | Yes -- tested, works with `bun` runtime and `bun build --compile` |
| **Dependencies** | `@pgsql/types` |

**API:**
- `parse(sql)` -- async, returns `{ stmts: [...] }` with full AST
- `parseSync(sql)` -- sync variant (requires `loadModule()` first in Bun)
- `loadModule()` -- explicit WASM init (needed for sync API in Bun)

**Performance (Bun, Apple Silicon):**
- `parseSync`: ~0.014 ms/query (71K queries/sec)
- `parse` (async): ~0.025 ms/query (40K queries/sec)

**Tested capabilities:**
```
parse('SELECT 1; SELECT 2')         -> stmts.length === 2 (multi-statement)
parse('SET default_transaction_read_only = on')
  -> stmts[0].stmt.VariableSetStmt.name === "default_transaction_read_only"
parse('SELECT $$hello$$')           -> parses correctly
parse('BEGIN READ WRITE')           -> stmts[0].stmt.TransactionStmt with options
```

**Pros:**
- Uses PostgreSQL's actual parser -- 100% correct for all PG syntax by definition
- WASM-based, no native compilation needed
- Zero binary size overhead in Bun compile
- Rich AST lets us detect statement types, variable names, transaction modes
- Actively maintained, tracks PG major versions
- MIT license

**Cons:**
- Full parse, not just tokenize (slight overhead vs a pure lexer, but 0.014ms is negligible)
- WASM module requires async init (one-time `loadModule()` or first `parse()` call)
- Returns rich AST when we only need statement type detection (but we can ignore the rest)

---

### node-sql-parser (PostgreSQL build)

| Attribute | Value |
|---|---|
| **Package** | `node-sql-parser` (import from `node-sql-parser/build/postgresql`) |
| **Version** | 5.4.0 |
| **Weekly downloads** | 664,690 |
| **Install size** | 88 MB total; PG-only JS file is 301 KB |
| **Bundle size** | ~150 KB (PG-only import, gzipped ~429 KB for full) |
| **License** | Apache-2.0 |
| **Last updated** | 2026-01-12 |
| **PostgreSQL dialect** | Yes (one of many supported dialects) |
| **Dollar-quoted strings** | Yes -- tested, parses `$$..$$` correctly |
| **Tokenize without full parse** | No -- full PEG.js-based parser |
| **Bun compatible** | Yes -- pure JS |

**Performance (Bun, Apple Silicon):**
- `astify`: ~0.082 ms/query (12K queries/sec) -- ~6x slower than libpg-query

**Tested capabilities:**
```
astify('SELECT 1; SELECT 2')        -> array of 2 ASTs
astify('SET default_transaction_read_only = on')  -> { type: "set", ... }
astify('SELECT $$hello$$')          -> parses correctly
astify('BEGIN READ WRITE')          -> { type: "transaction", ... }
```

**Pros:**
- Largest community (664K weekly downloads)
- Pure JavaScript, no WASM/native deps
- Can import PG-only build (301 KB)
- Handles all four test cases

**Cons:**
- 88 MB install size (though only 301 KB used at runtime with PG import)
- 6x slower than libpg-query
- PEG.js-generated parser -- may not handle all PG edge cases (it's a reimplementation, not PG's actual parser)
- Apache-2.0 (fine, but not MIT)

---

### sql-parser-cst

| Attribute | Value |
|---|---|
| **Package** | `sql-parser-cst` |
| **Version** | 0.38.2 |
| **Weekly downloads** | 30,694 |
| **Install size** | 6.7 MB |
| **Bundle size** | ~104 KB gzipped |
| **License** | GPL-2.0-or-later |
| **Last updated** | 2026-01-10 |
| **PostgreSQL dialect** | Experimental (PG 16) |
| **Dollar-quoted strings** | Unknown -- PG support is experimental |
| **Tokenize without full parse** | No -- lexer is not exposed as public API |
| **Dependencies** | None |

**Pros:**
- Zero dependencies
- Concrete Syntax Tree preserves all formatting
- Actively maintained

**Cons:**
- **GPL-2.0 license** -- viral, would require our project to be GPL
- PostgreSQL support is explicitly "experimental"
- No standalone lexer API
- May not handle all PG syntax (experimental status)

---

### sql-tokenizer

| Attribute | Value |
|---|---|
| **Package** | `sql-tokenizer` |
| **Version** | 1.0.5 |
| **Weekly downloads** | 1,444 |
| **Install size** | 15 KB |
| **License** | MIT |
| **Last updated** | 2025-10-14 |
| **PostgreSQL dialect** | No -- dialect-agnostic |
| **Dollar-quoted strings** | **No** -- only handles single quotes, double quotes, backticks |
| **Tokenize without full parse** | Yes -- pure tokenizer |
| **Dependencies** | None |

**Reviewed source code:** The `extractQuotes.js` module explicitly handles only single quotes (`'`), double quotes (`"`), and backticks. No dollar-quote support. Returns flat string arrays without token type classification.

**Verdict:** Does not meet requirements. No dollar-quoted string support, no token type classification.

---

### sql-lexer

| Attribute | Value |
|---|---|
| **Package** | `sql-lexer` |
| **Version** | 0.2.2 |
| **Weekly downloads** | 51 |
| **Install size** | Unknown |
| **License** | BSD |
| **Last updated** | 2015-03-16 |
| **PostgreSQL dialect** | No -- SQL92/MySQL/SQLite only, PG "intended" |
| **Dollar-quoted strings** | No |

**Verdict:** Abandoned (11 years old, 51 downloads/week). Does not meet requirements.

---

### pgsql-parser / pg-query-parser (libpg-query wrappers)

| Attribute | pgsql-parser | pg-query-parser |
|---|---|---|
| **Version** | 17.9.14 | 0.3.0 |
| **Weekly downloads** | 73,785 | 1,663 |
| **Last updated** | 2026-03-15 | 2021-09-29 |
| **Dependencies** | libpg-query, @pgsql/types, pgsql-deparser | lodash, pg-query-native |

These are higher-level wrappers around libpg-query. `pgsql-parser` adds a deparser (AST back to SQL) which we don't need. `pg-query-parser` uses the old native bindings (not WASM) and is unmaintained. Neither adds value for our use case -- using `libpg-query` directly is simpler.

---

### @pyramation/libpg-query-wasm

| Attribute | Value |
|---|---|
| **Version** | 17.1.1 |
| **Install size** | 39 MB |
| **Dependencies** | @launchql/protobufjs, @pgsql/types, deasync |

This is an older/alternative WASM build. The main `libpg-query` package already uses WASM and is smaller (1.2 MB vs 39 MB). No reason to use this.

---

## Rolling Our Own Lexer

For comparison, what would a custom lexer need to handle:

1. **Single-quoted strings:** `'hello'`, `'it''s'` (escaped quotes)
2. **Dollar-quoted strings:** `$$body$$`, `$tag$body$tag$` (case-sensitive tags)
3. **Double-quoted identifiers:** `"column name"`
4. **Line comments:** `-- comment\n`
5. **Block comments:** `/* comment */` (nested in PG!)
6. **Semicolons:** statement separator (only outside strings/comments)
7. **Keywords:** SET, BEGIN, READ, WRITE, etc.

The tricky parts are dollar-quoted strings (tag matching, nesting) and nested block comments (PG extension). A naive regex-based approach will miss edge cases. We'd essentially be reimplementing a subset of PG's lexer.

**Estimated effort:** 200-400 lines of TypeScript, plus tests. Ongoing maintenance for edge cases we discover.

**Risk:** Getting dollar-quote parsing wrong could cause false positives on multi-statement detection, which is a security-relevant feature (read-only enforcement).

## Recommendation

**Use `libpg-query` directly.**

Rationale:
1. **Correctness:** It is PostgreSQL's actual parser. Zero risk of dialect mismatches or missed edge cases.
2. **Performance:** 0.014 ms per parse (sync) is negligible. Even async at 0.025 ms is fast enough.
3. **Size:** 1.2 MB install, zero binary size overhead in Bun compile.
4. **Bun compatibility:** Tested and working with both `bun` runtime and `bun build --compile`.
5. **API fit:** `parse()` returns typed statement nodes. Detecting SET/BEGIN/SELECT is a simple check on `Object.keys(stmt)[0]`.
6. **Maintenance:** We get PG parser updates for free by bumping the dependency version.

The only "downside" is that it does a full parse when we only need statement type detection, but at 0.014 ms that's a non-issue. A custom lexer would be faster in theory but slower to ship, harder to maintain, and riskier for correctness.

### Usage Pattern

```typescript
import { parse, loadModule } from "libpg-query";

// One-time init (needed for parseSync in Bun)
await loadModule();

function analyzeQuery(sql: string) {
  const result = parseSync(sql);

  // Multi-statement detection
  if (result.stmts.length > 1) { /* reject or handle */ }

  // Statement type detection
  const stmtType = Object.keys(result.stmts[0].stmt)[0];
  // "SelectStmt", "VariableSetStmt", "TransactionStmt", etc.

  // SET detection
  if (stmtType === "VariableSetStmt") {
    const name = result.stmts[0].stmt.VariableSetStmt.name;
    // "default_transaction_read_only", "transaction_read_only", etc.
  }

  // BEGIN READ WRITE detection
  if (stmtType === "TransactionStmt") {
    const opts = result.stmts[0].stmt.TransactionStmt.options;
    // Check for read_only option
  }
}
```
