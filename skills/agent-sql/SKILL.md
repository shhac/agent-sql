---
name: agent-sql
description: |
  Read-only-by-default SQL CLI for AI agents. Use when:
  - Exploring SQL databases (PostgreSQL, SQLite) -- tables, columns, indexes, constraints
  - Querying data (SELECT, sample rows, count, explain plans)
  - Writing data when explicitly permitted (INSERT, UPDATE, DELETE with --write)
  - Checking database connections or adjusting CLI settings
  Triggers: "sql query", "sql database", "sql table", "sql schema", "postgres", "postgresql", "sqlite", "sql connection", "query database", "sql select", "sql insert", "sql explain"
---

# SQL database exploration with `agent-sql`

`agent-sql` is a read-only-by-default SQL CLI binary on `$PATH`. All output is JSON to stdout. Errors go to stderr as `{ "error": "...", "hint": "...", "fixable_by": "agent|human|retry" }` with non-zero exit.

Supports PostgreSQL and SQLite. MySQL coming soon.

## Quick start

Connections are pre-configured by the user. Start by discovering what's available:

```bash
agent-sql usage                          # full reference card
agent-sql connection list                # see available connections
agent-sql connection test                # verify default connection works
```

## Exploring a database

```bash
agent-sql schema tables                              # list all tables
agent-sql schema tables --include-system              # include system tables (PG)
agent-sql schema describe users                       # columns, types, nullability, defaults
agent-sql schema describe users --full                # add constraints, indexes, comments
agent-sql schema describe analytics.events            # PG namespace dot notation
agent-sql schema indexes                              # all indexes across all tables
agent-sql schema indexes users                        # indexes for a specific table
agent-sql schema constraints users                    # PKs, FKs, unique, check constraints
agent-sql schema constraints --type fk                # filter by constraint type
agent-sql schema search user                          # search table and column names
agent-sql schema dump                                 # full schema (all tables, columns, indexes, constraints)
agent-sql schema dump --tables users,orders           # dump specific tables only
agent-sql query sample users                          # 5 sample rows (default)
agent-sql query sample users --limit 10 --where "status = 'active'"
```

## Querying data

```bash
agent-sql run "SELECT * FROM users WHERE age >= 21"                 # top-level shorthand
agent-sql query run "SELECT * FROM users WHERE age >= 21"           # equivalent
agent-sql query run "SELECT * FROM users" --limit 50                # override row limit
agent-sql query run "SELECT * FROM users" --compact                 # array-of-arrays (saves tokens)
agent-sql query explain "SELECT * FROM users WHERE email = 'a@b'"   # query plan
agent-sql query explain "SELECT * FROM users" --analyze             # EXPLAIN ANALYZE
agent-sql query count users                                         # total row count
agent-sql query count users --where "status = 'active'"             # filtered count
```

## Writing data (requires permission)

Writes are blocked by default. The user must configure a credential with write permission. Then opt in per-query:

```bash
agent-sql run "INSERT INTO logs (msg) VALUES ('hello')" --write
agent-sql run "UPDATE users SET active = true WHERE id = 1" --write
```

If writes are blocked, the error will have `"fixable_by": "human"` -- do not retry, escalate to the user.

## Truncation

Strings exceeding `truncation.maxLength` (default 200) are truncated with `...` and an `@truncated` metadata object per row showing original lengths.

```bash
agent-sql --full query run "SELECT * FROM posts"             # expand all fields
agent-sql --expand body query run "SELECT * FROM posts"      # expand specific field
```

These are global flags -- place them before or after the command.

## Timeout

Default timeout is 30s (configurable via `query.timeout`). Override per-command:

```bash
agent-sql --timeout 60000 run "SELECT * FROM large_table"
```

## Configuration

```bash
agent-sql config list-keys                           # all keys with defaults/ranges
agent-sql config set defaults.limit 50
agent-sql config get query.timeout
agent-sql config reset                               # restore defaults
```

Key settings: `defaults.limit` (20), `query.timeout` (30000ms), `query.maxRows` (100), `truncation.maxLength` (200).

## Connection management

Connections are set up by the user. The agent can list and test but not add/remove/modify:

```bash
agent-sql connection list                            # saved connections + defaults
agent-sql connection test                            # test default connection
agent-sql connection test -c prod                    # test specific connection
```

Connection resolution: `-c` flag > `AGENT_SQL_CONNECTION` env > config default > error listing available connections.

## Safety

- **Read-only by default**: writes require `--write` flag AND a credential with write permission
- **Defense in depth**: PG uses database-level read-only transactions + session guard (libpg-query); SQLite uses OS-level SQLITE_OPEN_READONLY
- **Result cap**: `query.maxRows` (default 100)
- **Timeout**: `query.timeout` (default 30s), override per-command with `--timeout <ms>`

## Error handling

Errors include a `fixable_by` field:
- `"agent"` -- you can fix this (typo in table name, wrong syntax). Error includes valid alternatives.
- `"human"` -- requires human action (permission change, credential setup). Do not retry.
- `"retry"` -- transient error (timeout, connection lost). Worth retrying.

## Per-command usage docs

Every command group has a `usage` subcommand with detailed, LLM-optimized docs:

```bash
agent-sql usage                    # top-level overview
agent-sql connection usage         # connection commands
agent-sql schema usage             # schema exploration commands
agent-sql query usage              # query commands
agent-sql config usage             # settings keys, defaults, validation
```

Use `agent-sql <command> usage` when you need deep detail on a specific domain before acting.

## References

- [references/commands.md](references/commands.md): full command map + all flags
- [references/output.md](references/output.md): JSON output shapes + field details
