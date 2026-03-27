import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { enhanceError } from "../../lib/errors.ts";
import { printError, printJson } from "../../lib/output.ts";

type CountOptions = {
  connection?: string;
  where?: string;
};

const buildCountSql = (table: string, where?: string): string => {
  const whereClause = where ? ` WHERE ${where}` : "";
  return `SELECT COUNT(*) AS count FROM ${table}${whereClause}`;
};

export function registerCount(parent: Command): void {
  parent
    .command("count")
    .description("Count rows in a table")
    .argument("<table>", "Table name (supports schema.table for PG)")
    .option("-c, --connection <alias>", "Connection to use")
    .option("--where <condition>", "WHERE clause filter")
    .action(async (table: string, opts: CountOptions) => {
      try {
        const sql = buildCountSql(table, opts.where);
        const driver = await resolveDriver({ connection: opts.connection });

        try {
          const result = await driver.query(sql);
          const count = result.rows[0]?.count ?? 0;
          printJson({ table, count: Number(count) });
        } finally {
          await driver.close();
        }
      } catch (err) {
        const enhanced = enhanceError(err instanceof Error ? err : new Error(String(err)));
        printError({
          message: enhanced.message,
          hint: enhanced.hint,
          fixableBy: enhanced.fixableBy,
        });
      }
    });
}
