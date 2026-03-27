import type { Command } from "commander";

const USAGE_TEXT = `agent-sql — Read-only-by-default SQL CLI for AI agents (JSON output)

SETUP (human-only):
  connection add <alias> --driver pg|sqlite [--host --port --database --path --url --credential]
  credential add <alias> --username <u> --password <p> [--write]
  connection test [alias]

COMMANDS:
  connection add|remove|update|list|test|set-default   Manage SQL connections
  credential add|remove|list                           Manage stored credentials
  config get|set|reset|list-keys                       Persistent settings

  run "<sql>" [--limit] [--write] [--compact]          Execute SQL (top-level alias)
  query run "<sql>" [--limit] [--write] [--compact]    Execute SQL
  query sample <table> [--limit] [--where]             Sample rows
  query explain "<sql>" [--analyze]                    EXPLAIN a query
  query count <table> [--where]                        Count rows

  schema tables [--include-system]                     List tables
  schema describe <table> [--detailed]                  Columns, types, nullability
  schema indexes [table]                               Index details
  schema constraints [table] [--type]                  PKs, FKs, unique, check
  schema search <pattern>                              Search table/column names
  schema dump [--tables] [--include-system]            Full schema dump

GLOBAL FLAGS: -c <alias> (connection), --expand <fields>, --full, --timeout <ms>

CONNECTION: -c flag > AGENT_SQL_CONNECTION env > config default.
  PG requires a stored credential. SQLite uses file path (credential optional).

SAFETY: Read-only by default. Use --write to opt in to writes.
  --write requires a credential with writePermission (or credential-less SQLite).
  Results capped at query.maxRows (default 100). Timeout: query.timeout (default 30s).

OUTPUT: JSON to stdout. Errors: { "error": "...", "fixable_by": "agent"|"human" } to stderr.
  Long strings truncated with @truncated metadata. Use --full or --expand <field> to expand.

DETAIL: Run "<command> usage" for per-command docs.
`;

export function registerUsageCommand({ program }: { program: Command }): void {
  program
    .command("usage")
    .description("Print concise documentation (LLM-optimized)")
    .action(() => {
      console.log(USAGE_TEXT.trim());
    });
}
