# agent-sql

Read-only-by-default SQL CLI for AI agents.

- **Structured JSON output** -- all output is JSON to stdout, errors to stderr
- **LLM-optimized** -- `agent-sql usage` prints concise docs for agent consumption
- **Read-only by default** -- write access requires explicit opt-in per credential and per query
- **Defense in depth** -- driver-level, parser-level, and credential-level enforcement layers
- **PostgreSQL, MySQL, and SQLite** -- three drivers, one interface
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

### Ad-hoc usage (zero setup)

The `-c` flag accepts connection aliases, file paths, or connection strings -- no configuration needed for SQLite:

```bash
agent-sql run -c ./mydb.sqlite 'SELECT 1'
agent-sql schema tables -c ./data.db
agent-sql run -c postgres://user:pass@localhost/myapp 'SELECT * FROM users'
agent-sql run -c mysql://user:pass@localhost/myapp 'SELECT * FROM orders'
```

### Named connections (PG / MySQL / SQLite)

For databases you use repeatedly, save a named connection:

```bash
# PostgreSQL
agent-sql credential add mydb --username app --password secret
agent-sql connection add mydb --driver pg --host localhost --port 5432 --database myapp --credential mydb
agent-sql connection test

# MySQL
agent-sql credential add mydb --username app --password secret
agent-sql connection add mydb --driver mysql --host localhost --port 3306 --database myapp --credential mydb

# SQLite
agent-sql connection add local --driver sqlite --path ./data.db
```

### Explore schema

```bash
agent-sql schema tables
agent-sql schema describe users
agent-sql schema indexes users
agent-sql schema constraints users
```

### Query data

```bash
agent-sql run "SELECT * FROM users WHERE active = true" --limit 10
agent-sql query count users --where "age >= 21"
agent-sql query sample users --limit 5
agent-sql query explain "SELECT * FROM orders JOIN users ON orders.user_id = users.id"
```

## Command map

```text
agent-sql [-c <alias>] [--format json|yaml|csv] [--full] [--expand <fields>] [--timeout <ms>]
├── connection
│   ├── add <alias> --driver pg|mysql|sqlite [--host --port --database --path --url --credential]
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

| Layer | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| **Credential** | `--write` flag on `credential add` grants write permission | Same | Credential-less connections are read-only by default |
| **Query flag** | `--write` required on each write query | Same | Same |
| **SQL parser** | `libpg-query` (PG's actual parser, WASM) validates statement types against an allowlist | `START TRANSACTION READ ONLY` per query + protocol-level single-statement enforcement | N/A -- `SQLITE_OPEN_READONLY` is OS-level enforcement |
| **Result cap** | `query.maxRows` (default 100) | Same | Same |
| **Timeout** | `query.timeout` (default 30s), per-command `--timeout <ms>` | Same | Same |

Write operations require both a credential with `writePermission` and the `--write` flag on the query itself. This two-gate design prevents accidental writes even when credentials allow them.

## Output

- Default output is JSON to stdout. Use `--format yaml` or `--format csv` for alternate formats.
- CSV applies to tabular results only (`query run`, `query sample`); non-tabular commands fall back to JSON
- Errors always go to stderr as JSON `{ "error": "...", "fixable_by": "agent"|"human" }` with non-zero exit code
- NULLs preserved in query results, empty fields pruned in admin output
- Long strings truncated with per-row `@truncated` metadata showing original lengths
- `--compact` mode uses parallel arrays (column names + row arrays) for reduced token count

```bash
agent-sql --full run "SELECT * FROM users"                 # expand all fields
agent-sql --expand name,bio run "SELECT * FROM users"      # expand specific fields
agent-sql --format yaml run "SELECT * FROM users"          # YAML output
agent-sql --format csv run "SELECT * FROM users"           # CSV output
agent-sql config set defaults.format yaml                  # persistent format default
```

## Configuration

Persistent settings stored in `~/.config/agent-sql/config.json`:

| Key | Default | Description |
| --- | --- | --- |
| `defaults.format` | json | Default output format (json/yaml/csv) |
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
agent-sql connection add mydb --driver mysql --url "mysql://host/db" --credential acme
agent-sql connection set-default staging
agent-sql connection list
agent-sql connection test            # pings default connection
agent-sql connection test -c local   # pings specific connection
```

Connection resolution order: `-c` flag > `AGENT_SQL_CONNECTION` env > config default. The `-c` flag also accepts file paths (e.g. `./data.db`) and connection URLs (e.g. `postgres://user:pass@host/db`) for ad-hoc use without prior setup.

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
