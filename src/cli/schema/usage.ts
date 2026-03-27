import type { Command } from "commander";

const USAGE_TEXT = `SCHEMA COMMANDS
===============

List tables:
  agent-sql schema tables                          All user tables
  agent-sql schema tables --include-system         Include system/internal tables

Describe a table:
  agent-sql schema describe <table>                Columns, types, nullability
  agent-sql schema describe <table> --detailed     Include constraints, indexes, and comments
  agent-sql schema describe public.users           PG namespace (schema.table)

Indexes:
  agent-sql schema indexes                         All indexes across all tables
  agent-sql schema indexes <table>                 Indexes for a specific table

Constraints:
  agent-sql schema constraints                     All constraints across all tables
  agent-sql schema constraints <table>             Constraints for a specific table
  agent-sql schema constraints <table> --type pk   Filter by type (pk, fk, unique, check)

Search:
  agent-sql schema search <pattern>                Search table and column names by pattern

Dump full schema:
  agent-sql schema dump                            DDL-style dump of all tables
  agent-sql schema dump --tables users,orders      Dump specific tables only
  agent-sql schema dump --include-system           Include system tables

OPTIONS
  -c, --connection <alias>    Connection to use (default: configured default)
  --detailed                  Include constraints, indexes, and comments (describe only)
  --include-system            Include system/internal tables (tables, dump)
  --type <type>               Filter constraint type: pk, fk, unique, check
  --tables <list>             Comma-separated table names (dump only)
  --expand <fields>           Comma-separated fields to show untruncated
  --full                      Show all fields untruncated (global truncation flag)

OUTPUT FORMAT
  All commands return JSON to stdout.
  Errors: { "error": "...", "fixable_by": "agent"|"human" } to stderr.

WORKFLOW
  1. schema tables               List what's available
  2. schema describe <table>     Inspect columns and types
  3. schema indexes <table>      Check index coverage
  4. schema constraints <table>  Understand relationships
  5. schema search <pattern>     Find tables/columns by name
`;

export function registerUsage(parent: Command): void {
  parent
    .command("usage")
    .description("Print schema command reference (LLM-optimized)")
    .action(() => {
      process.stdout.write(USAGE_TEXT);
    });
}
