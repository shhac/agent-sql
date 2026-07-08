# Output format (reference)

## General

Everything on stdout honors `--format` (`jsonl` default, `json`, `yaml`; `csv` on query commands only):

- **Query results** (`query run`, `query sample`): JSONL by default — one JSON object per row, no envelope. `--format json`/`yaml` gives a `{columns, rows, pagination}` envelope. `--format csv` gives CSV.
- **List-shaped output** (`schema tables`, `schema indexes`, `schema constraints`, `connection list`, `credential list`, `config list-keys`): NDJSON records by default — one JSON object per line, no wrapper key. `--format json`/`yaml` wraps them in a `{"data": [...]}` envelope.
- **Single resources and receipts** (`schema describe/search/dump`, `query count/explain`, write receipts, connection/credential/config receipts): one compact JSON line by default. `--format json` pretty-prints; `--format yaml` gives YAML.
- **CSV** is accepted only on query commands. Elsewhere `--format csv` is rejected with a structured error: `unknown format "csv", expected: json, yaml, jsonl`. Only `query.format` (not `defaults.format`) may persist csv; within query commands, non-tabular output (count, explain, write receipts) falls back to jsonl when csv is in effect.
- **Errors**: always JSON to stderr regardless of `--format`.
- **Notices**: non-error advisories go to stderr as structured `{"notice": "...", "hint": "..."}` JSON lines.
- **`--color auto|always|never`** (default auto): colorizes JSON/YAML/NDJSON when the stream is a terminal; piped output stays plain.

Set a persistent default with `agent-sql config set defaults.format json` (all commands, universal formats only). Query commands additionally honor `query.format` (which may be `csv`) over `defaults.format`. The `--format` flag overrides both.

NULL values are preserved in query results (`"bio": null` is meaningful). Empty/null fields are pruned in admin/config output only.

Error messages include valid alternatives when input is invalid (e.g., `Table "usr" not found. Available: users, user_roles, user_sessions`).

Timeout errors include hints to increase `query.timeout` and check table indexes.

## Error output (stderr)

```json
{
  "error": "Query blocked: INSERT statements are not allowed in read-only mode.",
  "fixable_by": "human",
  "hint": "Connection 'prod' uses read-only credential 'prod-readonly'. Write operations require a credential with writePermission: true."
}
```

The `fixable_by` field classifies errors:
- `"agent"` — the LLM can fix this (typo in table name, wrong syntax). Error includes valid alternatives.
- `"human"` — requires human action (permission change, credential setup). Do not retry.
- `"retry"` — transient error (timeout, connection lost). Worth retrying.

Driver classification is preserved end to end — e.g. connection refused is `fixable_by: "human"` with a hint to check host/port.

## Notice output (stderr)

Non-error advisories (credentials stripped, fallback used, ...) are structured JSON lines on stderr. `hint` is included only when non-empty:

```json
{"notice":"stripped embedded credentials from connection string; using --credential prod-readonly"}
```

## Truncation

Strings exceeding `truncation.maxLength` (default 200) are truncated with `...` and a per-row `@truncated` metadata object showing original character counts.

**Default format — JSONL (truncated):**

```jsonl
{"id":1,"name":"Alice","bio":"Software engineer with 10 years of experience in distributed systems...","@truncated":{"bio":1847}}
{"id":2,"name":"Bob","bio":null,"@truncated":null}
```

**With `--full` or `--expand bio` (expanded):** full string shown, `@truncated` null.

`@truncated` is always present on every row: an object with original lengths when truncation occurred, `null` otherwise.

Global flags: `--expand <field,...>` or `--full`.

## Query results -- JSONL format (default) (`query run`, `query sample`)

One JSON object per line, no envelope. Each line contains one row with all columns plus `@truncated` metadata:

```jsonl
{"id":1,"name":"Alice","email":"alice@example.com","bio":"Software eng...","@truncated":{"bio":12345}}
{"id":2,"name":"Bob","email":"bob@example.com","bio":null,"@truncated":null}
{"@pagination":{"has_more":true,"row_count":2,"hint":"stopped at your --limit of 2 rows; raise --limit for more, or push the cap into your SQL with LIMIT/TOP for planner-side acceleration"}}
```

The `@pagination` line appears only when there are more rows beyond the limit. `row_count` is the number of rows returned; `hint` says how to get more.

## Query results -- JSON envelope format (`--format json`)

```json
{
  "columns": ["id", "name", "email", "bio"],
  "pagination": {
    "has_more": true,
    "row_count": 20,
    "hint": "stopped at your --limit of 20 rows; raise --limit for more, or push the cap into your SQL with LIMIT/TOP for planner-side acceleration"
  },
  "rows": [
    { "id": 1, "name": "Alice", "email": "alice@example.com", "bio": "Software eng...", "@truncated": { "bio": 12345 } },
    { "id": 2, "name": "Bob", "email": "bob@example.com", "bio": null, "@truncated": null }
  ]
}
```

`has_more` indicates more rows match beyond the limit. `row_count` is the number of rows returned.

## Query results -- compact format (`--compact`)

Typed NDJSON lines: column names appear once, rows are arrays. Saves tokens for large results.

```jsonl
{"type":"columns","values":["id","name","email","bio"]}
{"type":"row","values":[1,"Alice","alice@example.com","Software eng..."]}
{"type":"row","values":[2,"Bob","bob@example.com",null]}
{"type":"pagination","values":{"has_more":true,"row_count":2,"hint":"..."}}
```

The `pagination` line appears only when more rows exist beyond the limit.

## Query results -- YAML format (`--format yaml`)

```yaml
columns:
  - id
  - name
  - email
pagination:
  has_more: true
  hint: stopped at your --limit of 20 rows; raise --limit for more, or push the cap into your SQL with LIMIT/TOP for planner-side acceleration
  row_count: 20
rows:
  - id: 1
    name: Alice
    email: alice@example.com
  - id: 2
    name: Bob
    email: bob@example.com
```

## Query results -- CSV format (`--format csv`)

```csv
id,name,email
1,Alice,alice@example.com
2,Bob,bob@example.com
```

CSV only applies to query commands (`query run`, `query sample`). Fields containing commas, newlines, or quotes are RFC 4180 quoted. NULL values render as empty fields.

## Query count (`query count`)

One JSON line by default (pretty with `--format json`):

```jsonl
{"count":42,"table":"users"}
```

## Query explain (`query explain`)

One JSON line by default (pretty with `--format json`). Plan shape is driver-specific (string for PG, structured rows for SQLite):

```jsonl
{"plan":"Seq Scan on users  (cost=0.00..1.05 rows=5 width=72)"}
```

## Write operation output (`--write`)

```jsonl
{"command":"UPDATE","result":"ok","rows_affected":5}
```

## Schema tables (`schema tables`)

NDJSON records by default; `{"data": [...]}` envelope with `--format json`/`yaml`:

```jsonl
{"schema":"public","name":"users","type":"table"}
{"schema":"public","name":"orders","type":"table"}
{"schema":"public","name":"user_summary","type":"view"}
```

SQLite omits the `schema` field. Snowflake uses uppercase identifiers (e.g. `PUBLIC.USERS`).

## Schema describe (`schema describe`)

One JSON line by default (shown pretty here, as with `--format json`):

```json
{
  "table": "users",
  "columns": [
    { "name": "id", "type": "integer", "nullable": false, "primary_key": true, "default_value": "nextval('users_id_seq')" },
    { "name": "name", "type": "text", "nullable": false },
    { "name": "email", "type": "text", "nullable": false },
    { "name": "bio", "type": "text", "nullable": true }
  ]
}
```

`default_value` is omitted when the column has no default; `primary_key` appears only when true. With `--detailed`, adds `constraints`, `indexes`, and `comments` fields.

## Schema indexes (`schema indexes`)

NDJSON records by default; `{"data": [...]}` envelope with `--format json`/`yaml`:

```jsonl
{"table":"users","name":"users_pkey","columns":["id"],"unique":true}
{"table":"users","name":"users_email_key","columns":["email"],"unique":true}
```

## Schema constraints (`schema constraints`)

NDJSON records by default; `{"data": [...]}` envelope with `--format json`/`yaml`:

```jsonl
{"table":"users","name":"users_pkey","type":"primary_key","columns":["id"]}
{"table":"orders","name":"orders_user_fk","type":"foreign_key","columns":["user_id"],"referenced_table":"users","referenced_columns":["id"]}
```

## Schema dump (`schema dump`)

Combines tables, columns, indexes, and constraints in one response (a single resource: one JSON line by default). Same structure as individual commands, nested under the table name.

## Connection list (`connection list`)

NDJSON records by default; `{"data": [...]}` envelope with `--format json`/`yaml`. Each record shows `alias`, `driver`, `display_url`, plus `host`/`port`/`database`/`credential`/`options` when set, and `default: true` for the default. `display_url` is the canonical connection target — it includes the per-driver default port (5432, 26257, 3306, 1433) and any stored options as `?key=value&...`, so the URL reflects what would actually be used at connect time. `host` and `port` are the effective values (URL-backfilled if needed; default port applied for host-port drivers). Snowflake reports its account as `host` and omits `port`; SQLite/DuckDB omit both. `options` carries driver-specific knobs (sslmode, parseTime, encrypt, _journal_mode, memory_limit, query_tag, ...). Raw storage fields (`path`, `url`) are not emitted.

```jsonl
{"alias":"local","driver":"sqlite","display_url":"sqlite:///Users/paul/data/app.sqlite","default":true}
{"alias":"prod","driver":"pg","display_url":"postgres://db.example.com:5432/myapp","host":"db.example.com","port":5432,"database":"myapp","credential":"prod-readonly"}
{"alias":"warehouse","driver":"snowflake","display_url":"snowflake://myorg-myaccount/ANALYTICS/PUBLIC","host":"myorg-myaccount","database":"ANALYTICS","credential":"sf-readonly"}
{"alias":"ms","driver":"mssql","display_url":"mssql://sqlhost:1433/reporting","host":"sqlhost","port":1433,"database":"reporting","credential":"mssql-readonly"}
```

If a single entry fails to render, that record is replaced with `{alias, driver, default, error}` and the rest of the list is unaffected.

## Connection test (`connection test`)

One JSON line by default (pretty with `--format json`). `rows` echoes the `SELECT 1` probe result:

```jsonl
{"connection":"local","ok":true,"rows":[{"1":1}]}
```

## Config list-keys (`config list-keys`)

NDJSON records by default; `{"data": [...]}` envelope with `--format json`/`yaml`:

```jsonl
{"key":"defaults.format","type":"string","default":"jsonl","allowed_values":["jsonl","json","yaml"],"description":"Default output format (all commands)"}
{"key":"query.format","type":"string","default":"jsonl","allowed_values":["jsonl","json","yaml","csv"],"description":"Default output format for query commands (overrides defaults.format there)"}
{"key":"query.timeout","type":"number","default":30000,"min":1000,"max":300000,"description":"Query timeout in milliseconds"}
```
