# agent-sql

Read-only-by-default SQL CLI for AI agents. TypeScript + Bun, compiled to standalone binaries.

## Design docs

Design docs live in `design-docs/` (gitignored, local-only). If present:

- `design-docs/agent-sql/design.md` — source of truth for all design decisions
- `design-docs/TASKS.md` — implementation plan with dependencies
- `design-docs/` — research on reference tools, Bun SQL support, read-only enforcement, SQL lexer options, MySQL readonly analysis

## Runtime

- **Bun** — runtime, test runner, and compiler (`bun build --compile` for standalone binaries)
- **Bun.SQL** — native PostgreSQL and MySQL driver via `import { SQL } from "bun"` with `sql.unsafe()` for raw SQL execution
- **bun:sqlite** — native SQLite driver via `import { Database } from "bun:sqlite"`
- **DuckDB** — subprocess driver, spawns `duckdb` CLI with NDJSON output (requires CLI installed separately)
- No npm packages needed for database access (except `libpg-query` for PG read-only guard)

## Key design decisions

See design docs (if present) for full rationale on each.

- **Read-only by default** — credentials (username, password, writePermission) stored in macOS Keychain, not in config file. Config has zero sensitive data.
- **PG/CockroachDB session guard** — `libpg-query` (PG's actual parser, WASM) with an allowlist of permitted statement types. CockroachDB uses PG wire protocol; guard fails closed for CRDB-specific syntax.
- **SQLite readonly** — `SQLITE_OPEN_READONLY` is OS-level, cannot be bypassed by SQL. No guard needed.
- **MySQL readonly** — `START TRANSACTION READ ONLY` per query + protocol-level single-statement enforcement. No parser needed.
- **DuckDB readonly** — `-readonly` CLI flag, engine-level enforcement (like SQLite). No guard needed.
- **Snowflake readonly** — client-side keyword allowlist + `MULTI_STATEMENT_COUNT=1`.
- **Subprocess drivers** — pattern for databases without native Bun drivers. Spawns CLI tool with structured output (NDJSON), parses results. See `design-docs/subprocess-drivers.md`.
- **Output** — JSON to stdout, errors to stderr. NULLs preserved in query results. `@truncated` structured object per row. `fixable_by` field on errors.
- **Skill boundary** — query, schema, config, connection list/test, usage exposed to LLMs. Credential and connection mutation are human-only.

## Dev tools

**Use `bun` for everything — not node, npm, or npx.** This is a Bun project. Use `bun run`, `bun test`, `bun add`, `bunx`.

- **Linting**: `bun run lint` / `bun run lint:fix` (oxlint — `type` over `interface`, kebab-case filenames, max 350 lines/file, max 2 params/function)
- **Formatting**: `bun run format` (oxfmt)
- **Testing**: `bun test`
- **Typecheck**: `bun run typecheck`
- **Dev runner**: `bun run dev -- <args>`
- **Git hooks**: simple-git-hooks (pre-commit: lint fix + format)

## Architecture

```
src/
  index.ts                    # CLI entry — registers commands via commander, top-level `run` alias
  cli/
    connection/               # connection add/remove/update/list/test/set-default/usage (human-only)
    credential/               # credential add/remove/list/usage (human-only)
    config/                   # config get/set/reset/list-keys/usage
    schema/                   # schema tables/describe/indexes/constraints/search/dump/usage
    query/                    # query run/sample/explain/count/usage
    usage/                    # Top-level LLM reference card
  lib/
    config.ts                 # Config file I/O (connections + settings, no credentials)
    credentials.ts            # Credential storage (Keychain on macOS, file fallback)
    output.ts                 # printJson, printPaginated, printError, printCompact
    truncation.ts             # applyTruncation with @truncated structured object
    errors.ts                 # Per-driver error mapping, fixable_by classification
    timeout.ts                # CLI --timeout > config > default 30s
    keychain.ts               # macOS Keychain via security CLI
    pg-session-guard.ts       # libpg-query allowlist for PG read-only mode
    version.ts                # Build-time define > env > package.json
  drivers/
    types.ts                  # DriverConnection interface, QueryResult, schema types
    pg/                       # PostgreSQL (Bun.SQL) — also used by CockroachDB
    sqlite.ts                 # SQLite (bun:sqlite)
    mysql/                    # MySQL (Bun.SQL with mysql adapter)
    duckdb/                   # DuckDB (subprocess — spawns duckdb CLI with NDJSON output)
    snowflake/                # Snowflake (REST API with PAT auth)
    resolve.ts                # Driver resolution from connection config
    resolve-detect.ts         # URL scheme and file extension detection
    resolve-ad-hoc.ts         # Ad-hoc connections (URLs, file paths)
```

## Key patterns

- **Command registration**: Each `cli/*/index.ts` exports `registerXyzCommand({ program })` called from `index.ts`
- **Output**: Query results through `printJson()` / `printPaginated()` / `printCompact()`. Errors through `printError()` with `fixable_by` classification. Admin output prunes nulls; query output preserves them.
- **Connection resolution**: `-c` accepts aliases, file paths (SQLite `.db`, DuckDB `.duckdb`), or connection URLs (postgres://, cockroachdb://, mysql://, duckdb://, snowflake://). Chain: `-c` flag > `AGENT_SQL_CONNECTION` env > config default > error listing available connections
- **Driver abstraction**: Shared `DriverConnection` interface, each driver implements schema discovery with its own native queries, returns shared types
- **Error messages**: Always include valid alternatives for LLM self-correction and `fixable_by` (`"agent"` / `"human"` / `"retry"`)
- **Truncation**: Strings over `truncation.maxLength` truncated with `...`, per-row `@truncated: { "column": originalLength }` metadata. Compact mode uses top-level parallel arrays.
- **PG namespaces**: Dot notation everywhere (`schema.table`), system schemas excluded by default (`--include-system` to show)

## After making changes

When changing CLI behavior, flags, output shape, or commands, also update the applicable docs:
- `src/cli/usage/index.ts` — top-level LLM reference card
- `src/cli/*/usage.ts` — per-command usage text
- `skills/agent-sql/SKILL.md` — Claude Code skill definition
- `skills/agent-sql/references/commands.md` — full command reference
- `skills/agent-sql/references/output.md` — output format reference
- `README.md` — user-facing documentation

### Adding a new driver — checklist

Every file below must be updated. Use the existing drivers as a model.

**Core:**
- [ ] `src/drivers/types.ts` — add to `Driver` union type
- [ ] `src/drivers/<name>/` — driver implementation (index.ts, schema.ts, and subprocess.ts if subprocess-based)
- [ ] `src/drivers/resolve-detect.ts` — URL pattern and/or file extension detection
- [ ] `src/drivers/resolve-ad-hoc.ts` — ad-hoc connection handling (URL and file path)
- [ ] `src/drivers/resolve.ts` — import, `configConnectBuilders` entry, write permission check, driver name in error messages
- [ ] `src/lib/errors.ts` — error handler if needed (subprocess drivers use pre-classified errors instead)

**CLI text (every place that lists drivers):**
- [ ] `src/cli/connection/add.ts` — `--driver` option description, `resolveDriver` error, `parseConnectionString` error, connection string parsing logic
- [ ] `src/cli/connection/update.ts` — `--driver` option description
- [ ] `src/cli/connection/usage.ts` — examples, `--driver` enum, AD-HOC section
- [ ] `src/cli/credential/add.ts` — hint text
- [ ] `src/cli/usage/index.ts` — driver list, ad-hoc examples, CONNECTION section

**Documentation:**
- [ ] `skills/agent-sql/SKILL.md` — description, triggers, driver list, quick start, safety section, connection resolution
- [ ] `skills/agent-sql/references/commands.md` — `-c` flag description
- [ ] `README.md` — tagline, quick start, named connections, safety table, connection resolution, env vars
- [ ] `CLAUDE.md` — this file (runtime, design decisions, architecture tree, key patterns)

**Tests:**
- [ ] `test/<name>.test.ts` — driver-specific tests (query, schema, read-only, data types, error classification)
- [ ] `test/resolve.test.ts` — URL detection, file extension detection, isConnectionUrl, isFilePath
- [ ] `test/resolve-mocked.isolated.ts` — URL detection, write permission check
