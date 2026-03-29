import type { Command } from "commander";

const USAGE_TEXT = `QUERY COMMANDS
==============

Run SQL:
  agent-sql run "<sql>"                    Execute any SQL query
  agent-sql query run "<sql>"              Same as above (long form)
  agent-sql query run "<sql>" --write      Enable write mode (INSERT/UPDATE/DELETE)
  agent-sql query run "<sql>" --compact    Array-of-arrays output (saves tokens)
  agent-sql query run "<sql>" --limit 50   Limit result rows

Sample rows:
  agent-sql query sample <table>           Get 5 sample rows
  agent-sql query sample <table> --limit 10
  agent-sql query sample users --where "active = true"
  agent-sql query sample analytics.events  PG namespace (schema.table)

Explain query plan:
  agent-sql query explain "<sql>"          Show execution plan
  agent-sql query explain "<sql>" --analyze  Run EXPLAIN ANALYZE (read-only queries only)

Count rows:
  agent-sql query count <table>            Count all rows
  agent-sql query count users --where "created_at > '2024-01-01'"

OPTIONS
  -c, --connection <alias>    Connection alias, file path, or URL (default: configured default)
  --format json|yaml|csv      Output format (default: json, or config defaults.format)
  --limit <n>                 Max rows (run: from config, sample: 5)
  --write                     Enable write mode (requires write-enabled credential)
  --compact                   Array-of-arrays format for large results
  --where <condition>         WHERE clause for sample/count
  --analyze                   EXPLAIN ANALYZE (explain only, read-only queries)
  --expand <fields>           Comma-separated fields to show untruncated
  --full                      Show all fields untruncated

OUTPUT FORMAT (default)
  { "columns": [...], "rows": [{...}], "pagination": { "hasMore": true, "rowCount": 20 } }

OUTPUT FORMAT (--compact)
  { "columns": [...], "rows": [[...]], "truncated": {...}, "pagination": {...} }

WRITE OUTPUT
  { "result": "ok", "rowsAffected": 5, "command": "UPDATE" }

FORMAT EXAMPLES
  agent-sql run "SELECT * FROM users" --format yaml    YAML output
  agent-sql run "SELECT * FROM users" --format csv     CSV output (tabular only)
  CSV only applies to tabular results (run, sample). Non-tabular falls back to JSON.
  Errors are always JSON regardless of --format.

SAFETY
  Queries are read-only by default. --write requires a credential with writePermission.
  Long strings are truncated; use --full or --expand to see full values.
`;

export function registerUsage(parent: Command): void {
  parent
    .command("usage")
    .description("Show LLM-optimized query command reference")
    .action(() => {
      process.stdout.write(USAGE_TEXT);
    });
}
