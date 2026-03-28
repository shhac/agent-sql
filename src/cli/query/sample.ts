import type { Command } from "commander";
import type { DriverConnection } from "../../drivers/types.ts";
import {
  handleActionError,
  resolveConnectionAlias,
  configureTruncationFromOpts,
  printQueryResults,
  withDriver,
} from "../action-helpers.ts";

type SampleOptions = {
  connection?: string;
  limit?: string;
  where?: string;
  compact?: boolean;
  expand?: string;
  full?: boolean;
};

const DEFAULT_SAMPLE_SIZE = 5;

const buildSampleSql = (
  driver: DriverConnection,
  opts: { table: string; limit: number; where?: string },
): string => {
  const quoted = driver.quoteIdent(opts.table);
  const whereClause = opts.where ? ` WHERE ${opts.where}` : "";
  return `SELECT * FROM ${quoted}${whereClause} LIMIT ${opts.limit}`;
};

export function registerSample(parent: Command): void {
  parent
    .command("sample")
    .description("Return sample rows from a table")
    .argument("<table>", "Table name (supports schema.table for PG)")
    .option("--limit <n>", "Number of sample rows (default 5)")
    .option("--where <condition>", "WHERE clause filter")
    .option("--compact", "Use compact array-of-arrays output format")
    .option("--expand <fields>", "Comma-separated fields to show untruncated")
    .option("--full", "Show all fields untruncated")
    .action(async (table: string, opts: SampleOptions) => {
      const connectionAlias = resolveConnectionAlias(opts, parent);
      try {
        configureTruncationFromOpts(opts);

        const limit = opts.limit !== undefined ? Number(opts.limit) : DEFAULT_SAMPLE_SIZE;
        await withDriver({ connection: connectionAlias }, async (driver) => {
          const sql = buildSampleSql(driver, { table, limit, where: opts.where });
          const result = await driver.query(sql);

          printQueryResults({
            result,
            displayRows: result.rows,
            hasMore: false,
            compact: opts.compact,
          });
        });
      } catch (err) {
        handleActionError(err, connectionAlias);
      }
    });
}
