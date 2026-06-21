# Streaming Output Design

## Current State

All drivers today collect all rows into a `QueryResult` object (`{ columns, rows }`) and pass it to `printJson`/`printPaginated`/`printCompact` as a single unit. No streaming output path exists.

## The @truncated Unlock

Currently `@truncated` is per-row metadata in default mode, but in compact mode it's top-level parallel arrays. This makes streaming impossible in compact mode.

**New design**: `@truncated` becomes a regular column that's ALWAYS present in every row. When no truncation occurred for a row, the cell is `null`. This makes every row self-contained — the key unlock for streaming.

### Before (default mode)

```json
{"rows": [
  {"id": 1, "bio": "Long tex…", "@truncated": {"bio": 5000}},
  {"id": 2, "bio": "Short"}
]}
```

### After (default mode)

```json
{"rows": [
  {"id": 1, "bio": "Long tex…", "@truncated": {"bio": 5000}},
  {"id": 2, "bio": "Short", "@truncated": null}
]}
```

### Before (compact mode)

```json
{"columns": ["id", "bio"], "rows": [[1, "Long tex…"], [2, "Short"]], "truncated": {"bio": [5000, null]}}
```

### After (compact mode)

```json
{"columns": ["id", "bio", "@truncated"], "rows": [[1, "Long tex…", {"bio": 5000}], [2, "Short", null]]}
```

This means compact mode now includes `@truncated` as a column in the `columns` array and as the last element of each row array. Every row is self-contained.

## Streaming Architecture

For JSON output, streaming means NDJSON (newline-delimited JSON) or a JSON array that opens `[`, emits rows as they arrive, and closes `]`.

The approach:

1. Emit metadata header (columns, format info)
2. Stream rows as they arrive (each row is self-contained with `@truncated`)
3. Emit footer (pagination info, total count)

### Snowflake

Emit rows from partition 0 immediately, then fetch partition 1 and emit those rows, etc. Stop when `maxRows` reached. The partition-based fetching model is inherently streaming-friendly — each partition is a batch that can be processed and emitted independently.

### PG / MySQL / SQLite

These drivers return all rows at once from their native APIs. Streaming for these means we can start output processing (truncation, formatting) incrementally rather than buffering the entire formatted output. The real win is for Snowflake where data arrives in partitions.

## Changes Required

- Modify `applyTruncation` to work per-row (already mostly does) with always-present `@truncated`
- Modify `printJson`/`printPaginated`/`printCompact` to accept an async iterable or similar
- Add streaming output mode that emits rows incrementally
- Snowflake driver returns an async iterable of row batches (one per partition)
- Other drivers can wrap their array results in a trivial async iterable for compatibility

## Config Changes

- `query.maxRows` default changes from 100 to 10,000 (enables more useful large queries while streaming prevents memory issues)

## JSONL Format (New Default)

JSONL (JSON Lines / NDJSON) emits one JSON object per line. Every row is self-contained — column names are keys, no separate header needed. This is the ideal format for LLM consumption: no risk of losing track of which cell belongs to which column.

### Output shape

```
{"ID": 1, "NAME": "Alice", "EMAIL": "alice@test.com", "@truncated": null}
{"ID": 2, "NAME": "Bob", "EMAIL": "bob@test.com", "@truncated": null}
```

No envelope, no columns header, no pagination wrapper. Just rows.

When there are more rows than the limit, a final metadata line is emitted:
```
{"@pagination": {"hasMore": true, "rowCount": 20}}
```

### Why JSONL as default

- Every line is self-contained — LLMs never lose column context
- Streams naturally — can process row-by-row without buffering
- Pipes well — `agent-sql run ... | jq .NAME` works per-line
- Token-efficient — no envelope overhead
- Standard ecosystem — jq, ndjson-cli, etc. all support it

### Format landscape

| Format | Use case | Shape |
|--------|----------|-------|
| `jsonl` | **Default.** LLM-native, streaming | One `{col: val}` per line |
| `json` | Structured/programmatic | `{columns, rows}` envelope |
| `compact` | Token-efficient arrays | `{columns, rows: [[val]...]}` |
| `yaml` | Human debugging | Same as json, YAML syntax |
| `csv` | Export/spreadsheet | Header + rows |

### Non-tabular output

JSONL only applies to tabular query results (run, sample). Non-tabular output (schema, config, explain, count, connection/credential admin) uses JSON or YAML as before. Errors always go to stderr as JSON.

## Phase Plan

**Phase 1 (now):** Build Snowflake driver with streaming-friendly internal design (partition-based fetching returns batches).

**Phase 2 (after Snowflake ships):** Refactor output layer to support streaming, update `@truncated` to always-present pattern, update all drivers.
