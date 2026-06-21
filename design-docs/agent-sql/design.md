# agent-sql Design Document

## Overview

**agent-sql** is a read-only-by-default SQL CLI for AI agents, supporting PostgreSQL, SQLite, and MySQL/MariaDB. It provides structured JSON output optimized for LLM consumption, with layered read-only enforcement that cannot be bypassed by the LLM.

**v1 scope:** PostgreSQL + SQLite. MySQL is designed-in but ships as a fast-follow (Bun supports all three natively with zero deps).

**Runtime:** Bun (TypeScript), compiled to standalone binaries via `bun build --compile`

**Distribution:** Homebrew tap (`shhac/tap/agent-sql`), GitHub releases, Claude Code skill

---

## Core Design Principles

1. **Read-only by default** — write permission is stored with the credential, not exposed to the LLM
2. **Defense in depth** — database-level enforcement (primary) + PG parser for session escape prevention
3. **LLM-native output** — structured JSON to stdout, self-correcting errors to stderr
4. **Minimal dependencies** — Bun provides PostgreSQL, SQLite, and MySQL natively; only `commander` + `libpg-query` as runtime deps
5. **Independent from agent-mongo** — copy-and-adapt shared patterns, greenfield where appropriate

---

## Permission Model

### Write Permission Lives in the Keychain with the Credential

Credentials (username, password, writePermission) are stored together in the macOS Keychain, not in the config file. The config file contains only connection aliases and settings — zero sensitive data, zero policy.

```
Keychain (app.paulie.agent-sql):
  "prod-readonly" → { username: "reader", password: "secret", writePermission: false }
  "prod-admin"    → { username: "admin", password: "s3cure", writePermission: true }

Config file (~/.config/agent-sql/config.json):
  connection "prod" → { host, port, database, credential: "prod-readonly" }
```

### Why this is secure

The LLM can't escalate from read to write because:

1. **Credentials live in the Keychain, not the config file.** Editing `config.json` gains nothing — there's no `writePermission` to flip.
2. **Creating a useful PG write credential requires knowing the database password.** The LLM can call `credential add` but without a valid password, the credential is worthless.
3. **If the LLM can read Keychain entries** (`security find-generic-password`), it can already read passwords — the game is over regardless of our design. This is the same threat model as any credential stored on the system.

For non-macOS platforms, credentials fall back to a file (`~/.config/agent-sql/credentials.json`). On those platforms, `writePermission` is as editable as the password itself — same security posture. The password is the real gate.

### Credential-Less Connections

SQLite connections without a credential default to **read-only**:
```bash
# No credential → read-only (zero friction for the common case)
agent-sql connection add mydb --driver sqlite --path /Users/paul/data/app.sqlite

# --write with no credential → allowed for SQLite only (it's a local file)
agent-sql query run "INSERT INTO logs ..." -c mydb --write

# --write on PG with no credential → error (PG always needs auth)
```

Rationale: if we block writes and the LLM has `sqlite3` or Python available, it just writes another way. Credential-less write for SQLite acknowledges the reality of local file access.

PostgreSQL always requires a credential (it needs auth).

### Write Opt-In

Even when a credential has `writePermission: true`, the query command defaults to read-only. The caller must explicitly opt in:

```bash
# Read-only (default) — even if credential allows writes
agent-sql query run "SELECT * FROM users" -c prod

# Write mode — requires credential.writePermission: true
agent-sql query run "INSERT INTO logs ..." -c prod --write

# Write mode with read-only credential → error
agent-sql query run "INSERT INTO logs ..." -c prod-ro --write
# Error: "Connection 'prod-ro' uses credential 'readonly' which does not have write permission.
#         To enable writes, use a credential with writePermission: true."
```

### Enforcement Layers

```
SQL Input
  │
  ├─ [Layer 1: PG Session Guard] (PostgreSQL read-only mode only)
  │   Uses libpg-query (PG's actual parser compiled to WASM)
  │   Blocks session escape attempts that could disable read-only mode
  │   NOT used for: SQLite (unnecessary), PG write mode (unnecessary)
  │
  ├─ [Layer 2: Database-Level Enforcement] (primary safety layer)
  │   PostgreSQL (read mode): default_transaction_read_only=on + BEGIN READ ONLY
  │   PostgreSQL (write mode): normal transaction
  │   SQLite (read mode): { readonly: true } — OS-level, cannot be bypassed by any SQL
  │   SQLite (write mode): { readwrite: true, create: false }
  │
  └─ [Layer 3: Error Messages]
      Clear, actionable messages explaining WHY a query was blocked
      and what the user (not the LLM) needs to do to enable writes
```

---

## PG Session Guard (libpg-query)

### Why it exists

PostgreSQL's `default_transaction_read_only=on` can be bypassed by:
- `SET default_transaction_read_only = off`
- `SET transaction_read_only = off`
- `BEGIN READ WRITE`

The LLM constructs arbitrary SQL, so it could (intentionally or not) include these escape statements. The session guard uses PostgreSQL's own parser (compiled to WASM via `libpg-query`) to detect and block them.

### Why only for PG read-only mode

- **SQLite**: `SQLITE_OPEN_READONLY` is OS-level and cannot be bypassed by any SQL statement. No guard needed.
- **PG write mode**: No read-only session to escape from. No guard needed.

### Allowlist approach

Rather than denylisting individual escape vectors (and inevitably missing one), the session guard uses an **allowlist** of permitted AST node types. Anything not on the list is rejected.

Using `libpg-query`'s `parseSync()` which returns typed AST nodes:

**Allowed statement types (read-only mode):**

| AST Node | SQL | Notes |
|---|---|---|
| `SelectStmt` | `SELECT ...` | Core read operation |
| `ExplainStmt` | `EXPLAIN ...` | Query plan inspection (inner stmt validated recursively) |
| `VariableShowStmt` | `SHOW ...` | Show config values |
| `CopyStmt` (direction=TO only) | `COPY ... TO` | Data export (read operation) |

**Always blocked:**

| Check | Reason |
|---|---|
| `stmts.length > 1` | Multi-statement injection |
| `SelectStmt` with `INTO` clause | `SELECT ... INTO` creates a table |
| `SelectStmt` with locking clause | `SELECT ... FOR UPDATE/SHARE` takes locks |
| `ExplainStmt` wrapping a non-allowed inner stmt | `EXPLAIN ANALYZE DELETE ...` |

Everything else — `VariableSetStmt` (SET/RESET), `TransactionStmt`, `DiscardStmt`, `LoadStmt`, any DML/DDL — is rejected by virtue of not being on the allowlist. This covers:
- `SET default_transaction_read_only = off`
- `RESET ALL` / `DISCARD ALL`
- `BEGIN READ WRITE`
- `LOAD` (shared libraries)
- `SELECT set_config(...)` — caught because `set_config` is a regular function call within a `SelectStmt`, which IS allowed. However, `set_config` is neutralized by the database-level `BEGIN READ ONLY` transaction wrapping (PG blocks `set_config` on read-only GUCs within a read-only transaction).

The allowlist is inherently safe against future PG features — new statement types are blocked by default until explicitly permitted.

### Performance

- `libpg-query` WASM: **0.014 ms/parse** (sync), 1.2 MB install, zero binary size overhead in `bun build --compile`
- One-time `loadModule()` call at startup
- Negligible latency vs. the actual database round-trip

### Why not a custom lexer

PostgreSQL has dollar-quoted strings (`$$...$$`, `$tag$...$tag$`), nested block comments, and other syntax that's easy to get wrong in a hand-rolled lexer. `libpg-query` is PostgreSQL's actual parser — correctness is guaranteed, and we get updates for free by bumping the dependency.

---

## CLI Command Structure

### Complete Command Map

```
agent-sql
├── run "<sql>"                          # top-level alias → query run
│
├── connection                           # human-only, not in skill
│   ├── add <alias>                      # --driver pg|sqlite --host --port --database
│   │                                    #   --path (sqlite) --url (pg connection string)
│   │                                    #   --credential --default
│   ├── remove <alias>
│   ├── update <alias>                   # same flags as add, all optional
│   ├── list
│   ├── test [alias]                     # no alias = test default connection
│   ├── set-default <alias>
│   └── usage
│
├── credential                           # human-only, not in skill
│   ├── add <name>                       # --username --password --write
│   ├── remove <name>
│   ├── list                             # passwords masked, writePermission shown
│   └── usage
│
├── config                               # in skill
│   ├── get <key>
│   ├── set <key> <value>
│   ├── reset
│   ├── list-keys
│   └── usage
│
├── schema                               # in skill
│   ├── tables                           # --include-system
│   ├── describe <table>                 # --full (adds constraints, indexes, comments)
│   ├── indexes [table]
│   ├── constraints [table]              # --type pk|fk|unique|check
│   ├── search <pattern>
│   ├── dump                             # --tables <list> --include-system
│   └── usage
│
├── query                                # in skill
│   ├── run "<sql>"                      # --limit --write --compact
│   ├── sample <table>                   # --limit (default 5) --where --compact
│   ├── explain "<sql>"                  # --analyze
│   ├── count <table>                    # --where "condition"
│   └── usage
│
├── usage                                # top-level LLM reference card
├── --help
└── --version

Global flags (all commands): -c/--connection  --format  --expand  --full  --timeout
```

### Global Options

| Flag | Short | Description |
|---|---|---|
| `--connection <alias>` | `-c` | Connection to use |
| `--format <fmt>` | | Output format: `json`, `yaml`, `csv` (default: `json`) |
| `--expand <fields>` | | Comma-separated fields to show untruncated |
| `--full` | | Expand all truncated fields |
| `--timeout <ms>` | | Query timeout override |
| `--version` | `-V` | Print version and exit |
| `--help` | `-h` | Print help and exit |

### Top-Level Alias

`agent-sql run "<sql>"` is a top-level alias for `agent-sql query run "<sql>"`. The most frequently used command should be the shortest. Both forms work identically.

### Command Groups

#### `connection` — Manage database connections (human-only, not in skill)
| Subcommand | Description |
|---|---|
| `add <alias> [connection-string]` | Add a connection. Optional positional URL or file path auto-detects driver and parses host/port/database/account/schema/warehouse/role. Flags override parsed values. |
| `remove <alias>` | Remove a connection |
| `update <alias>` | Update connection fields (same flags as add, all optional) |
| `list` | List all connections |
| `test [alias]` | Test connectivity (no alias = default connection, output includes which connection is being tested) |
| `set-default <alias>` | Set the default connection |
| `usage` | LLM-optimized connection docs |

`connection add <alias> [connection-string]` — the optional second positional argument accepts a URL or file path. Driver is auto-detected from the scheme; host/port/database/account/schema/warehouse/role are parsed from the URL. Flags override anything parsed from the connection string. Examples:
```bash
agent-sql connection add mydb postgres://localhost:5432/myapp --credential pg-cred
agent-sql connection add mydb mysql://localhost/myapp --credential mysql-cred
agent-sql connection add local ./data.db
agent-sql connection add sf "snowflake://org-acct/DB/PUBLIC?warehouse=WH" --credential sf-cred
```

Flags (all optional when a connection string is provided):
- `--driver pg|sqlite|mysql|snowflake` (auto-detected from connection string scheme if omitted)
- `--host`, `--port`, `--database` (PG/MySQL individual fields)
- `--url <connection-string>` (alternative to positional argument; auto-detects driver)
- `--path /absolute/path.sqlite` (SQLite, resolved to absolute at add time)
- `--account <id>`, `--warehouse <wh>`, `--role <role>`, `--schema <schema>` (Snowflake fields)
- `--credential <name>` (optional, references a Keychain credential)
- `--default` (set as default connection)

Bun.SQL auto-detects the adapter from connection strings: `postgres://`/`postgresql://` → PG, `mysql://`/`mariadb://` → MySQL, `sqlite://`/file path → SQLite, `snowflake://` → Snowflake. We leverage this for auto-detection from both the positional argument and `--url`.

#### `credential` — Manage credentials (human-only, not in skill)
| Subcommand | Description |
|---|---|
| `add <name>` | Add credential (--username, --password, --write to enable write permission) |
| `remove <name>` | Remove a credential |
| `list` | List credentials (passwords masked, writePermission shown) |
| `usage` | LLM-optimized credential docs |

Note: SQLite credentials have no username/password — they exist solely to carry `writePermission`.

#### `config` — Persistent settings (in skill)
| Subcommand | Description |
|---|---|
| `get <key>` | Get a config value |
| `set <key> <value>` | Set a config value |
| `reset` | Reset all settings to defaults |
| `list-keys` | List valid config keys with descriptions |
| `usage` | LLM-optimized config docs |

#### `schema` — Database structure discovery (in skill)
| Subcommand | Description |
|---|---|
| `tables` | List all tables with namespace (PG shows `public.users`, etc.). `--include-system` for `pg_catalog`/`information_schema`. |
| `describe <table>` | Show columns, types, nullability, defaults. Supports dot notation: `schema describe analytics.events`. `--full` adds constraints, indexes, and comments. |
| `indexes [table]` | Show indexes. All tables if no table specified. Dot notation supported. |
| `constraints [table]` | Show constraints — PKs, FKs, unique, check. `--type pk\|fk\|unique\|check` to filter. All tables if no table specified. Dot notation supported. |
| `search <pattern>` | Search table and column names by pattern (e.g., `agent-sql schema search user`). |
| `dump` | Full schema dump (all tables, columns, indexes, constraints). `--tables analytics.events,public.users` to filter. `--include-system` for system schemas. |
| `usage` | LLM-optimized schema docs |

Each subcommand is independently useful — an LLM can ask "just show me the indexes" without pulling a full dump. The `dump` command combines everything for initial exploration.

`describe` shows compact column info by default. `--full` adds constraints, indexes, and comments for the described table.

Foreign keys are included in `constraints` output with `"type": "foreign_key"`. No separate `foreign-keys` command — FKs are constraints, and LLMs won't reliably distinguish when to use a separate command.

**PG namespace handling:** All schema commands use dot notation for PG namespaces (`schema.table`). System schemas (`pg_catalog`, `information_schema`) are excluded by default; `--include-system` shows them. `schema tables` output includes the namespace column so it's always visible. SQLite has no namespaces — dot notation is simply not needed.

#### `query` — Execute SQL queries (in skill)
| Subcommand | Description |
|---|---|
| `run "<sql>"` | Execute a SQL query (also available as top-level `agent-sql run`) |
| `sample <table>` | Return sample rows from a table. `--limit` (default 5), `--where "condition"`. No SQL required. Dot notation supported. |
| `explain "<sql>"` | Run EXPLAIN on a query. `--analyze` for EXPLAIN ANALYZE (read-only queries only). |
| `count <table>` | Count rows. `--where "condition"` to filter. Dot notation supported. |
| `usage` | LLM-optimized query docs |

Query-specific options:
- `--limit <n>` — max rows to return (default from config)
- `--write` — opt in to write mode (requires credential with writePermission, or credential-less for SQLite)
- `--compact` — array-of-arrays row format for large results (saves tokens)

#### `usage` — Top-level LLM reference (in skill)
Prints a concise reference card covering all commands, connection setup, and safety notes. The skill exposes this as the entry point for LLM discovery.

---

## Output Format

Five output formats are supported: JSONL (default), JSON, YAML, CSV, and compact.

- **JSONL** — default, optimized for LLM consumption. One JSON object per line, every row self-contained with column names as keys. Streams naturally and pipes well (`jq .NAME` per-line). Query-results-only (`run`, `sample`); non-tabular output falls back to JSON.
- **JSON** — structured envelope (`{columns, rows}`), good for programmatic consumption.
- **YAML** — human-readable, good for debugging and inspection. Same structure as JSON.
- **CSV** — for export/spreadsheet workflows. Only applicable to query results (`run`, `sample`). Schema, config, and error output falls back to JSON or YAML.

### Format Resolution

Resolution order: `--format` flag > `defaults.format` config key > `jsonl`

```bash
# Explicit flag (highest priority)
agent-sql run "SELECT * FROM users" --format yaml

# Persistent default via config
agent-sql config set defaults.format yaml

# Errors are always JSON, regardless of format setting — LLMs need structured errors
```

### Format Applicability

| Output type | JSONL | JSON | YAML | CSV |
|---|---|---|---|---|
| Query results (`run`, `sample`) | Yes (default) | Yes | Yes | Yes |
| Schema output | No (falls back to JSON) | Yes | Yes | No (falls back to YAML/JSON) |
| Config output | No (falls back to JSON) | Yes | Yes | No (falls back to YAML/JSON) |
| Write operation results | No (falls back to JSON) | Yes | Yes | No (falls back to YAML/JSON) |
| Errors (stderr) | Never | Always | Never | Never |

### Implementation

A `formatOutput(data, format)` function in `src/lib/output.ts` handles format dispatch:

- **JSON**: existing `printJson` behavior
- **YAML**: lightweight key-value serializer for our structured output (small dep like `yaml` if needed — Bun has no built-in YAML serializer)
- **CSV**: header row + data rows. Nulls rendered as empty cells. Strings containing commas, newlines, or quotes are quoted per RFC 4180.

### Query Results (default format)

```json
{
  "columns": ["id", "name", "email", "bio"],
  "rows": [
    { "id": 1, "name": "Alice", "email": "alice@example.com", "bio": "Software eng…", "@truncated": { "bio": 12345 } },
    { "id": 2, "name": "Bob", "email": "bob@example.com", "bio": null }
  ],
  "pagination": {
    "hasMore": true,
    "rowCount": 20
  }
}
```

### Query Results (YAML format, `--format yaml`)

```yaml
columns:
  - id
  - name
  - email
  - bio
rows:
  - id: 1
    name: Alice
    email: alice@example.com
    bio: "Software eng…"
    "@truncated":
      bio: 12345
  - id: 2
    name: Bob
    email: bob@example.com
    bio: null
pagination:
  hasMore: true
  rowCount: 20
```

### Query Results (CSV format, `--format csv`)

```csv
id,name,email,bio
1,Alice,alice@example.com,"Software eng…"
2,Bob,bob@example.com,
```

CSV notes:
- Nulls are rendered as empty cells
- Truncation metadata and pagination are not included (CSV is a flat data format)
- Strings containing commas, newlines, or double quotes are quoted per RFC 4180
- Only applicable to query results (`run`, `sample`). Other commands fall back to JSON/YAML.

### Query Results (compact format, `--compact`)

For large result sets where token efficiency matters. Column names appear once; rows are arrays:

```json
{
  "columns": ["id", "name", "email", "bio"],
  "rows": [
    [1, "Alice", "alice@example.com", "Software eng…"],
    [2, "Bob", "bob@example.com", null]
  ],
  "truncated": { "bio": [12345, null] },
  "pagination": { "hasMore": true, "rowCount": 20 }
}
```

In compact mode, `truncated` is a top-level object mapping column names to arrays of original lengths (parallel to `rows`). `null` means no truncation for that row.

### Truncation

- Strings exceeding `truncation.maxLength` (default 200) are truncated with `…`
- Default format: each row with truncated values gets `"@truncated": { "column": originalLength }` — structured JSON, trivially machine-readable
- Compact format: `"truncated"` is a top-level parallel array per column
- `--full` expands all fields; `--expand field1,field2` expands specific fields
- Rows with no truncated fields have no `@truncated` key (default format)

### NULL handling

**NULLs are preserved in query results.** Unlike MongoDB (where documents have variable fields), SQL rows have fixed columns. Pruning nulls would make the LLM think a column doesn't exist. `"bio": null` is meaningful and distinct from a missing key.

NULL pruning is still applied to admin/config output where the schema is less rigid.

### Error Output (stderr)

```json
{
  "error": "Query blocked: INSERT statements are not allowed in read-only mode.",
  "hint": "Connection 'prod' uses read-only credential 'prod-readonly'. Write operations require a credential with writePermission: true.",
  "fixable_by": "human"
}
```

The `fixable_by` field tells the LLM whether to self-correct or escalate:
- `"agent"` — the LLM can fix this (typo in table name, wrong syntax, etc.). Error includes valid alternatives.
- `"human"` — requires human action (permission change, credential setup, config change)
- `"retry"` — transient error (timeout, connection lost), worth retrying

All "not found" errors include valid alternatives (available connections, tables, columns, config keys) so the LLM can self-correct.

### Write Operation Output

```json
{
  "result": "ok",
  "rowsAffected": 5,
  "command": "UPDATE"
}
```

---

## Config File

Location: `~/.config/agent-sql/config.json` (respects `XDG_CONFIG_HOME`)

The config file contains **no sensitive data** — no passwords, no write policies. It is safe for the LLM to read.

```json
{
  "default_connection": "local",
  "connections": {
    "local": {
      "driver": "sqlite",
      "path": "/Users/paul/data/app.sqlite"
    },
    "local-writable": {
      "driver": "sqlite",
      "path": "/Users/paul/data/app.sqlite",
      "credential": "local-rw"
    },
    "prod-pg": {
      "driver": "pg",
      "host": "db.example.com",
      "port": 5432,
      "database": "myapp",
      "credential": "prod-readonly"
    },
    "prod-mysql": {
      "driver": "mysql",
      "host": "mysql.example.com",
      "port": 3306,
      "database": "myapp",
      "credential": "mysql-reader"
    }
  },
  "settings": {
    "defaults": { "format": "json", "limit": 20 },
    "query": { "timeout": 30000, "maxRows": 100 },
    "truncation": { "maxLength": 200 }
  }
}
```

Note: `local` has no credential — defaults to read-only. `local-writable` references a credential in the Keychain. SQLite paths are stored as absolute paths (resolved at `connection add` time).

### Config Keys

| Key | Type | Default | Min | Max | Description |
|---|---|---|---|---|---|
| `defaults.format` | string | `"json"` | — | — | Default output format (`json`, `yaml`, `csv`) |
| `defaults.limit` | number | 20 | 1 | 1000 | Default row limit |
| `query.timeout` | number | 30000 | 1000 | 300000 | Query timeout (ms) |
| `query.maxRows` | number | 100 | 1 | 10000 | Max rows per query |
| `truncation.maxLength` | number | 200 | 50 | 100000 | String truncation threshold |

---

## Connection Resolution

Priority chain (same as agent-mongo):
1. `-c <alias>` CLI flag
2. `AGENT_SQL_CONNECTION` environment variable
3. Config `default_connection`
4. Error listing all available connections

---

## Credential Storage

Credentials are stored **outside** the config file. The config file only references credentials by name.

### macOS (primary)

Keychain via `security` CLI:
- **Service:** `app.paulie.agent-sql`
- **Account:** credential name (e.g., `prod-readonly`)
- **Value:** JSON `{"username":"reader","password":"secret","writePermission":false}`

Username, password, and writePermission are stored together as a single Keychain entry. You can't change writePermission without re-storing the credential (which requires knowing the password).

### Non-macOS fallback

Credentials stored in `~/.config/agent-sql/credentials.json` (separate from config.json). This file contains passwords in plaintext — a warning is printed at `credential add` time on non-macOS platforms.

On these platforms, writePermission is as editable as the password — same security posture. The password is the real gate for PG.

---

## Database Driver Layer

### Abstraction

The abstraction is at the **result** level, not the query level. Each driver uses its own native queries for schema discovery internally, but returns a shared `SchemaInfo` type.

```typescript
type Driver = "pg" | "sqlite" | "mysql"

type QueryResult = {
  columns: string[]
  rows: Record<string, unknown>[]
  rowsAffected?: number
  command?: string
}

type DriverConnection = {
  query(sql: string, opts?: { write?: boolean }): Promise<QueryResult>
  getTables(): Promise<TableInfo[]>
  describeTable(name: string): Promise<ColumnInfo[]>
  getIndexes(table?: string): Promise<IndexInfo[]>
  getConstraints(table?: string): Promise<ConstraintInfo[]>
  close(): Promise<void>
}
```

All three drivers use `import { SQL } from "bun"` for PG/MySQL and `import { Database } from "bun:sqlite"` for SQLite. Bun provides all three natively — zero npm deps for database access.

All drivers use `sql.unsafe(rawString)` for query execution — Bun.SQL's tagged template API doesn't work for CLI-received strings. Parameterization is not a safety layer here; the session guard + read-only transactions are.

**Known limitation:** `sql.unsafe()` does not parse PostgreSQL array types — `array_agg()` returns string literals like `"{a,b}"` instead of JS arrays. The tagged template path (`sql\`...\``) does parse arrays correctly (since Bun 1.2.4, PR #17094), but we can't use it for dynamic SQL. Workaround: `parsePgArray()` in `pg/schema.ts` handles both formats. Future fix: if Bun adds array parsing to the unsafe path, the workaround becomes a no-op (the `Array.isArray` check passes through).

### PostgreSQL Driver (v1)

- Connection via `new SQL({ hostname, port, database, username, password, max: 1 })`
- Read mode: `default_transaction_read_only=on` in connection options
- Session guard: `libpg-query` allowlist (blocks session escape attempts)
- Timeout: `statement_timeout` connection parameter (blocked from modification by session guard allowlist)
- Schema discovery: `information_schema` queries
- Connection pool limited to `max: 1` (CLI runs one query and exits)
- Explicit `sql.close()` before exit

### SQLite Driver (v1)

- Connection via `new Database(path, { readonly: true | false })`
- Read mode: `{ readonly: true }` (OS-level `SQLITE_OPEN_READONLY` — bulletproof, no session guard needed)
- Write mode: `{ readwrite: true, create: false }` (allows writes, prevents accidental DB creation)
- Shared cache is never enabled (off by default, must stay off for readonly isolation)
- Schema discovery: `sqlite_master` + `PRAGMA table_info()`, `PRAGMA index_list()`, `PRAGMA foreign_key_list()`
- Explicit `db.close()` before exit

### MySQL Driver

#### Connection

```typescript
// Connection string form
new SQL("mysql://user:pass@host:port/db")

// Options form
new SQL({ adapter: "mysql", hostname, port, database, username, password, max: 1 })
```

Same pattern as PG: pool limited to `max: 1` (CLI runs one query and exits), explicit `sql.close()` before exit. Bun.SQL provides the MySQL driver natively — zero npm deps.

#### Read-Only Enforcement

**No session guard / parser / lexer needed for security.** MySQL's read-only enforcement is the simplest of the three drivers because protocol-level single-statement enforcement + per-query read-only transactions = complete defense with zero parsing.

```
SQL Input
  │
  ├─ [Layer 1: Protocol-Level Single-Statement Enforcement]
  │   multipleStatements: false (Bun.SQL default, server-enforced)
  │   The LLM's query is always exactly one statement — no ; injection
  │
  ├─ [Layer 2: Per-Query Read-Only Transaction Wrapping]
  │   Driver issues three separate calls per query:
  │     START TRANSACTION READ ONLY
  │     <user query>              ← single statement, protocol-enforced
  │     COMMIT
  │   Cannot be escaped mid-transaction (ERROR 1568 on SET TRANSACTION
  │   inside active transaction; ERROR 1792 on any write attempt)
  │
  ├─ [Layer 3: Session-Level Default (belt-and-suspenders)]
  │   On connect: SET SESSION TRANSACTION READ ONLY
  │   Bypassable by SET SESSION TRANSACTION READ WRITE, but harmless —
  │   every query gets its own Layer 2 wrapping regardless
  │
  └─ [Layer 4: Error Messages]
      Catch ERROR 1792 (SQLSTATE 25006): "Cannot execute statement in a READ ONLY transaction"
      Surface clear error with fixable_by classification
```

**Why this is complete without a parser:**

1. **Only one statement executes** — protocol-enforced at the MySQL wire level. The LLM cannot inject `; SET SESSION TRANSACTION READ WRITE; INSERT INTO ...`.
2. **That statement runs inside an inescapable read-only transaction** — `START TRANSACTION READ ONLY` cannot be modified mid-transaction. `SET TRANSACTION READ WRITE` inside an active transaction returns ERROR 1568.
3. **Even if the single statement is `SET SESSION TRANSACTION READ WRITE`** — it only affects future transactions, and every future query gets its own `START TRANSACTION READ ONLY` wrapping, making the escape pointless.
4. **Executable comments (`/*! ... */`)** — not a security concern. They expand into SQL that the server executes, but since only one statement runs (protocol-enforced) and it runs inside a read-only transaction, executable comments cannot escape the sandbox.
5. **`SHOW` statements** are allowed in read-only transactions (they are read operations).

**Write mode:** skip the transaction wrapping. Detect write commands (`INSERT`, `UPDATE`, `DELETE`, `REPLACE`) for `rowsAffected` / `command` in the result.

See `design-docs/agent-sql/mysql-readonly-research.md` for the full escape vector analysis.

#### Schema Discovery

All schema queries use `information_schema` for consistency with the PG driver. `SHOW` variants noted as alternatives.

**`getTables`**
```sql
SELECT table_name
FROM information_schema.tables
WHERE table_schema = DATABASE()
  AND table_type = 'BASE TABLE'
ORDER BY table_name
```
Alternative: `SHOW TABLES`. No namespace prefix in output — MySQL uses databases (not schemas), and a connection is already scoped to a database.

**`describeTable`**
```sql
SELECT
  c.column_name,
  c.column_type,        -- MySQL: use column_type (includes size) not data_type
  c.is_nullable,
  c.column_default,
  c.column_key          -- 'PRI' for primary key
FROM information_schema.columns c
WHERE c.table_schema = DATABASE()
  AND c.table_name = ?
ORDER BY c.ordinal_position
```
Alternative: `SHOW COLUMNS FROM <table>`. Uses `column_type` (e.g., `varchar(255)`, `int unsigned`) rather than `data_type` (e.g., `varchar`, `int`) for richer type info.

**`getIndexes`**
```sql
SELECT
  index_name,
  table_name,
  GROUP_CONCAT(column_name ORDER BY seq_in_index) AS columns,
  NOT non_unique AS is_unique
FROM information_schema.statistics
WHERE table_schema = DATABASE()
GROUP BY table_name, index_name, non_unique
ORDER BY table_name, index_name
```
Alternative: `SHOW INDEX FROM <table>`. `GROUP_CONCAT` aggregates multi-column indexes into a single row. Split the `columns` string on `,` in the driver.

**`getConstraints`**
```sql
SELECT
  tc.constraint_name,
  tc.table_name,
  tc.constraint_type,
  GROUP_CONCAT(kcu.column_name ORDER BY kcu.ordinal_position) AS columns,
  kcu.referenced_table_name,
  GROUP_CONCAT(kcu.referenced_column_name ORDER BY kcu.ordinal_position) AS referenced_columns
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON kcu.constraint_name = tc.constraint_name
  AND kcu.table_schema = tc.table_schema
  AND kcu.table_name = tc.table_name
WHERE tc.table_schema = DATABASE()
GROUP BY tc.constraint_name, tc.table_name, tc.constraint_type,
         kcu.referenced_table_name
ORDER BY tc.table_name, tc.constraint_name
```
For FK details (update/delete rules), join `information_schema.referential_constraints`:
```sql
LEFT JOIN information_schema.referential_constraints rc
  ON rc.constraint_name = tc.constraint_name
  AND rc.constraint_schema = tc.table_schema
```
Note: MySQL does not support `CHECK` constraints in `information_schema.table_constraints` before 8.0.16. On older versions, check constraints are silently ignored.

**`searchSchema`**
```sql
-- Search table names
SELECT table_name
FROM information_schema.tables
WHERE table_schema = DATABASE()
  AND table_name LIKE ?
ORDER BY table_name

-- Search column names
SELECT table_name, column_name
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND column_name LIKE ?
ORDER BY table_name, column_name
```
Uses `LIKE` with `%pattern%` (case-insensitive by default in MySQL's default collation).

#### MySQL-Specific Considerations

- **No namespace/schema dot notation.** MySQL uses databases, not schemas — a connection is already scoped to a database via the connection string or `database` option. All `information_schema` queries filter on `table_schema = DATABASE()`. The `schema` field on `TableInfo`/`IndexInfo`/`ConstraintInfo` is left `undefined`.
- **Executable comments (`/*! ... */`)** — MySQL executes content inside these as SQL. Not a security concern for read-only enforcement (per-query transaction wrapping + single-statement protocol handle it), but worth noting for anyone reading query logs.
- **`SHOW` statements** — allowed in read-only transactions. Used as fallback alternatives for schema discovery.
- **`REPLACE` statement** — MySQL-specific write operation (insert-or-update). Blocked by read-only transaction like any other DML.
- **`LOAD DATA INFILE`** — write operation, blocked by read-only transaction (ERROR 1792).
- **Backtick identifier quoting** — MySQL uses `` `identifier` `` by default. The driver's `quoteIdent` helper uses backticks (not double-quotes as in PG/SQLite).

#### Error Codes

| MySQL Error | SQLSTATE | Meaning | `fixable_by` |
|---|---|---|---|
| ERROR 1792 | 25006 | Write attempt in READ ONLY transaction | `"human"` |
| ERROR 1568 | 25001 | Transaction characteristics change mid-transaction | `"human"` |
| ERROR 1146 | 42S02 | Table doesn't exist | `"agent"` (include valid tables) |
| ERROR 1054 | 42S22 | Unknown column | `"agent"` (include valid columns) |
| ERROR 2002/2003 | HY000 | Connection refused / Can't connect | `"retry"` or `"human"` |
| ERROR 1045 | 28000 | Access denied (bad credentials) | `"human"` |

#### Testing

- Needs Docker MySQL instance, similar to PG tests (`docker run --rm -e MYSQL_ROOT_PASSWORD=... -p 3306:3306 mysql:8`)
- Test file: `test/drivers/mysql.test.ts`
- Tests mirror PG driver tests: read-only enforcement, write blocking, schema discovery, error mapping
- Key read-only tests:
  - `SELECT` succeeds inside read-only transaction
  - `INSERT`/`UPDATE`/`DELETE`/`REPLACE` return ERROR 1792
  - `SET SESSION TRANSACTION READ WRITE` followed by `SELECT` still executes inside read-only transaction (escape attempt is futile)
  - `SHOW TABLES` / `SHOW COLUMNS` succeed in read-only mode
- CI: Docker MySQL service, same pattern as PG Docker service

### Per-driver error mapping

Each database has its own error system:
- PG: SQLSTATE codes (e.g., `42P01` undefined table, `25006` read-only violation, `57014` query cancelled)
- SQLite: integer error codes (e.g., `SQLITE_READONLY` 8, `SQLITE_ERROR` 1)
- MySQL: error numbers + SQLSTATE (e.g., `1290` read-only mode, `1146` table doesn't exist)

The `errors.ts` enhancement layer normalizes these into user-friendly messages with `fixable_by` classification. Connection errors sanitize hostnames (use the alias, not internal hostnames).

---

## Dependencies

### Runtime (2)
| Package | Purpose |
|---|---|
| `commander` | CLI framework |
| `libpg-query` | PostgreSQL's parser (WASM) — session escape detection for PG read-only mode |

Bun provides PostgreSQL, SQLite, and MySQL drivers natively — no additional packages needed.

### Dev
| Package | Purpose |
|---|---|
| `typescript` | Type checking |
| `bun-types` | Bun type definitions |
| `oxlint` | Linting |
| `oxfmt` | Formatting |
| `simple-git-hooks` | Pre-commit hooks |

---

## Project Structure

```
src/
  index.ts                    # CLI entry point (registers commands, top-level `run` alias)
  cli/
    connection/               # connection add/remove/update/list/test/set-default/usage
    credential/               # credential add/remove/list/usage
    config/                   # config get/set/reset/list-keys/usage
    schema/                   # schema tables/describe/indexes/constraints/search/dump/usage
    query/                    # query run/sample/explain/count/usage
    usage/                    # Top-level LLM usage reference
  lib/
    config.ts                 # Config file I/O (connections + settings only, no credentials)
    credentials.ts            # Credential storage (Keychain on macOS, file fallback)
    output.ts                 # formatOutput, printJson, printYaml, printCsv, printPaginated, printError, printCompact
    truncation.ts             # applyTruncation (with @truncated structured object)
    errors.ts                 # SQL error enhancement, fixable_by classification
    timeout.ts                # Timeout resolution
    keychain.ts               # macOS Keychain integration
    pg-session-guard.ts       # PG read-only session guard (libpg-query, allowlist)
    version.ts                # Version resolution
  drivers/
    types.ts                  # DriverConnection interface, QueryResult, SchemaInfo types
    pg.ts                     # PostgreSQL driver (Bun.SQL, sql.unsafe())
    sqlite.ts                 # SQLite driver (bun:sqlite)
    mysql.ts                  # MySQL driver (Bun.SQL, per-query READ ONLY transactions)
    resolve.ts                # Driver resolution from connection config
skills/
  agent-sql/
    SKILL.md                  # Claude Code skill (exposes query, schema, usage only)
    references/
      commands.md             # Command reference
      output.md               # Output format reference
test/
  pg-session-guard.test.ts    # Allowlist tests — critical, security-relevant
  output.test.ts
  truncation.test.ts
  config.test.ts
  credential.test.ts
  errors.test.ts              # Error mapping and fixable_by tests
  drivers/
    sqlite.test.ts            # SQLite driver (readonly enforcement, schema, queries — uses temp files)
    pg.test.ts                # PG driver (CI-only, needs real PG via Docker)
    mysql.test.ts             # MySQL driver (CI-only, needs real MySQL via Docker)
scripts/
  build-release-assets.sh     # Cross-platform binary builds
  release.sh                  # Version bump, tag, push
```

---

## Reuse from agent-mongo

| Component | Approach |
|---|---|
| `truncation.ts` | Adapt (structured `@truncated` object instead of companion keys) |
| `timeout.ts` | Direct copy |
| `output.ts` | Adapt (add compact mode, preserve nulls for query output, add `fixable_by` to errors) |
| `keychain.ts` | Copy, change service name |
| `config.ts` | Rewrite (credentials removed from config, stored separately) |
| `valid-keys.ts` | Adapt keys for SQL |
| `credential/add.ts` | Rewrite (keychain-centric storage, --write flag) |
| `connection/add.ts` | Rewrite (host/port/path/url, absolute path resolution) |
| `errors.ts` | Rewrite (per-driver error mapping, fixable_by, sanitize hostnames) |
| CLI registration pattern | Same pattern, different commands, top-level `run` alias |
| Build/release scripts | Copy, change binary name |

Note: `compact-json.ts` (`pruneEmpty`) is NOT reused for query results — SQL nulls must be preserved. May still be useful for config/admin output.

---

## SSH Tunnels

**Deferred from v1.** For now, users can establish SSH tunnels externally and point agent-sql at `localhost:<forwarded-port>`. The skill documentation should mention this pattern:

```bash
# User establishes tunnel externally
ssh -L 5433:db.internal:5432 bastion &

# agent-sql connects to forwarded port
agent-sql connection add prod --driver pg --host localhost --port 5433 --database myapp --credential prod-ro
```

If SSH tunnel support is added later, it would live in the driver layer as a connection option.

---

## Resolved Decisions

1. **Credential storage** — credentials (username, password, writePermission) stored together in macOS Keychain, not in config file. Config file contains zero sensitive data. LLM can't escalate by editing config. Creating a useful PG write credential requires knowing the database password.

2. **PG session guard** — allowlist approach (permit SelectStmt, ExplainStmt, VariableShowStmt, CopyStmt TO) rather than denylisting escape vectors. Inherently safe against future PG features.

3. **Truncation** — structured `@truncated` object per row: `{ "bio": 12345 }`. Self-describing, trivially machine-readable. Compact mode uses parallel arrays.

4. **NULL handling** — NULLs preserved in query results (SQL rows have fixed columns). Only pruned in config/admin output.

5. **Credential-less SQLite** — no credential = read-only. `--write` with no credential allowed for SQLite only (local file). PG always requires a credential.

6. **Query parameterization** — deferred from v1. The LLM constructs arbitrary SQL strings; SQL injection from the LLM into its own read-only query isn't a real threat vector.

7. **Output formats** — three formats supported: JSON (default, LLM-optimized), YAML (human-readable, debugging), CSV (export/spreadsheet). Global `--format json|yaml|csv` flag, persistent default via `defaults.format` config key. Resolution: `--format` flag > `defaults.format` config > `json`. CSV only applies to query results (`run`, `sample`) — schema/config output falls back to JSON/YAML. Errors are always JSON regardless of format setting (LLMs need structured errors). All format logic in `lib/output.ts` via `formatOutput(data, format)`.

8. **Schema targeting** — each schema subcommand (`indexes`, `constraints`) works independently. FKs merged into `constraints` with a `type` field. `dump` combines everything with `--tables` filter. PG namespaces use dot notation (`schema.table`), no `--schema` flag.

9. **SQL validation** — `libpg-query` (PG's actual WASM parser) for PG read-only mode only. Not used for SQLite (OS-level readonly is bulletproof) or PG write mode (nothing to escape from).

10. **Raw SQL execution** — PG driver uses `sql.unsafe(rawString)`. Parameterization is not a safety layer; the session guard + read-only transaction are.

11. **Top-level `run` alias** — `agent-sql run "SQL"` as shorthand for `agent-sql query run "SQL"`. Most-used command should be shortest.

12. **Error classification** — `fixable_by` field (`"agent"` / `"human"` / `"retry"`) tells LLM whether to self-correct or escalate. All "not found" errors include valid alternatives.

13. **Skill boundary** — skill exposes query, schema, config get, connection list/test, and usage only. Credential and connection mutation commands are human-only (not in skill, but well-documented in `--help`).

14. **MySQL support** — fully designed-in as a supported driver (`"driver": "mysql"`). Bun.SQL supports MySQL natively with zero deps. Same `DriverConnection` interface — adding MySQL is implementing the interface, no session guard needed. `--url mysql://...` auto-detects the driver.

## Open Questions

1. **SSH tunnels** — deferred from v1. Skill docs should mention the external tunnel pattern. Worth building in later?

2. **`bun build --compile` + WASM** — needs early verification that `libpg-query`'s WASM module embeds correctly in standalone binaries. Test on clean machine before investing in implementation.

3. **`COPY TO` in read-only mode** — allowed by the session guard (it's a read operation), but can dump large amounts of data. Should `maxRows` apply to COPY, or is this out of scope?
