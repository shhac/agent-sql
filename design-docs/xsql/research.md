# xsql Research Notes

**Repository:** https://github.com/zx06/xsql
**Tagline:** "Let AI safely query your databases"
**License:** MIT
**Latest Release:** v0.1.0 (March 2026)

## Purpose and Features

xsql is an AI-first cross-database CLI tool designed to let AI agents safely query databases. Its core value proposition combines enterprise-grade security (read-only enforcement, credential management) with AI-native design (structured JSON output, schema introspection, MCP server).

### Key features

- **Dual-layer read-only protection** -- SQL static analysis (lexical tokenizer with allowlist/denylist) plus database-level `BEGIN READ ONLY` transactions
- **OS keyring integration** for credential storage (macOS Keychain, Linux secret-tool, Windows cmdkey)
- **SSH tunnel support** for reaching internal databases through bastion hosts
- **MCP server** for Claude Desktop / AI assistant integration
- **Claude Code plugin** support
- **Schema discovery** (`xsql schema dump`) so AI can understand table structure before querying
- **Multiple output formats** -- JSON, YAML, table, CSV, auto (TTY=table, pipe=JSON)
- **Profile system** for managing multiple database connections via YAML config

## Language and Runtime

- **Language:** Go (98.9%), JavaScript (1.1% -- likely npm wrapper)
- **Go version:** 1.24.0
- **CGO:** Disabled (static binaries, no native dependencies)
- **Build tool:** GoReleaser

## CLI Command Structure

Built with **spf13/cobra**. Root command is `xsql` with subcommands:

| Command | Purpose |
|---|---|
| `xsql query "<SQL>"` | Execute read-only SQL queries |
| `xsql schema dump` | Export database structure (tables, columns, indexes, FKs) |
| `xsql profile list` | List all configured profiles |
| `xsql profile show <name>` | Show profile details (passwords masked) |
| `xsql mcp server` | Launch MCP server for AI integration |
| `xsql spec` | Output tool specification (JSON/YAML) for AI agent discovery |
| `xsql proxy` | SSH port-forwarding proxy for local DB client access |
| `xsql config init` | Create config file template |
| `xsql config set <key> <value>` | Update config via dot-notation paths |
| `xsql version` | Display version info |

### Global flags

| Flag | Short | Default | Env var |
|---|---|---|---|
| `--config <path>` | -- | `./xsql.yaml` or `~/.config/xsql/xsql.yaml` | -- |
| `--profile <name>` | `-p` | -- | `XSQL_PROFILE` |
| `--format <fmt>` | `-f` | `auto` | `XSQL_FORMAT` |

### Query-specific flags

- `--unsafe-allow-write` -- bypass read-only protection
- `--allow-plaintext` -- accept plaintext credentials in config
- `--ssh-skip-known-hosts-check` -- skip SSH host key verification
- `--query-timeout` -- override default 30s timeout

### Schema-specific flags

- `--table <pattern>` -- wildcard filter for table names
- `--include-system` -- include system tables
- `--schema-timeout` -- override default 60s timeout

### Parameter priority

CLI flags > Environment variables > Config file

## Database Support

Two databases supported via a driver registry pattern:

- **MySQL** -- via `github.com/go-sql-driver/mysql`
- **PostgreSQL** -- via `github.com/jackc/pgx/v5`

### Driver architecture

- `internal/db/registry.go` -- thread-safe driver registry with `sync.RWMutex`
- `Driver` interface: `Open(ctx, ConnOptions) (*sql.DB, *XError)`
- `Dialer` interface: enables custom network connections (SSH tunnels)
- `ConnOptions` struct: DSN, Host, Port, User, Password, Database, Params, Dialer, RegisterCloseHook
- Per-database implementations in `internal/db/mysql/` and `internal/db/pg/`

## Database Connection

### Connection methods

1. **Direct connection** -- host/port/user/password/database fields in profile
2. **DSN** -- raw connection string (highest priority if set)
3. **SSH tunnel** -- proxy config references a named SSH proxy definition with bastion host, identity file, known_hosts

### Configuration file

YAML at `~/.config/xsql/xsql.yaml`:

```yaml
ssh_proxies:
  bastion:
    host: bastion.example.com
    port: 22
    user: deploy
    identity_file: ~/.ssh/id_ed25519
    known_hosts_file: ~/.ssh/known_hosts

profiles:
  dev:
    db: mysql
    host: 127.0.0.1
    port: 3306
    user: root
    password: "keyring:dev/password"   # OS keyring reference
    database: mydb
  prod:
    db: pg
    host: db.internal
    port: 5432
    user: readonly
    password: "keyring:prod/password"
    database: analytics
    ssh_proxy: bastion
    unsafe_allow_write: false
    query_timeout: 15
    schema_timeout: 30
```

### Secrets management

- Keyring references: `keyring:<account>` format, service name `xsql`
- Plaintext passwords require `allow_plaintext: true` or `--allow-plaintext` flag
- Passwords masked in `profile show` output

## Read-Only Protection

Dual-layer defense-in-depth:

### Layer 1: SQL static analysis (client-side)

Conservative deny-by-default lexical analyzer in `internal/db/readonly.go`:

- **Allowlist** for starting keywords: `SELECT`, `SHOW`, `DESCRIBE`, `DESC`, `EXPLAIN`, `WITH`, `TABLE`, `VALUES`
- **Denylist** for forbidden keywords: `INSERT`, `UPDATE`, `DELETE`, `REPLACE`, `MERGE`, `CREATE`, `ALTER`, `DROP`, `TRUNCATE`, `BEGIN`, `COMMIT`, `ROLLBACK`, `SET`, `GRANT`, `REVOKE`, `LOCK`, `UNLOCK`, `COPY`, `LOAD`
- Genuine SQL lexical tokenizer (not regex) -- handles quoted strings, backticks, dollar-quoted strings, comments
- Multi-statement detection (rejects query batching via semicolons)
- CTE write detection (`WITH cte AS (...) INSERT ...`)
- Lock clause detection (`SELECT ... FOR SHARE`)
- Rejects queries starting with `(` to prevent subquery bypasses

### Layer 2: Database-level transaction (server-side)

- Wraps queries in `BEGIN READ ONLY` transaction, then rollback
- Write-enabled mode (`--unsafe-allow-write`) bypasses both layers entirely

## Output Formatting

### Envelope structure

All machine-readable output uses a stable envelope:

```json
// Success
{"ok": true, "schema_version": 1, "data": {"columns": [...], "rows": [...]}}

// Error
{"ok": false, "schema_version": 1, "error": {"code": "XSQL_...", "message": "...", "details": {...}}}
```

### Formats

| Format | Use case | Includes metadata envelope |
|---|---|---|
| `json` | AI/machine consumption | Yes |
| `yaml` | Human/config debugging | Yes |
| `table` | Terminal display | No |
| `csv` | Export/spreadsheet | No |
| `auto` | TTY -> table, pipe -> json | Conditional |

### Implementation

- `internal/output/contract.go` -- output envelope types
- `internal/output/format.go` -- format parsing and selection
- `internal/output/writer.go` -- format-specific rendering
- Query results stored as `[]map[string]any` with column ordering preserved
- Byte slices auto-converted to strings in result scanning

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 2 | Parameter/configuration error |
| 3 | Connection error (DB or SSH) |
| 4 | Read-only policy violation |
| 5 | SQL execution error |
| 10 | Internal error |

### Error codes

Namespaced with `XSQL_` prefix:
- Config: `XSQL_CFG_NOT_FOUND`, `XSQL_CFG_INVALID`, `XSQL_SECRET_NOT_FOUND`
- SSH: `XSQL_SSH_AUTH_FAILED`, `XSQL_SSH_HOSTKEY_MISMATCH`, `XSQL_SSH_DIAL_FAILED`
- DB: `XSQL_DB_DRIVER_UNSUPPORTED`, `XSQL_DB_CONNECT_FAILED`, `XSQL_DB_AUTH_FAILED`, `XSQL_DB_EXEC_FAILED`
- Policy: `XSQL_RO_BLOCKED`
- Infra: `XSQL_PORT_IN_USE`, `XSQL_INTERNAL`

## Key Dependencies

| Dependency | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/go-sql-driver/mysql` | MySQL driver |
| `github.com/jackc/pgx/v5` | PostgreSQL driver |
| `github.com/modelcontextprotocol/go-sdk` | MCP server SDK |
| `github.com/zalando/go-keyring` | OS keyring integration |
| `github.com/google/jsonschema-go` | JSON Schema generation (for spec command) |
| `golang.org/x/crypto` | SSH client implementation |
| `golang.org/x/term` | Terminal detection (TTY vs pipe for auto format) |
| `gopkg.in/yaml.v3` | YAML config parsing and output |

Total: 9 direct dependencies, ~17 indirect.

## Packaging and Distribution

### Build

- GoReleaser with `CGO_ENABLED=0` for static binaries
- Cross-compiled: Linux/macOS/Windows x AMD64/ARM64
- Linker flags: `-s -w` (stripped), version/commit/date injected at compile time
- Archives: tar.gz (Unix), zip (Windows)

### Distribution channels

| Channel | Command/Method |
|---|---|
| Homebrew (macOS) | `brew install zx06/tap/xsql` |
| Scoop (Windows) | Scoop bucket at `zx06/scoop-bucket` |
| npm | `npm install -g xsql-cli` (JS wrapper around binary) |
| GitHub Releases | Direct binary download |
| Claude Code Plugin | `/plugin install xsql@xsql` |

### MCP server integration

For Claude Desktop, add to MCP config:
```json
{
  "mcpServers": {
    "xsql": {
      "command": "/path/to/xsql",
      "args": ["mcp", "server", "--config", "/path/to/xsql.yaml"]
    }
  }
}
```

MCP transports: `stdio` (default) or `streamable_http` (with required auth token).

MCP tools exposed: `query`, `profile_list`, `profile_show`, `schema_dump`.

## Project Structure

```
cmd/xsql/          -- CLI entry point and command definitions
  main.go           -- entry point
  root.go           -- root command, global flags, config resolution
  query.go          -- query command
  schema.go         -- schema dump command
  profile.go        -- profile list/show commands
  mcp.go            -- MCP server command
  proxy.go          -- SSH proxy command
  spec.go           -- spec output command
  version.go        -- version command
  config.go         -- config init/set commands
  helpers.go        -- shared utilities
internal/
  app/              -- application-level orchestration (connection resolution)
  config/           -- YAML config loading and resolution
  db/               -- database abstraction layer
    mysql/           -- MySQL driver
    pg/              -- PostgreSQL driver
    registry.go      -- driver registry
    query.go         -- query execution with dual read-only protection
    readonly.go      -- SQL static analysis (lexical tokenizer)
    schema.go        -- schema extraction
  errors/           -- structured error types (XError)
  log/              -- logging
  mcp/              -- MCP server creation and tool definitions
  output/           -- output formatting (JSON, YAML, table, CSV)
  proxy/            -- SSH port forwarding
  secret/           -- keyring integration
  spec/             -- tool specification generation
  ssh/              -- SSH client and tunnel management
docs/               -- comprehensive documentation
  cli-spec.md        -- CLI specification
  config.md          -- configuration guide
  error-contract.md  -- error code reference
  ai.md             -- AI integration guide
  ssh-proxy.md       -- SSH proxy setup
  architecture.md    -- system architecture
  db.md             -- database internals
  rfcs/             -- design RFCs
.agents/skills/xsql -- AI skill definitions for IDE integration
.claude-plugin       -- Claude Code plugin manifest
```

## Design Observations

**Strengths:**
- Well-defined output contract with schema versioning (additive-only changes)
- Defense-in-depth for read-only: genuine SQL lexer, not regex, plus DB-level enforcement
- Zero external runtime dependencies (single static binary)
- AI-native from the ground up: MCP, Claude plugin, structured output, schema discovery
- Clean separation between CLI layer (cobra commands) and core logic (internal packages)
- Comprehensive error taxonomy with stable codes for programmatic handling

**Limitations / scope:**
- Only MySQL and PostgreSQL supported (no SQLite, SQL Server, etc.)
- No query result pagination or streaming (JSONL listed as "planned")
- Write mode is all-or-nothing (`--unsafe-allow-write` bypasses both protection layers)
- No connection pooling visible (opens connection per command invocation)
- No interactive/REPL mode
- v0.1.0 -- early stage
