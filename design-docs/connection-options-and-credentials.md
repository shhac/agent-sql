# Connection options + credential hygiene

Captured 2026-05-02. Two related gaps surfaced while cleaning up `connection list`.

## 1. Driver-specific options

`config.Connection` has no `Options`/`Params` field. The connect chain in
`internal/resolve/resolve.go` builds each driver's `Opts` struct by hand from
`Host`/`Port`/`Database`/`Credential`/`Readonly` (+ snowflake's
`Account`/`Warehouse`/`Role`/`Schema`). Nothing else is threaded through.

This is a **real usage gap, not just a listing gap.** Even adding an
`Options map[string]string` to `config.Connection` would do nothing unless
each driver's `Connect`/`Opts` is updated to consume it.

What can't be expressed on a stored connection today:

- pg / cockroachdb: `sslmode`, `search_path`, `application_name`, `connect_timeout`
  (pg.go:36 hardcodes `sslmode=prefer`).
- mysql / mariadb: `tls`, `parseTime`, `loc`, `charset`, `allowCleartextPasswords`,
  `readTimeout`/`writeTimeout`.
- mssql: `encrypt`, `TrustServerCertificate`, `app name` (we hardcode `agent-sql`).
- sqlite: `_journal_mode`, `_busy_timeout`, PRAGMAs.
- duckdb: extensions, temp dir, memory limit.
- snowflake: `QUERY_TAG`, custom session params (warehouse/role are first-class).

### Ad-hoc URL query-param coverage

`-c <url>` paths vary by driver:

| Driver | Path | Query params honored? |
|---|---|---|
| pg / cockroachdb | `pg.ConnectURL(ctx, url, …)` → `pgx.Connect(ctx, url)` | ✓ pgx parses URL natively |
| mysql / mariadb | `connectMysqlLikeURL` → our `parseURL` → fresh `gomysql.Config` | ✗ params dropped |
| mssql | `connectMssqlURL` → our `parseURL` → fresh `sqlserver://` URL with only `database`+`app name` | ✗ params dropped |
| snowflake | `parseSnowflakeURL` reads only `warehouse` + `role` | ✗ everything else dropped |

So "preserve URL query params" means different things per driver:

- **pg / crdb:** already works.
- **mssql:** easy — go-mssqldb parses URL query params natively. Pass the
  raw URL through instead of rebuilding from parts (or merge stored params
  into the rebuilt URL).
- **mysql / mariadb:** harder — `gomysql.ParseDSN` expects gomysql's DSN
  form (`user:pw@tcp(h:p)/db?params`), not URL form. Either translate each
  known param into the corresponding `gomysql.Config` field, or convert
  URL → gomysql DSN as a preprocessing step.
- **snowflake:** custom; we own the HTTP REST client. Add params as session
  config in the request body if Snowflake supports the param.

### Possible shape

```go
type Connection struct {
    // ...existing fields...
    Options map[string]string `json:"options,omitempty"`
}
```

CLI: `--option k=v` (repeatable) on `connection add` / `connection update`.
Listing: emit `options: {…}` when non-empty (already trivially handled by
the existing `omitempty` pattern in `renderConnection`).

Each driver's `Connect`/`Opts` would need to accept and apply them. pg is
the easiest (append to DSN string). mysql needs a `Options` -> `gomysql.Config`
translation table (whitelist of supported params). mssql can append to the
URL query. snowflake needs explicit per-key handling.

## 2. Credential hygiene in stored URLs

`parseConnectionString` (`internal/cli/connection/parse.go`) does
`*url = connStr` for any URL-form input. If the user runs:

```
connection add prod postgres://user:secret@host/db
```

then `conn.URL = "postgres://user:secret@host/db"` is written to
`~/.config/agent-sql/config.json` **in plaintext**. This violates the
explicit design goal stated in CLAUDE.md / AGENTS.md:

> Read-only by default — credentials stored in macOS Keychain, not in
> config file. Config has zero sensitive data.

### Display side (already safe)

- `DisplayURL` builds from `Host`/`Port`/`Database` (with URL-backfill
  via `url.Parse`, which only reads Hostname/Port/Path — not user info).
- The listing dropped the raw `url` field. So `connection list` and
  `connection add`'s receipt do not surface credentials.

### Storage side (real leak)

Today: secrets land in `config.json` if embedded in the URL.

Fix options, in increasing strictness:

1. **At add time, extract `user:pass@` from the URL, strip them, and
   refuse to store the URL with embedded credentials.** Suggest
   `--credential <name>` and link to `credential add`. This is the simplest
   safe default.
2. Same as (1) but offer to auto-create a credential entry from the
   extracted user/password (interactive prompt or `--accept-extracted`).
3. Always strip credentials from the URL before storing, silently. Show a
   warning in the receipt.

Whichever route, the listing should always render a masked form when a URL
is the source of host info — e.g. `postgres://***:***@host:5432/db` — so a
human reading `connection list` immediately sees that credentials are
involved (vs. an unauthenticated/cred-by-reference connection).

### Listing/render rules

- Never emit `username`, `password`, or any user-info component as its own
  property.
- If display is built from a URL that contained user info (post-cleanup
  this should be impossible, but handle defensively), mask both halves of
  the userinfo: `***:***@host…`.

## Order of work (if/when picked up)

1. Plug the credential leak (option 1 or 2 above). Pure correctness fix.
2. Add `Options` field + per-driver consumption. New feature; opt-in per
   driver — pg first since it's a one-liner, mssql next (URL-native), then
   mysql, then snowflake.
3. Audit ad-hoc URL query-param handling and align with stored-options
   semantics so the same `?sslmode=require` works on both `-c URL` and
   stored-connection paths.
