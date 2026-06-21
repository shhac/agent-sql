# agent-mongo Research

Thorough analysis of the `agent-mongo` project at `/Users/paul/projects-personal/agent-mongo/`.

## 1. Purpose and Features

**agent-mongo** is a read-only MongoDB CLI designed for AI agents (v0.4.0). It provides structured JSON output optimized for LLM consumption.

Core features:
- **Read-only by design** -- no insert/update/delete operations exist; aggregation pipelines reject `$out` and `$merge` stages
- **Structured JSON output** -- all output is JSON to stdout, errors go to stderr as `{ "error": "..." }` with non-zero exit code
- **LLM-optimized usage docs** -- every command group has a `usage` subcommand printing concise agent-friendly documentation
- **Schema inference** -- samples documents from a collection to discover field names, types, and presence percentages
- **Connection management** -- named connection aliases stored in `~/.config/agent-mongo/config.json` with credential separation
- **Credential management** -- credentials stored separately from connections, with macOS Keychain integration; connections reference credentials by name
- **Truncation** -- long strings automatically truncated with `...` suffix and companion `{field}Length` keys; controllable via `--full` and `--expand` flags
- **Configurable safety limits** -- result caps (`query.maxDocuments` default 100), query timeouts (`query.timeout` default 30s)

## 2. Architecture and Project Structure

```
src/
  index.ts                          # CLI entry point -- creates Commander program, registers all command groups
  cli/
    connection/                     # connection add/remove/update/list/test/set-default/usage
      index.ts                      # registerConnectionCommand({ program }) -- creates "connection" command group
      add.ts, remove.ts, update.ts, list.ts, test.ts, set-default.ts, usage.ts
    credential/                     # credential add/remove/list/usage
      index.ts                      # registerCredentialCommand({ program })
      add.ts, remove.ts, list.ts, usage.ts
    config/                         # config get/set/reset/list-keys/usage
      index.ts                      # registerConfigCommand({ program })
      get.ts, set.ts, reset.ts, list-keys.ts, usage.ts, valid-keys.ts
    database/                       # database list/stats/usage
      index.ts                      # registerDatabaseCommand({ program })
      list.ts, stats.ts, usage.ts
    collection/                     # collection list/schema/indexes/stats/usage
      index.ts                      # registerCollectionCommand({ program })
      list.ts, schema.ts, indexes.ts, stats.ts, usage.ts
    query/                          # query find/get/count/sample/distinct/aggregate/usage
      index.ts                      # registerQueryCommand({ program })
      find.ts, get.ts, count.ts, sample.ts, distinct.ts, aggregate.ts, usage.ts
    usage/
      index.ts                      # Top-level "usage" command -- prints full LLM overview
  lib/
    config.ts                       # Config file I/O (~/.config/agent-mongo/config.json)
                                    # Manages connections, credentials, settings
    output.ts                       # printJson, printJsonRaw, printPaginated, printError, resolvePageSize
    compact-json.ts                 # pruneEmpty() -- recursively strips null/undefined/empty-string/empty-object fields
    truncation.ts                   # applyTruncation() -- truncates any string > maxLength, adds {field}Length companion
    errors.ts                       # enhanceErrorMessage() -- adds timeout hints, index suggestions to MongoDB errors
    timeout.ts                      # getTimeout() -- resolves CLI --timeout > config query.timeout > 30s default
    keychain.ts                     # macOS Keychain integration via `security` CLI for credential storage
    version.ts                      # Version resolution: build-time define > env var > package.json walk
  mongo/
    client.ts                       # MongoClient factory with connection pooling; alias resolution chain:
                                    #   -c flag > AGENT_MONGO_CONNECTION env > config default > error
    databases.ts                    # listDatabases, getDatabaseStats
    collections.ts                  # listCollections, getCollectionStats, validateCollectionExists
    schema.ts                       # inferSchema -- $sample-based field/type discovery with depth control
    indexes.ts                      # listIndexes
    query.ts                        # findDocuments, findById, countDocuments, getDistinctValues
    aggregate.ts                    # runAggregate with $out/$merge rejection; auto-appends $limit if missing
    serialize.ts                    # BSON to JSON-safe conversion (ObjectId->hex, Date->ISO, Binary->base64, etc.)
bin/
  agent-mongo.bun.js               # Dev runner -- imports dist/index.js or falls back to src/index.ts
scripts/
  build-release-assets.sh          # Cross-platform bun build --compile for 7 targets
  release.sh                       # Version bump, git commit, tag, push
skills/
  agent-mongo/
    SKILL.md                        # Claude Code skill definition for agent discovery
    references/
      commands.md                   # Full command reference
      output.md                     # JSON output shape documentation
test/
  aggregate.test.ts                 # Tests for pipeline validation
  compact-json.test.ts              # Tests for pruneEmpty
  config.test.ts                    # Tests for config management
  credential.test.ts                # Tests for credential operations
  output.test.ts                    # Tests for output formatting
  schema.test.ts                    # Tests for schema inference
  serialize.test.ts                 # Tests for BSON serialization
  truncation.test.ts                # Tests for truncation logic
```

### Command Registration Pattern

Each `cli/*/index.ts` exports a `registerXyzCommand({ program })` function called from the top-level `src/index.ts`. Within each group, subcommands are registered by separate files (e.g., `registerFind(query)` in `cli/query/find.ts`). This keeps each file focused and under the 350-line limit enforced by linting.

### Data Flow Through a Query

1. CLI command parses args via Commander
2. `getMongoClient(alias)` resolves connection alias, creates/reuses `MongoClient`
3. Mongo layer function executes query with `maxTimeMS` from `getTimeout()`
4. Results pass through `serializeDocuments()` (BSON to JSON-safe)
5. Output passes through `pruneEmpty()` (strips nulls/empties) then `applyTruncation()` (shortens long strings)
6. Final JSON printed to stdout via `printJson()` or `printPaginated()`

### Error Handling Pattern

All CLI actions use try/catch. Errors are enhanced via `enhanceErrorMessage()` which detects MongoDB timeout errors (code 50) and adds hints about `--timeout` flag and index checking. Errors include valid values so LLMs can self-correct (e.g., `Connection "x" not found. Available: local, staging`).

## 3. Language/Runtime

- **Language**: TypeScript (strict mode, ES2022 target)
- **Runtime**: Bun
- **Module system**: ES modules (`"type": "module"` in package.json)
- **TypeScript config**: `moduleResolution: "Bundler"`, `allowImportingTsExtensions: true`, `types: ["bun-types"]`
- **Build**: `bun build src/index.ts --outdir dist --target=bun --sourcemap` for dev; `bun build --compile --target=<platform>` for release binaries
- **Testing**: `bun test` (bun's built-in test runner, no mocking libraries)
- **Linting**: oxlint (enforces `type` over `interface`, kebab-case filenames, max 350 lines/file, max 2 params/function)
- **Formatting**: oxfmt
- **Git hooks**: simple-git-hooks (pre-commit runs oxlint fix + oxfmt)

## 4. CLI Command Structure

Built with **Commander** (`commander` ^14.0.1). The program has 7 top-level command groups:

| Command Group | Subcommands | Purpose |
|---|---|---|
| `connection` | add, remove, update, list, test, set-default, usage | Manage named MongoDB connection aliases |
| `credential` | add, remove, list, usage | Manage username/password credentials (separate from connections) |
| `config` | get, set, reset, list-keys, usage | Persistent settings (limits, timeouts, truncation) |
| `database` | list, stats, usage | List databases, get database stats |
| `collection` | list, schema, indexes, stats, usage | List collections, infer schema, list indexes, get stats |
| `query` | find, get, count, sample, distinct, aggregate, usage | Query documents (all read-only) |
| `usage` | (standalone) | Print top-level LLM-optimized documentation |

Global flags (on the root program):
- `-c, --connection <alias>` -- connection alias to use
- `--expand <fields>` -- comma-separated field names to show untruncated
- `--full` -- expand all truncated fields
- `--timeout <ms>` -- query timeout override

A `preAction` hook on the root program configures truncation and timeout before any subcommand runs.

### Config Keys (with validation)

Defined in `src/cli/config/valid-keys.ts`:

| Key | Type | Default | Min | Max |
|---|---|---|---|---|
| `defaults.limit` | number | 20 | 1 | 1000 |
| `defaults.sampleSize` | number | 5 | 1 | 100 |
| `defaults.schemaSampleSize` | number | 100 | 1 | 1000 |
| `query.timeout` | number | 30000 | 1000 | 300000 |
| `query.maxDocuments` | number | 100 | 1 | 10000 |
| `truncation.maxLength` | number | 200 | 50 | 100000 |

## 5. Database Connection

### Connection Resolution Chain
1. `-c <alias>` CLI flag
2. `AGENT_MONGO_CONNECTION` environment variable
3. Config default connection (`default_connection` in config.json)
4. Error listing all available connections

### Connection Storage
- Stored in `~/.config/agent-mongo/config.json` (respects `XDG_CONFIG_HOME`)
- Each connection has: `connection_string`, optional `database`, optional `credential` reference, optional `name`
- First connection added automatically becomes the default

### Credential Separation
- Credentials (username/password) stored independently from connections
- Connections reference credentials by name via the `credential` field
- On macOS, credentials stored in Keychain (`security` CLI) with service `app.paulie.agent-mongo`; config file stores `__KEYCHAIN__` placeholder
- Non-macOS platforms fall back to plaintext storage in config file
- Credential rotation affects all referencing connections automatically

### Client Pooling
- `MongoClient` instances cached in a `Map<string, MongoClient>` keyed by alias
- Reused across operations within a single CLI invocation
- `closeAllClients()` called in `finally` blocks after each command action

### Database Name Resolution
- Explicit `--database` flag on `connection add`
- Falls back to parsing the database name from the connection string URI path

## 6. Output Formatting

### JSON Output Pipeline

All data flows through a consistent pipeline before reaching stdout:

1. **BSON Serialization** (`mongo/serialize.ts`):
   - `ObjectId` -> hex string
   - `Date` -> ISO string
   - `Binary` -> base64 string
   - `Long` -> number (if safe integer) or string
   - `Decimal128` -> string
   - `UUID` -> string
   - `RegExp` -> string
   - Unknown `_bsontype` -> `String(value)`

2. **Empty Pruning** (`lib/compact-json.ts` -- `pruneEmpty()`):
   - Recursively removes `null`, `undefined`, empty strings (after trim), empty arrays, empty objects
   - Ensures compact output without noise

3. **Truncation** (`lib/truncation.ts` -- `applyTruncation()`):
   - Any string field exceeding `configuredMaxLength` (default 200) is truncated with Unicode ellipsis suffix
   - A companion `{fieldName}Length` key is added showing the original full length
   - `--full` flag expands all fields; `--expand field1,field2` expands specific fields
   - Applied to all string fields generically (not just preset field names)

4. **Output Functions** (`lib/output.ts`):
   - `printJson(data)` -- prune + truncate + pretty-print to stdout
   - `printJsonRaw(data)` -- prune only (no truncation), used for admin/config output
   - `printPaginated(items, pageInfo)` -- wraps items in `{ items: [...], pagination?: { hasMore, nextCursor } }`
   - `printError(message)` -- writes `{ "error": "..." }` to stderr, sets `process.exitCode = 1`

### Error Output
- All errors go to stderr as JSON: `{ "error": "descriptive message" }`
- Error messages include actionable hints (valid values, suggested commands)
- MongoDB timeout errors (code 50) enhanced with `--timeout` and index check suggestions

## 7. Key Dependencies

### Runtime Dependencies (2 total)
| Package | Version | Purpose |
|---|---|---|
| `commander` | ^14.0.1 | CLI framework -- command parsing, options, help |
| `mongodb` | ^6.16.0 | MongoDB driver -- client, queries, BSON types |

### Dev Dependencies
| Package | Version | Purpose |
|---|---|---|
| `@types/node` | ^24 | Node.js type definitions |
| `bun-types` | ^1.3.8 | Bun runtime type definitions |
| `oxfmt` | ^0.28.0 | Code formatter |
| `oxlint` | ^1.43.0 | Linter (enforces type style, file naming, complexity limits) |
| `simple-git-hooks` | ^2.13.1 | Git hooks (pre-commit: lint fix + format) |
| `typescript` | ^5.8.2 | TypeScript compiler (typecheck only, no emit) |

The README claims "zero runtime deps" because the compiled binary bundles everything via `bun build --compile`.

## 8. Packaging and Distribution

### Homebrew (Primary Distribution)
```bash
brew install shhac/tap/agent-mongo
```
Distributed via a custom Homebrew tap at `shhac/tap`. The release process produces cross-platform binaries and checksums; the tap formula is updated manually with new sha256 values.

### Compiled Binaries
Built with `bun build --compile` targeting 7 platforms:
- `bun-darwin-arm64` (macOS Apple Silicon)
- `bun-darwin-x64` (macOS Intel)
- `bun-linux-x64`
- `bun-linux-x64-musl`
- `bun-linux-arm64`
- `bun-linux-arm64-musl`
- `bun-windows-x64`

Output binaries are standalone executables (no runtime dependency on Bun/Node). Version is injected at build time via `--define "AGENT_MONGO_BUILD_VERSION='$version'"`.

### Release Process
1. `bun run release <patch|minor|major>` -- bumps version in package.json, commits, tags, pushes to origin
2. `bun run build:release` -- builds all 7 platform binaries into `release/` directory, generates `checksums-sha256.txt`
3. Upload release assets to GitHub release
4. Update homebrew-tap formula with new sha256 checksums

### Claude Code Skill
```bash
npx skills add shhac/agent-mongo
```
Installs a Claude Code skill so AI agents can discover and use `agent-mongo` automatically. The skill lives at `skills/agent-mongo/SKILL.md` with references in `skills/agent-mongo/references/`. Uses the [skills.sh](https://skills.sh) ecosystem.

### Dev Runner
`bin/agent-mongo.bun.js` -- a Bun script that imports from `dist/index.js` (if built) or falls back to `src/index.ts` directly. Used during development via `bun run dev`.

## 9. Notable Design Decisions

- **Strictly read-only**: No write operations anywhere in the codebase. Aggregation explicitly validates and rejects `$out`/`$merge` stages. This makes it safe for AI agents to use without risk of data modification.
- **Self-correcting errors**: Error messages always include valid alternatives (available connections, valid config keys, suggested commands). This is specifically designed so LLMs can recover from mistakes without human intervention.
- **Generic truncation**: Unlike some tools that only truncate specific field names, agent-mongo truncates any string exceeding the threshold. The companion `{field}Length` key lets agents know data was truncated and request the full value if needed.
- **Credential separation from connections**: Credentials are stored independently and referenced by name, enabling password rotation without updating each connection individually. On macOS, credentials use the system Keychain for secure storage.
- **No mocking in tests**: Tests use inline fixtures and test pure functions directly. Test files cover serialization, truncation, schema inference, aggregation validation, config management, and output formatting.
- **Pagination via hasMore/nextCursor**: `printPaginated` adds pagination metadata only when there are more results, keeping responses compact.
- **`find` fetches limit+1**: Queries request one extra document to determine `hasMore` without a separate count query.
