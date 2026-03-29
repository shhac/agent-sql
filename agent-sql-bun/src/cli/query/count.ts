import type { Command } from "commander";
import type { DriverConnection } from "../../drivers/types.ts";
import { printJson } from "../../lib/output.ts";
import { resolveConnectionAlias, withDriverAction } from "../action-helpers.ts";

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
    .option("--where <condition>", "WHERE clause filter")
    .action(async (table: string, opts: CountOptions) => {
      const connectionAlias = resolveConnectionAlias(opts, parent);
      await withDriverAction({ connection: connectionAlias }, async (driver) => {
        const sql = buildCountSql(driver, { table, where: opts.where });
        const result = await driver.query(sql);
        const count = result.rows[0]?.count ?? 0;
        printJson({ table, count: Number(count) });
      });
    });
}
