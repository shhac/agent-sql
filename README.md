# agent-sql

Read-only-by-default SQL CLI for AI agents.

- **Structured JSON output** -- all output is JSON to stdout, errors to stderr
- **LLM-optimized** -- `agent-sql usage` prints concise docs for agent consumption
- **Read-only by default** -- write access requires explicit opt-in per credential and per query
- **Defense in depth** -- driver-level, parser-level, and credential-level enforcement layers
- **PostgreSQL + SQLite** -- MySQL planned for post-v1
- **Zero runtime deps** -- single compiled binary via `bun build --compile`

## Installation

```bash
brew install shhac/tap/agent-sql
```

### Other options

Download a binary from [GitHub releases](https://github.com/shhac/agent-sql/releases), or build from source:

```bash
git clone https://github.com/shhac/agent-sql.git
cd agent-sql
bun install
bun run build:release
```

### Claude Code / AI agent skill

```bash
npx skills add shhac/agent-sql
```

This installs the `agent-sql` skill so Claude Code (and other AI agents) can discover and use `agent-sql` automatically. See [skills.sh](https://skills.sh) for details.

## Quick start

### 1. Add a connection

```bash
# PostgreSQL
agent-sql credential add mydb --username app --password secret
agent-sql connection add mydb --driver pg --host localhost --port 5432 --database myapp --credential mydb
agent-sql connection test

# SQLite
agent-sql connection add local --driver sqlite --path ./data.db
```

### 2. Explore schema

```bash
agent-sql schema tables
agent-sql schema describe users
agent-sql schema indexes users
agent-sql schema constraints users
```

### 3. Query data

```bash
agent-sql run "SELECT * FROM users WHERE active = true" --limit 10
agent-sql query count users --where "age >= 21"
agent-sql query sample users --limit 5
agent-sql query explain "SELECT * FROM orders JOIN users ON orders.user_id = users.id"
```

## Command map

```text
agent-sql [-c <alias>] [--full] [--expand <fields>] [--timeout <ms>]
├── connection
│   ├── add <alias> --driver pg|sqlite [--host --port --database --path --url --credential]
│   ├── update <alias> [--credential <name>] [--no-credential] [--database <db>]
│   ├── remove <alias>
│   ├── list
│   ├── test [alias]
│   ├── set-default <alias>
│   └── usage
├── credential
│   ├── add <alias> --username <u> --password <p> [--write]
│   ├── remove <alias> [--force]
│   ├── list
│   └── usage
├── config
│   ├── get <key>
│   ├── set <key> <value>
│   ├── reset
│   ├── list-keys
│   └── usage
├── schema
│   ├── tables [--include-system]
│   ├── describe <table> [--detailed]
│   ├── indexes [table]
│   ├── constraints [table] [--type]
│   ├── search <pattern>
│   ├── dump [--tables] [--include-system]
│   └── usage
├── query
│   ├── run "<sql>" [--limit] [--write] [--compact]
│   ├── sample <table> [--limit] [--where]
│   ├── explain "<sql>" [--analyze]
│   ├── count <table> [--where]
│   └── usage
├── run "<sql>" [--limit] [--write] [--compact]    # top-level alias for query run
└── usage                                           # LLM-optimized docs
```

Each command group has a `usage` subcommand for detailed, LLM-friendly documentation (e.g., `agent-sql query usage`). The top-level `agent-sql usage` gives a broad overview.

## Safety model

agent-sql is read-only by default with defense in depth:

| Layer | PostgreSQL | SQLite |
| --- | --- | --- |
| **Credential** | `--write` flag on `credential add` grants write permission | Credential-less connections are read-only by default |
| **Query flag** | `--write` required on each write query | Same |
| **SQL parser** | `libpg-query` (PG's actual parser, WASM) validates statement types against an allowlist | N/A -- `SQLITE_OPEN_READONLY` is OS-level enforcement |
| **Result cap** | `query.maxRows` (default 100) | Same |
| **Timeout** | `query.timeout` (default 30s), per-command `--timeout <ms>` | Same |

Write operations require both a credential with `writePermission` and the `--write` flag on the query itself. This two-gate design prevents accidental writes even when credentials allow them.

## Output

- All output is JSON to stdout
- Errors go to stderr as `{ "error": "...", "fixable_by": "agent"|"human" }` with non-zero exit code
- NULLs preserved in query results, empty fields pruned in admin output
- Long strings truncated with per-row `@truncated` metadata showing original lengths
- `--compact` mode uses parallel arrays (column names + row arrays) for reduced token count

```bash
agent-sql --full run "SELECT * FROM users"                 # expand all fields
agent-sql --expand name,bio run "SELECT * FROM users"      # expand specific fields
```

## Configuration

Persistent settings stored in `~/.config/agent-sql/config.json`:

| Key | Default | Description |
| --- | --- | --- |
| `defaults.limit` | 20 | Default row limit for queries |
| `query.timeout` | 30000 | Query timeout in milliseconds |
| `query.maxRows` | 100 | Maximum rows per query |
| `truncation.maxLength` | 200 | String truncation threshold |

```bash
agent-sql config set defaults.limit 50
agent-sql config get query.timeout
agent-sql config list-keys           # all keys with defaults and ranges
agent-sql config reset               # reset all to defaults
```

## Connection management

```bash
agent-sql connection add staging --driver pg --url "postgres://host/db" --credential acme
agent-sql connection set-default staging
agent-sql connection list
agent-sql connection test            # pings default connection
agent-sql connection test -c local   # pings specific connection
```

Connection resolution order: `-c` flag > `AGENT_SQL_CONNECTION` env > config default.

## Environment variables

| Variable | Description |
| --- | --- |
| `AGENT_SQL_CONNECTION` | Default connection alias |
| `XDG_CONFIG_HOME` | Override config directory (default: `~/.config`) |

## Development

```bash
bun install
bun run dev -- --help        # run in dev mode
bun run typecheck            # type check
bun test                     # run tests
bun run lint                 # lint
```

## License

MIT
