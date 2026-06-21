# Connection options — per-driver plan

Captured 2026-05-02. Plan for letting users persist driver-specific knobs
(sslmode, parseTime, encrypt, query_tag, etc.) on stored connections, and
have those knobs reach the driver at connect time. Implementation is
deferred — this doc is the contract before any code lands.

## Goals

- One stored field on `config.Connection`: `Options map[string]string`.
- Each driver owns its own options surface — what's accepted, what each
  key means, and how it threads into the connect-time DSN. Adding or
  removing a driver is a clean per-package boundary; nothing in `config/`,
  `resolve/`, or `cli/connection/` needs to change.
- Round-trip: a URL with `?param=value` passed to `connection add`
  produces a stored connection that, when listed, shows the same
  parameters in `display_url` and `options`.
- **Allow any key — no allowlist gate.** Each underlying driver lib already
  rejects unknown options at connect time with messages more accurate than
  anything we'd write. Maintaining our own allowlist would lag behind their
  releases and lock users out of new features. We pass options through
  verbatim; if a key is wrong, the user sees the driver's own error on
  `connection test`. Optional: keep a soft "common keys" hint list per
  driver in `usage.go` for `--help` text, but it is never enforced.
- Two input shapes both produce the same stored result:
  - URL with query string: `connection add prod postgres://h/d?sslmode=require`
  - Repeatable flag: `connection add prod --host h --database d --option sslmode=require`

## Non-goals

- Secrets in options. Options are always plaintext-safe (sslmode is fine;
  passwords are not). Anything credential-shaped continues to live in the
  keychain via `--credential`.
- Backwards-compat shims. The `Options` field is new; existing connections
  load with empty options. No migration required.
- Per-key validation beyond "is this in the allowlist?". Type/format
  errors surface from the driver at connect time.

## Storage

```go
// internal/config/config.go
type Connection struct {
    // ...existing fields...
    Options map[string]string `json:"options,omitempty"`
}
```

Marshalled as JSON, alphabetized by key (deterministic for diffing).

## Per-driver interface

Each driver package (pg, cockroachdb, mysql, mariadb, mssql, sqlite,
duckdb, snowflake) defines two small functions, ideally in a single
`options.go` file:

```go
// ParseURLOptions extracts options from a URL's query string (and any
// driver-specific URL parts, e.g. sqlite fragment). All keys pass
// through; the underlying driver lib validates them at connect time.
func ParseURLOptions(u *url.URL) map[string]string

// ApplyOptions threads stored options into the driver's connect-time
// data structure. Each driver signature differs because each "Opts"
// struct differs. Implementation is driver-native: pgx pass-through,
// gomysql.ParseDSN for mysql, SET/INSTALL for duckdb, etc.
func ApplyOptions(opts map[string]string, into *Opts) error
```

No `KnownOptions`, no `ValidateOptions` — neither is enforced. If a user
typos `sslmodd=require`, pgx tells them at `connection test` time.

Display side (no driver dependency in config/):

- `Connection.DisplayURL` appends `?key=value&…` (alphabetized) when
  `Options` is non-empty. config doesn't need to know what the keys mean.
- For drivers without URL form (duckdb), `DisplayURL` already returns
  `duckdb://path` and we just don't append the query string for that case.

## CLI shape

```
connection add <alias> [conn-string] [--option k=v]... [--credential <name>] [other flags]
connection update <alias> [--option k=v]... [--clear-options]
connection list                  # emits "options": {...} when non-empty
```

`--option` is repeatable. URL query params and `--option` flags merge,
with `--option` winning on conflict.

`connection list` adds `options` as a top-level field (omitted when
empty), mirroring `database`/`host`/`port`/`credential`.

## Per-driver mapping

### pg / cockroachdb

- Underlying lib: pgx — accepts URL form natively.
- Connect path: build a URL string with the options query, hand to
  `pgx.Connect(ctx, urlStr)`. **Pass-through.** pgx errors on unknown keys.
- Today's hardcoded `sslmode=prefer` in `pg.go:36` becomes a default used
  only when `Options["sslmode"]` is unset.
- Common keys (hint, not enforced): `sslmode`, `sslrootcert`,
  `application_name`, `connect_timeout`, `statement_timeout`.

### mysql / mariadb

- Underlying lib: go-sql-driver/mysql. DSN form
  `user:pw@tcp(h:p)/db?param=value`.
- Connect path: build the DSN string with our host/port/user/pass plus
  the options query string verbatim, then `gomysql.ParseDSN(dsn)`.
  ParseDSN handles every typed field (ParseTime, Loc, TLSConfig, …) and
  drops anything else into `Config.Params`. **Pass-through via ParseDSN.**
- Explicit override: we set `MultiStatements=false` after ParseDSN
  regardless of input, for safety.
- Common keys (hint): `parseTime`, `loc`, `tls`, `timeout`, `charset`.

### mssql

- Underlying lib: go-mssqldb — URL form natively supported.
- Connect path: today we build a fresh `sqlserver://` URL with only
  `database` + `app name`. Refactor to append the options query string;
  let go-mssqldb parse them. **Pass-through.**
- Common keys (hint): `encrypt`, `TrustServerCertificate`,
  `connection timeout`, `ApplicationIntent`.

### sqlite

- Underlying lib: modernc.org/sqlite. DSN form `file:path?_pragma=value`.
- Connect path: append the options query string to the DSN. **Pass-through.**
- Read-only mode (`?mode=ro`) stays driver-internal, set independently
  after merging user options (so users can't override the read-only
  guarantee via an option).
- Common keys (hint): `_journal_mode`, `_busy_timeout`, `_synchronous`,
  `_cache_size`, `_foreign_keys`.

### duckdb

- Underlying lib: subprocess `duckdb` CLI — no URL form.
- Connect path: subprocess driver runs `SET key='value';` for each option
  after open, before any user query. DuckDB returns its own error for
  unknown settings — surface that as a connect-time error.
- For `extensions` specifically: split by comma, run `INSTALL ext;
  LOAD ext;` in the subprocess. (One option key reserved for this UX.)
- Display: `DisplayURL` ignores options for duckdb (no URL form), but
  `connection list` still emits `options: {…}`.
- Common keys (hint): `memory_limit`, `temp_directory`, `threads`,
  `extensions`.

### snowflake

- Underlying lib: our HTTP REST client.
- Connect path: after handshake, run `ALTER SESSION SET key=value` for
  each option in a single statement. Snowflake errors on unknowns.
- `parseSnowflakeURL` continues to special-case `warehouse` and `role`
  (they remain first-class fields); extend it to extract everything else
  into options.
- Common keys (hint): `query_tag`, `timezone`,
  `statement_timeout_in_seconds`.

## Add/update flow (CLI)

```
1. Parse positional connection string with parseConnectionString (existing).
2. Detect driver (existing).
3. Strip credentials from URL (existing — covered by 0f924ce).
4. opts := drv.ParseURLOptions(u)             // pass-through, no filtering
5. for k,v := range --option flags: opts[k] = v   // flag wins on conflict
6. Store in conn.Options. No validation; driver gets the final say at connect.
```

## Connect flow (resolve.go)

```
1. Resolve alias → conn (existing).
2. Build driver Opts from conn.Host/Port/Database/etc. (existing).
3. drv.ApplyOptions(conn.Options, &opts) — merges into the typed Opts.
4. drv.Connect(ctx, opts).
```

`resolve.go` stays driver-agnostic: it dispatches to the right driver
package and never inspects keys directly.

## Examples

### Example 1 — pg with sslmode

Input:
```
agent-sql connection add prod-pg \
  "postgres://db.example.com:5432/myapp?sslmode=require&application_name=agent-sql" \
  --credential prod-cred
```

Stored (`config.json`):
```json
{
  "driver": "pg",
  "host": "db.example.com",
  "port": 5432,
  "database": "myapp",
  "credential": "prod-cred",
  "options": {
    "application_name": "agent-sql",
    "sslmode": "require"
  }
}
```

`connection list` row:
```json
{
  "alias": "prod-pg",
  "driver": "pg",
  "display_url": "postgres://db.example.com:5432/myapp?application_name=agent-sql&sslmode=require",
  "host": "db.example.com",
  "port": 5432,
  "database": "myapp",
  "credential": "prod-cred",
  "options": {
    "application_name": "agent-sql",
    "sslmode": "require"
  },
  "default": false
}
```

Connect: pgx receives the full URL with `?application_name=…&sslmode=require`.

### Example 2 — mysql via flag, not URL

Input:
```
agent-sql connection add staging-mysql \
  --driver mysql --host stage.example.com --database inventory \
  --option parseTime=true --option tls=skip-verify \
  --credential stage-cred
```

Stored:
```json
{
  "driver": "mysql",
  "host": "stage.example.com",
  "database": "inventory",
  "credential": "stage-cred",
  "options": { "parseTime": "true", "tls": "skip-verify" }
}
```

`connection list` `display_url`:
`mysql://stage.example.com:3306/inventory?parseTime=true&tls=skip-verify`

Connect: gomysql.Config has `ParseTime=true`, `TLSConfig="skip-verify"`.

### Example 3 — sqlite with PRAGMAs

Input:
```
agent-sql connection add local ./data.db \
  --option _journal_mode=wal --option _busy_timeout=5000
```

Stored:
```json
{
  "driver": "sqlite",
  "path": "/Users/paul/proj/data.db",
  "options": { "_busy_timeout": "5000", "_journal_mode": "wal" }
}
```

`display_url`: `sqlite:///Users/paul/proj/data.db?_busy_timeout=5000&_journal_mode=wal`

Connect: sqlite DSN becomes
`file:/Users/paul/proj/data.db?mode=ro&_busy_timeout=5000&_journal_mode=wal`.

### Example 4 — snowflake with query_tag

Input:
```
agent-sql connection add warehouse \
  "snowflake://acct/DB/PUBLIC?warehouse=WH&role=ANALYST&query_tag=agent-sql"
```

Stored:
```json
{
  "driver": "snowflake",
  "account": "acct",
  "database": "DB",
  "schema": "PUBLIC",
  "warehouse": "WH",
  "role": "ANALYST",
  "options": { "query_tag": "agent-sql" }
}
```

Note `warehouse` and `role` continue as first-class fields (existing
behavior), only `query_tag` lands in options. Connect-time the REST
client adds `ALTER SESSION SET QUERY_TAG='agent-sql'` after handshake.

### Example 5 — unknown option, deferred to driver

Input:
```
agent-sql connection add prod-pg \
  "postgres://h/d?sslmode=require&parseTime=true" \
  --credential c
```

Add succeeds — we don't second-guess the driver:
```json
{ "ok": true, "alias": "prod-pg", ... }
```

Connect-time (`agent-sql connection test prod-pg`) surfaces pgx's own
error, which is more accurate than anything we'd write:
```json
{
  "error": "unknown configuration parameter \"parseTime\"",
  "fixable_by": "human"
}
```

This way, when pgx adds support for a new option in a future release, it
just works — no allowlist update needed on our side.

### Example 6 — clearing options on update

Input:
```
agent-sql connection update prod-pg --clear-options
```

Stored: `options` field removed entirely. `display_url` reverts to
unparameterized `postgres://h:5432/d`.

## Order of work

1. Land `Options map[string]string` in `config.Connection` (storage only,
   no driver wiring yet). Round-trip through Read/Write.
2. Wire pg first — it's the easiest (URL-native via pgx). Confirm
   end-to-end with `sslmode=require`.
3. mssql next (URL-native via go-mssqldb).
4. sqlite (DSN append).
5. mysql/mariadb (typed-field translation).
6. duckdb (subprocess SET/INSTALL on connect).
7. snowflake (REST session params).
8. Update docs (SKILL.md, README.md, references/commands.md, output.md).

Each step is shippable on its own; until a driver's options.go lands,
its `Options` map stays empty (round-trips fine, no behavioral change).
