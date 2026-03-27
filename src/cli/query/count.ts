import type { Command } from "commander";
import type { DriverConnection } from "../../drivers/types.ts";
import { resolveDriver } from "../../drivers/resolve.ts";
import { enhanceError } from "../../lib/errors.ts";
import { printError, printJson } from "../../lib/output.ts";

type CountOptions = {
  connection?: string;
  where?: string;
};

const buildCountSql = (
  driver: DriverConnection,
  opts: { table: string; where?: string },
): string => {
  const quoted = driver.quoteIdent(opts.table);
  const whereClause = opts.where ? ` WHERE ${opts.where}` : "";
  return `SELECT COUNT(*) AS count FROM ${quoted}${whereClause}`;
};

export function registerCount(parent: Command): void {
  parent
    .command("count")
    .description("Count rows in a table")
    .argument("<table>", "Table name (supports schema.table for PG)")
    .option("-c, --connection <alias>", "Connection to use")
    .option("--where <condition>", "WHERE clause filter")
    .action(async (table: string, opts: CountOptions) => {
      const connectionAlias = opts.connection;
      try {
        const driver = await resolveDriver({ connection: connectionAlias });
        const sql = buildCountSql(driver, { table, where: opts.where });

        try {
          const result = await driver.query(sql);
          const count = result.rows[0]?.count ?? 0;
          printJson({ table, count: Number(count) });
        } finally {
          await driver.close();
        }
      } catch (err) {
        const enhanced = enhanceError(err instanceof Error ? err : new Error(String(err)), {
          connectionAlias,
        });
        printError({
          message: enhanced.message,
          hint: enhanced.hint,
          fixableBy: enhanced.fixableBy,
        });
      }
    });
}
