# Snowflake Driver Design Document

## Overview

Add Snowflake support to agent-sql as a fourth driver, using a **custom lightweight driver** that talks directly to Snowflake's SQL REST API v2 (`/api/v2/statements`). No SDK dependency — pure HTTP + JSON via `fetch()`.

### Why not use `snowflake-sdk`?

The official Node.js SDK (`snowflake-sdk`) is a 143MB dependency tree (237 packages) that drags in AWS S3, Azure Blob, Google Cloud Storage SDKs, moment.js, axios, winston, and more. All of that exists for file staging (PUT/GET) that we don't need. The Go driver (`gosnowflake`) has the same problem — pulls in AWS/Azure/GCS SDKs for staging.

Under the hood, **all Snowflake drivers talk to the same REST API endpoints over HTTPS/JSON**. There is no binary wire protocol. Every SDK (Node.js, Python, Go, JDBC, ODBC) is ultimately just an HTTP client. We can go direct.

### Why a custom driver makes sense

- **Zero new dependencies** — `fetch()` for HTTP, Bun's native `crypto` for JWT signing (if needed)
- **Same API surface** — our driver implements `DriverConnection` like PG/MySQL/SQLite
- **Modular and testable** — HTTP transport layer is easily mocked
- **Lightweight** — ~800-1200 lines vs 50k+ in the SDK
- **Bun-native** — no Node.js compatibility concerns, no native addons

### Research sources

Analysis based on:
- Snowflake SQL API v2 official documentation
- `snowflake-sdk` (Node.js) source code at v2.2.0 — inspected auth, transport, query execution, result handling
- `gosnowflake` (Go) source code — inspected auth flows, retry logic, session management, REST endpoints
- Snowflake REST API specs on GitHub
- Snowflake authentication deprecation announcements (single-factor password blocking)

---

## Protocol: Snowflake SQL REST API v2

Snowflake is cloud-native. All drivers (Node.js, Python, Go, JDBC, ODBC) ultimately make HTTPS requests. The SQL API v2 is the official public REST interface.

### Base URL

```
https://<account_identifier>.snowflakecomputing.com/api/v2/statements
```

Account identifier format: `<orgname>-<accountname>` (e.g., `myorg-myaccount`).

### Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/api/v2/statements` | Submit SQL statement |
| `GET` | `/api/v2/statements/{handle}` | Check status / get results |
| `GET` | `/api/v2/statements/{handle}?partition=N` | Get result partition N |
| `POST` | `/api/v2/statements/{handle}/cancel` | Cancel running statement |

Note: The SDKs also use internal endpoints (`/session/v1/login-request`, `/queries/v1/query-request`) but these are undocumented. We use the public SQL API v2 exclusively.

### Request Format

```json
POST /api/v2/statements
Content-Type: application/json
Authorization: Bearer <token>

{
  "statement": "SELECT * FROM my_table WHERE id = ?",
  "timeout": 60,
  "database": "MY_DB",
  "schema": "PUBLIC",
  "warehouse": "MY_WH",
  "role": "MY_ROLE",
  "parameters": {
    "MULTI_STATEMENT_COUNT": "1",
    "QUERY_TAG": "agent-sql"
  },
  "bindings": {
    "1": { "type": "TEXT", "value": "42" }
  }
}
```

### Response Format

**Synchronous (fast queries):**
```json
{
  "code": "090001",
  "statementHandle": "01a...",
  "sqlState": "00000",
  "message": "Statement executed successfully.",
  "resultSetMetaData": {
    "numRows": 3,
    "format": "jsonv2",
    "partitionInfo": [{ "rowCount": 3, "uncompressedSize": 123 }],
    "rowType": [
      { "name": "ID", "type": "fixed", "nullable": false, "scale": 0, "precision": 38 },
      { "name": "NAME", "type": "text", "nullable": true, "length": 256 }
    ]
  },
  "data": [
    ["1", "Alice"],
    ["2", "Bob"]
  ]
}
```

**All values are strings** (or null). The `rowType` metadata provides type info for parsing.

**Async (long-running queries):** Returns HTTP 202 with `"code": "333334"`. Poll `GET /api/v2/statements/{handle}` until status changes.

**Large results:** Split into partitions. First response contains partition 0. Fetch partition 1..N via `GET /api/v2/statements/{handle}?partition=N`. See [Memory and Large Results](#memory-and-large-results) for how we handle this.

---

## Authentication

### Primary: Personal Access Token (PAT)

PATs are Snowflake's intended replacement for password auth (which is being deprecated). A PAT is a bearer token — no signing, no key files, no OAuth dance.

```
Authorization: Bearer <pat_secret>
X-Snowflake-Authorization-Token-Type: PROGRAMMATIC_ACCESS_TOKEN
```

The `X-Snowflake-Authorization-Token-Type` header is optional — Snowflake auto-detects the token type.

**Why PAT is ideal for agent-sql:**
- Simplest possible credential: just a token string
- Created by the Snowflake user: `ALTER USER myuser ADD PROGRAMMATIC ACCESS TOKEN my_token ...`
- Can be scoped to a specific role (e.g., a read-only role)
- Works with the SQL API v2 directly
- Max 365-day lifetime (default 15 days), admin-configurable
- Max 15 PATs per user
- Works for both human users (TYPE=PERSON) and service users (TYPE=SERVICE)
- Stored in our Keychain like any other credential

**Credential shape in Keychain:**
```json
{
  "username": null,
  "password": "<pat_secret>",
  "writePermission": false
}
```

The PAT goes in the `password` field — consistent with how PATs work as password replacements across Snowflake tooling.

### Fallback: Key-Pair JWT

For orgs that mandate key-pair auth:

1. User provides a private key file path (stored in config, not Keychain — it's a file reference)
2. Driver generates JWT at request time:
   - `iss`: `ACCOUNT.USER.SHA256:<pubkey_fingerprint>` (uppercase)
   - `sub`: `ACCOUNT.USER` (uppercase)
   - `iat`: now, `exp`: now + 59 minutes
   - Signed with RS256 using the private key
3. JWT used as bearer token: `Authorization: Bearer <jwt>`
4. Public key fingerprint: SHA256 of DER-encoded PKIX public key, base64-encoded

Bun's native `crypto` module handles RSA key loading and JWT signing — no `jsonwebtoken` dependency needed.

**This is a stretch goal for v1.** PAT auth covers the common case. Key-pair can be added later without changing the driver architecture.

### Not supported: Username + Password (legacy)

Snowflake is deprecating single-factor password auth:
- **April 2025**: Enforcement began for new accounts
- **November 2025**: Full enforcement across all accounts

The legacy `/session/v1/login-request` endpoint is undocumented and requires session management (token refresh via master token, heartbeats every 15-60 minutes). Not worth implementing for a deprecated flow.

---

## Connection Configuration

### Config shape

```json
{
  "snowflake-prod": {
    "driver": "snowflake",
    "account": "myorg-myaccount",
    "database": "LDT_PROD",
    "schema": "PUBLIC",
    "warehouse": "COMPUTE_WH",
    "role": "READ_ONLY",
    "credential": "sf-readonly"
  }
}
```

**Required fields:** `account` (or derivable from URL)
**Optional fields:** `database`, `schema`, `warehouse`, `role` — can also be set per-request via SQL API

### Ad-hoc connections

Ad-hoc Snowflake connections via `-c` URL:

```bash
agent-sql run -c "snowflake://myorg-myaccount/MY_DB/PUBLIC?warehouse=COMPUTE_WH" "SELECT 1"
```

URL format: `snowflake://<account>/<database>/<schema>?warehouse=<wh>&role=<role>`

**Ad-hoc Snowflake connections are always read-only** (consistent with PG/MySQL ad-hoc URL behavior). They require `AGENT_SQL_SNOWFLAKE_TOKEN` env var for auth (since there's no credential alias to look up).

### `connection add` flags

```bash
agent-sql connection add sf-prod \
  --driver snowflake \
  --account myorg-myaccount \
  --database LDT_PROD \
  --schema PUBLIC \
  --warehouse COMPUTE_WH \
  --role READ_ONLY \
  --credential sf-readonly
```

Or via URL:
```bash
agent-sql connection add sf-prod \
  --url "snowflake://myorg-myaccount/LDT_PROD/PUBLIC?warehouse=COMPUTE_WH&role=READ_ONLY" \
  --credential sf-readonly
```

---

## Read-Only Enforcement

Snowflake has **no transaction-level read-only mode** (`BEGIN TRANSACTION READ ONLY` does not exist in Snowflake). No session parameter equivalent to PG's `default_transaction_read_only` or MySQL's `SET SESSION TRANSACTION READ ONLY`. The Go driver explicitly rejects read-only transactions with `ErrNoReadOnlyTransaction`.

### Layer 1: Client-side keyword allowlist (primary)

A lightweight statement-type detector — same approach as MySQL's `detectCommand()` but inverted to an allowlist:

**Allowed in read-only mode:**
- `SELECT` (including CTEs via `WITH ... SELECT`)
- `SHOW` (metadata queries: `SHOW TABLES`, `SHOW COLUMNS`, etc.)
- `DESCRIBE` / `DESC` (table/column metadata)
- `EXPLAIN` (query plans)
- `LIST` / `LS` (stage listing — read operation)

**Blocked** (everything else): `INSERT`, `UPDATE`, `DELETE`, `MERGE`, `CREATE`, `ALTER`, `DROP`, `TRUNCATE`, `COPY INTO`, `PUT`, `GET`, `GRANT`, `REVOKE`, `CALL`, `EXECUTE`, etc.

The allowlist is safe by default — new Snowflake statement types are blocked until explicitly permitted.

### Layer 2: Multi-statement prevention

Set `MULTI_STATEMENT_COUNT=1` on **every request, including write mode**. This is Snowflake's built-in server-side protection — the server rejects payloads with multiple semicolon-separated statements. No reason to allow multi-statement injection regardless of read/write mode.

### Layer 3: Role-based (documented recommendation)

Document that for maximum security, users should connect with a Snowflake role that only has `SELECT` + `USAGE` privileges. The driver can't enforce this, but error messages can suggest it via `fixable_by: "human"`.

Example role setup (for docs):
```sql
CREATE ROLE read_only;
GRANT USAGE ON DATABASE d1 TO ROLE read_only;
GRANT USAGE ON SCHEMA d1.s1 TO ROLE read_only;
GRANT SELECT ON ALL TABLES IN SCHEMA d1.s1 TO ROLE read_only;
GRANT SELECT ON FUTURE TABLES IN SCHEMA d1.s1 TO ROLE read_only;
GRANT USAGE ON WAREHOUSE w1 TO ROLE read_only;
```

### Why not use libpg-query?

Snowflake SQL is not PostgreSQL-compatible enough. Snowflake has `FLATTEN`, `QUALIFY`, `VARIANT` types, cross-database references, `COPY INTO`, stage operations, etc. that would fail to parse. A keyword-based allowlist is appropriate here — it's the same level of enforcement as MySQL (which also lacks a parser) and is sufficient given the multi-statement prevention.

### Honest assessment

Snowflake read-only enforcement is the **weakest of the four drivers**:
- PG: server-enforced `BEGIN READ ONLY` + client-side parser (strongest)
- SQLite: OS-level `SQLITE_OPEN_READONLY` (strongest, cannot be bypassed)
- MySQL: server-enforced `START TRANSACTION READ ONLY` + protocol single-statement (strong)
- Snowflake: client-side keyword allowlist + server-enforced `MULTI_STATEMENT_COUNT=1` (moderate)

The keyword allowlist prevents accidental writes and blocks naive bypass attempts. The real security boundary is the Snowflake role — if the PAT is scoped to a read-only role, the server rejects writes regardless of what we send. Our docs should strongly recommend this.

### Comparison with other drivers

| Layer | PG | MySQL | SQLite | Snowflake |
|-------|-----|-------|--------|-----------|
| DB-level enforcement | `default_transaction_read_only` | `SET SESSION TRANSACTION READ ONLY` | `SQLITE_OPEN_READONLY` (OS) | Role-based (human setup) |
| Per-query wrapping | `BEGIN READ ONLY` | `START TRANSACTION READ ONLY` | N/A | N/A |
| Client-side parsing | `libpg-query` allowlist | `detectCommand` | None needed | Keyword allowlist |
| Multi-statement prevention | Parser rejects | Protocol single-stmt | Single statement | `MULTI_STATEMENT_COUNT=1` |

---

## Write Mode

Write mode for Snowflake follows the same pattern as PG/MySQL:

### Permission gate (identical to other drivers)

Both conditions must be true:
1. Credential has `writePermission: true` in Keychain
2. Caller passes `--write` flag explicitly

```bash
# Read-only (default) — keyword allowlist active
agent-sql run -c sf-prod "SELECT * FROM users"

# Write mode — requires credential with writePermission + --write flag
agent-sql run -c sf-prod "INSERT INTO logs ..." --write

# Write credential without --write flag → still read-only (allowlist active)
agent-sql run -c sf-prod "SELECT * FROM users"

# --write with read-only credential → error
agent-sql run -c sf-prod "INSERT INTO logs ..." -c sf-readonly --write
# Error: "Connection 'sf-readonly' uses credential without write permission."
```

### What changes in write mode

| Aspect | Read mode | Write mode |
|--------|-----------|------------|
| Keyword allowlist | Active (SELECT/SHOW/DESCRIBE/EXPLAIN only) | **Skipped** |
| `MULTI_STATEMENT_COUNT=1` | Active | **Still active** (no reason to allow multi-statement) |
| REST API endpoint | Same | Same |
| Auth | Same PAT/JWT | Same PAT/JWT |

Write mode simply removes the client-side keyword allowlist. Everything else stays the same. The `MULTI_STATEMENT_COUNT=1` parameter is always set — there's no legitimate reason to allow multiple statements in a single request, even in write mode.

---

## Memory and Large Results

### The problem

Snowflake can return millions of rows. The REST API splits large results into **partitions** (~10-20MB uncompressed each). Without protection, fetching all partitions would exhaust memory.

### How other drivers handle this today

None of the existing drivers stream. All load results into memory:
- PG: `sql.unsafe()` returns all rows
- MySQL: `sql.unsafe()` returns all rows
- SQLite: `.all()` returns all rows

Memory protection comes from:
- `--limit` flag (default 20 rows)
- `query.maxRows` config (default 100, hard cap applied as `effectiveLimit = Math.min(pageSize, maxRows)`)
- `LIMIT` injected into sample/count queries

### Snowflake's partition model helps

The REST API response includes metadata **before** the actual data:
- `resultSetMetaData.numRows` — total row count
- `partitionInfo` — array describing each partition's `rowCount` and `uncompressedSize`
- Partition 0 data is inline in the initial response
- Partitions 1..N require separate GET requests

This means we can:
1. Read `numRows` from metadata to know the total size
2. Get partition 0 (comes free with the response)
3. **Stop fetching** if we already have enough rows for our limit
4. Only fetch additional partitions if the effective limit demands more rows than partition 0 contains

### Implementation

```
Query submitted → Response received
  ├─ Check numRows vs effectiveLimit
  ├─ Parse partition 0 data (always present)
  ├─ If partition 0 has enough rows → done
  └─ If more needed → fetch partitions 1..N until limit met
       └─ Stop as soon as accumulated rows >= effectiveLimit
```

For `query run` with default settings (limit=20, maxRows=100), we will almost always only need partition 0. Partition fetching only matters for explicit large limits or `--full` scenarios.

### Memory bounds

| Scenario | Partitions fetched | Memory |
|----------|--------------------|--------|
| Default (limit=20) | 1 (partition 0 only) | Minimal |
| Large limit (limit=1000) | 1-2 partitions | ~20-40MB |
| Extreme (limit=10000, maxRows=10000) | Several partitions | Bounded by maxRows config |
| Schema queries | 1 partition (metadata is small) | Minimal |

The `query.maxRows` config (default 100) is the hard upper bound. Even if someone sets `--limit 999999`, `maxRows` caps actual fetching.

> **Note:** Streaming output support is planned (see `design-docs/streaming-output.md`). The Snowflake driver's partition-based fetching is designed to be streaming-friendly — each partition can be emitted as a batch. For v1, results are still collected before output, bounded by `maxRows`.

### Comparison with other drivers

Other drivers rely on the database to handle LIMIT at the query level. Snowflake's partition model gives us an additional server-side boundary — we simply don't fetch partitions we don't need. This is actually *better* than PG/MySQL for memory management on large results, since those drivers receive all rows regardless of LIMIT in certain edge cases (e.g., `sql.unsafe()` with no LIMIT in the query text).

---

## Driver Implementation

### File structure

```
src/drivers/
  snowflake.ts              # connectSnowflake() → DriverConnection
  snowflake/
    client.ts               # HTTP client (fetch wrapper with retry + polling)
    auth.ts                 # PAT + JWT auth token generation
    types.ts                # Snowflake-specific types (API response shapes)
    parse-results.ts        # Convert Snowflake string[][] → typed rows
    read-only-guard.ts      # Keyword allowlist for read-only enforcement
```

### HTTP client (`client.ts`)

Thin wrapper around `fetch()`:
- Base URL construction from account identifier
- Authorization header injection (PAT bearer token)
- Request/response JSON serialization
- Retry with exponential backoff (3 retries, 1-8s, decorrelated jitter — matching Go driver pattern)
- Async polling for long-running queries (HTTP 202 → poll GET until complete; backoff: 0.5s, 0.5s, 1s, 1.5s, 2s, 4s, 5s then steady 5s — matching Go driver pattern)
- Timeout via `AbortController`
- Partition fetching with early termination (stop when enough rows collected)
- Cancel support via `POST /api/v2/statements/{handle}/cancel`

### Auth (`auth.ts`)

**PAT (v1):**
```typescript
function buildAuthHeaders(token: string): Record<string, string> {
  return {
    "Authorization": `Bearer ${token}`,
    "X-Snowflake-Authorization-Token-Type": "PROGRAMMATIC_ACCESS_TOKEN",
  };
}
```

**Key-pair JWT (stretch goal):**
```typescript
async function buildJwtAuthHeaders(opts: {
  account: string;
  user: string;
  privateKeyPath: string;
}): Promise<Record<string, string>> {
  // Load private key, derive public key fingerprint
  // Build JWT: iss=ACCOUNT.USER.SHA256:fingerprint, sub=ACCOUNT.USER
  // Sign with RS256 using Bun's native crypto
  return {
    "Authorization": `Bearer ${jwt}`,
    "X-Snowflake-Authorization-Token-Type": "KEYPAIR_JWT",
  };
}
```

### Result parsing (`parse-results.ts`)

Snowflake returns all values as strings. The parser uses `rowType` metadata to convert:

| Snowflake type | JS type | Notes |
|----------------|---------|-------|
| `fixed` (scale=0) | `number` (integer) | Use `Number()` for safe integers, keep string if >MAX_SAFE_INTEGER |
| `fixed` (scale>0) | `number` (float) | |
| `real` | `number` | |
| `text` | `string` | |
| `boolean` | `boolean` | `"true"` / `"false"` |
| `date` | `string` | Keep as string (ISO 8601) for LLM readability |
| `timestamp_ltz` | `string` | Keep as string |
| `timestamp_ntz` | `string` | Keep as string |
| `timestamp_tz` | `string` | Keep as string |
| `time` | `string` | Keep as string |
| `variant` | parsed JSON | `JSON.parse()` |
| `object` | parsed JSON | `JSON.parse()` |
| `array` | parsed JSON | `JSON.parse()` |
| `binary` | `string` (hex) | |
| `null` | `null` | |

### Schema discovery

Snowflake has a standard `INFORMATION_SCHEMA` (very similar to PG). Schema queries:

**getTables:**
```sql
SELECT table_schema, table_name, table_type
FROM information_schema.tables
WHERE table_schema NOT IN ('INFORMATION_SCHEMA')
ORDER BY table_schema, table_name
```

Returns `schema.table` format (like PG, Snowflake is schema-aware).

**describeTable:**
```sql
SELECT column_name, data_type, is_nullable, column_default, ordinal_position
FROM information_schema.columns
WHERE table_schema = ? AND table_name = ?
ORDER BY ordinal_position
```

Plus primary key detection via:
```sql
SHOW PRIMARY KEYS IN TABLE "schema"."table"
```

**getIndexes:** Snowflake doesn't have traditional indexes (it's columnar with automatic micro-partitioning). Return empty array. This is correct behavior, not a gap.

**getConstraints:**
```sql
SHOW PRIMARY KEYS IN SCHEMA "schema"
SHOW IMPORTED KEYS IN SCHEMA "schema"  -- foreign keys
SHOW UNIQUE KEYS IN SCHEMA "schema"
```

Note: `SHOW` commands return results in a different format than SQL queries. The driver must handle both response shapes.

**searchSchema:**
```sql
SELECT table_schema, table_name, column_name
FROM information_schema.columns
WHERE column_name ILIKE '%pattern%'
   OR table_name ILIKE '%pattern%'
```

### Identifier quoting

Snowflake uses double-quotes (like PG): `"schema"."table"`. Reuse `quoteIdentPg` from `src/lib/quote-ident.ts` — same escaping rules.

### Timeout

Set via the `timeout` field in the SQL API request body (in seconds). Maps from our millisecond config value. Additionally, `AbortController` on the fetch for client-side timeout as a safety net.

---

## Credential Flow

### `credential add` for Snowflake

```bash
# PAT-based (primary)
agent-sql credential add sf-readonly --password <pat_secret>

# With write permission
agent-sql credential add sf-admin --password <pat_secret> --write
```

The `--password` flag accepts the PAT secret. This is consistent with how Snowflake SDKs treat PATs as password replacements.

### Connection test

```bash
agent-sql connection test sf-prod
```

Executes `SELECT 1` via the SQL API to verify auth + connectivity.

---

## Changes to Existing Code

### `src/drivers/resolve.ts`
- Add `"snowflake"` to `Driver` type
- Add `snowflake://` to `DRIVER_URL_PATTERNS`
- Add `connectSnowflake()` case in driver resolution
- Snowflake connections always require a credential (like PG/MySQL)

### `src/drivers/types.ts`
- No changes to `DriverConnection` interface — Snowflake implements it as-is

### `src/cli/connection/add.ts`
- Add `--account`, `--warehouse`, `--role` flags (Snowflake-specific)
- `--driver snowflake` option
- `--schema` flag already exists for PG, reuse for Snowflake

### `src/lib/config.ts`
- Connection type gains optional `account`, `warehouse`, `role` fields

### `src/lib/errors.ts`
- Add Snowflake error code mappings (SQL API error codes → fixable_by classification)
- Snowflake errors come as JSON: `{ "code": "...", "sqlState": "...", "message": "..." }`

### `src/lib/quote-ident.ts`
- Snowflake uses same quoting as PG (double-quotes). Reuse `quoteIdentPg`.

### Documentation updates
- `CLAUDE.md` — add Snowflake to supported drivers, architecture section
- `src/cli/usage/index.ts` — add Snowflake to reference card
- `skills/agent-sql/SKILL.md` — add "snowflake" trigger
- `skills/agent-sql/references/commands.md` — Snowflake connection examples
- `skills/agent-sql/references/output.md` — note on Snowflake type mapping
- `README.md` — add Snowflake to supported databases

---

## Snowflake-Specific Considerations

### Schema-awareness
Snowflake is schema-aware like PG. Uses dot notation: `database.schema.table`. Our UI shows `schema.table` (same as PG). Cross-database queries are possible but we scope schema discovery to the configured database.

### Case sensitivity
Snowflake uppercases all unquoted identifiers. Our schema queries should handle this gracefully — display names as Snowflake stores them (usually uppercase) and accept case-insensitive input for table lookups (uppercase the input before querying).

### No indexes
Snowflake uses automatic micro-partitioning, not traditional B-tree indexes. `getIndexes()` returns an empty array. This is correct behavior, not a gap.

### Clustering keys
Snowflake has clustering keys (similar in purpose to indexes). Could expose via `SHOW CLUSTERING KEYS` in a future enhancement.

### VARIANT/semi-structured data
Snowflake has `VARIANT`, `OBJECT`, `ARRAY` types for semi-structured data. These come back as JSON strings — parse with `JSON.parse()` and pass through.

### Warehouse suspension
Queries require an active warehouse. If the warehouse is suspended, Snowflake auto-resumes it (with a ~1-5s delay). Our timeout should account for this — the default 30s timeout is sufficient, but document that first-query latency may be higher due to warehouse resume.

### Views
Snowflake views appear in `information_schema.tables` with `table_type = 'VIEW'`. Included by default (consistent with PG/MySQL/SQLite).

---

## Testing Strategy

### Unit tests (mocked HTTP)
- Auth header generation (PAT, JWT)
- Result parsing (string[][] → typed rows, all Snowflake types, null handling, large integers)
- Read-only guard (keyword allowlist — allowed and blocked statements)
- Error mapping (Snowflake error codes → fixable_by)
- URL parsing (account extraction, connection string → config)
- Retry logic (backoff timing, max retries, jitter)
- Async polling (202 handling, poll intervals)
- Partition fetching (early termination when limit met)

### Integration tests (env-gated)
- Connection test (`SELECT 1`)
- Schema discovery (tables, describe, constraints, search)
- Query execution (SELECT, WITH, EXPLAIN, SHOW)
- Read-only enforcement (blocked writes produce clear errors)
- Write mode (INSERT/UPDATE/DELETE with `--write`)
- Timeout behavior
- Large result partition handling
- Case sensitivity (uppercase identifiers)
- VARIANT/semi-structured data types

Integration tests gated on `SNOWFLAKE_TEST_ACCOUNT` + `SNOWFLAKE_TEST_TOKEN` env vars.

---

## Open Questions

1. **Ad-hoc auth**: How should ad-hoc `snowflake://` URLs authenticate? Current proposal: `AGENT_SQL_SNOWFLAKE_TOKEN` env var. Alternative: prompt for token interactively (but we're CLI-for-agents, not interactive).

2. **Key-pair auth priority**: Ship PAT-only in v1, or include key-pair from the start? PAT covers most use cases. Key-pair can be added without breaking changes.

3. **Cross-database queries**: Snowflake supports `SELECT * FROM other_db.schema.table`. Allow in raw queries, scope schema discovery to configured database.

4. **Warehouse in credential vs config**: Warehouse is not sensitive — belongs in config. Some orgs use different warehouses for different access levels. Keep in config for now.

5. **SHOW command result format**: `SHOW` commands (used for constraint discovery) return results in a different format than regular SQL queries. Need to handle both response shapes in the driver. Investigate whether `information_schema` alternatives exist for all constraint types.
