# Output format (reference)

## General

All commands default to JSON output on stdout. Use `--format yaml` or `--format csv` for alternate formats.

- **JSON** (default): structured output for all commands
- **YAML**: structured output for all commands (`--format yaml`)
- **CSV**: tabular data only — applies to `query run` and `query sample` (`--format csv`). Non-tabular commands (schema, explain, count, config) fall back to JSON.
- **Errors**: always JSON to stderr regardless of `--format`

Set a persistent default with `agent-sql config set defaults.format yaml`. The `--format` flag overrides the config.

NULL values are preserved in query results (`"bio": null` is meaningful). Empty/null fields are pruned in admin/config output only.

Error messages include valid alternatives when input is invalid (e.g., `Table "usr" not found. Available: users, user_roles, user_sessions`).

Timeout errors include hints to increase `query.timeout` and check table indexes.

## Error output (stderr)

```json
{
  "error": "Query blocked: INSERT statements are not allowed in read-only mode.",
  "hint": "Connection 'prod' uses read-only credential 'prod-readonly'. Write operations require a credential with writePermission: true.",
  "fixable_by": "human"
}
```

The `fixable_by` field classifies errors:
- `"agent"` — the LLM can fix this (typo in table name, wrong syntax). Error includes valid alternatives.
- `"human"` — requires human action (permission change, credential setup). Do not retry.
- `"retry"` — transient error (timeout, connection lost). Worth retrying.

## Truncation

Strings exceeding `truncation.maxLength` (default 200) are truncated with `...` and a per-row `@truncated` metadata object showing original character counts.

**Default format (truncated):**

```json
{
  "columns": ["id", "name", "bio"],
  "rows": [
    { "id": 1, "name": "Alice", "bio": "Software engineer with 10 years of experience in distributed systems...", "@truncated": { "bio": 1847 } },
    { "id": 2, "name": "Bob", "bio": null, "@truncated": null }
  ],
  "pagination": { "hasMore": false, "rowCount": 2 }
}
```

**With `--full` or `--expand bio` (expanded):** full string shown, `@truncated` null.

`@truncated` is always present on every row: an object with original lengths when truncation occurred, `null` otherwise.

Global flags: `--expand <field,...>` or `--full`.

## Query results -- default format (`query run`, `query sample`)

```json
{
  "columns": ["id", "name", "email", "bio"],
  "rows": [
    { "id": 1, "name": "Alice", "email": "alice@example.com", "bio": "Software eng...", "@truncated": { "bio": 12345 } },
    { "id": 2, "name": "Bob", "email": "bob@example.com", "bio": null, "@truncated": null }
  ],
  "pagination": {
    "hasMore": true,
    "rowCount": 20
  }
}
```

`hasMore` indicates more rows match beyond the limit. `rowCount` is the number of rows returned.

## Query results -- compact format (`--compact`)

Column names appear once; rows are arrays. `@truncated` is an additional column (always last). Saves tokens for large results.

```json
{
  "columns": ["id", "name", "email", "bio", "@truncated"],
  "rows": [
    [1, "Alice", "alice@example.com", "Software eng...", { "bio": 12345 }],
    [2, "Bob", "bob@example.com", null, null]
  ],
  "pagination": { "hasMore": true, "rowCount": 20 }
}
```

In compact mode, `@truncated` is the last column in `columns` and the last element in each row array. It contains an object with original lengths when truncation occurred, `null` otherwise.

## Query results -- YAML format (`--format yaml`)

```yaml
columns:
  - id
  - name
  - email
rows:
  - id: 1
    name: Alice
    email: alice@example.com
  - id: 2
    name: Bob
    email: bob@example.com
pagination:
  hasMore: true
  rowCount: 20
```

## Query results -- CSV format (`--format csv`)

```csv
id,name,email
1,Alice,alice@example.com
2,Bob,bob@example.com
```

CSV only applies to tabular results (`query run`, `query sample`). Fields containing commas, newlines, or quotes are RFC 4180 quoted. NULL values render as empty fields.

## Query count (`query count`)

```json
{
  "table": "users",
  "count": 42
}
```

## Query explain (`query explain`)

```json
{
  "plan": "Seq Scan on users  (cost=0.00..1.05 rows=5 width=72)"
}
```

## Write operation output (`--write`)

```json
{
  "result": "ok",
  "rowsAffected": 5,
  "command": "UPDATE"
}
```

## Schema tables (`schema tables`)

```json
{
  "tables": [
    { "schema": "public", "name": "users", "type": "table" },
    { "schema": "public", "name": "orders", "type": "table" },
    { "schema": "public", "name": "user_summary", "type": "view" }
  ]
}
```

SQLite omits the `schema` field. Snowflake uses uppercase identifiers (e.g. `PUBLIC.USERS`).

## Schema describe (`schema describe`)

```json
{
  "table": "users",
  "columns": [
    { "name": "id", "type": "integer", "nullable": false, "default": "nextval('users_id_seq')" },
    { "name": "name", "type": "text", "nullable": false, "default": null },
    { "name": "email", "type": "text", "nullable": false, "default": null },
    { "name": "bio", "type": "text", "nullable": true, "default": null }
  ]
}
```

With `--detailed`, adds `constraints`, `indexes`, and `comments` fields.

## Schema indexes (`schema indexes`)

```json
[
  { "table": "users", "name": "users_pkey", "columns": ["id"], "unique": true },
  { "table": "users", "name": "users_email_key", "columns": ["email"], "unique": true }
]
```

## Schema constraints (`schema constraints`)

```json
[
  { "table": "users", "name": "users_pkey", "type": "primary_key", "columns": ["id"] },
  { "table": "orders", "name": "orders_user_fk", "type": "foreign_key", "columns": ["user_id"], "references": { "table": "users", "columns": ["id"] } }
]
```

## Schema dump (`schema dump`)

Combines tables, columns, indexes, and constraints in one response. Same structure as individual commands, nested under the table name.

## Connection list (`connection list`)

```json
{
  "connections": [
    {
      "alias": "local",
      "driver": "sqlite",
      "path": "/Users/paul/data/app.sqlite",
      "default": true
    },
    {
      "alias": "prod",
      "driver": "pg",
      "host": "db.example.com",
      "port": 5432,
      "database": "myapp",
      "credential": "prod-readonly"
    },
    {
      "alias": "warehouse",
      "driver": "snowflake",
      "account": "myorg-myaccount",
      "database": "ANALYTICS",
      "schema": "PUBLIC",
      "warehouse": "COMPUTE_WH",
      "credential": "sf-readonly"
    }
  ]
}
```

## Connection test (`connection test`)

```json
{
  "connection": "local",
  "driver": "sqlite",
  "status": "ok"
}
```

## Config list-keys (`config list-keys`)

```json
[
  { "key": "defaults.limit", "type": "number", "default": 20, "min": 1, "max": 1000, "description": "Default row limit" },
  { "key": "query.timeout", "type": "number", "default": 30000, "min": 1000, "max": 300000, "description": "Query timeout in ms" }
]
```
