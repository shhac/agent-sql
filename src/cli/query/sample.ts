import type { Command } from "commander";
import type { DriverConnection } from "../../drivers/types.ts";
import { resolveDriver } from "../../drivers/resolve.ts";
import { getSetting } from "../../lib/config.ts";
import { enhanceError } from "../../lib/errors.ts";
import { printCompact, printError, printPaginated } from "../../lib/output.ts";
import {
  applyTruncation,
  applyTruncationCompact,
  configureTruncation,
} from "../../lib/truncation.ts";

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
      const connectionAlias =
        opts.connection ?? (parent.parent?.getOptionValue("connection") as string | undefined);
      try {
        configureTruncation({
          expand: opts.expand,
          full: opts.full,
          maxLength: getSetting("truncation.maxLength") as number | undefined,
        });

        const limit = opts.limit !== undefined ? Number(opts.limit) : DEFAULT_SAMPLE_SIZE;
        const driver = await resolveDriver({ connection: connectionAlias });
        const sql = buildSampleSql(driver, { table, limit, where: opts.where });

        try {
          const result = await driver.query(sql);

          if (opts.compact) {
            const arrayRows = result.rows.map((row) =>
              result.columns.map((col) => row[col] ?? null),
            );
            const compactResult = applyTruncationCompact({
              columns: result.columns,
              rows: arrayRows,
            });
            printCompact({
              columns: compactResult.columns,
              rows: compactResult.rows,
              hasMore: false,
              rowCount: result.rows.length,
            });
            return;
          }

          const truncatedRows = applyTruncation(result.rows);
          printPaginated({
            columns: result.columns,
            items: truncatedRows,
            hasMore: false,
            rowCount: result.rows.length,
          });
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
