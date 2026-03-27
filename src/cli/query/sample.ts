import type { Command } from "commander";
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

const buildSampleSql = (table: string, opts: { limit: number; where?: string }): string => {
  const whereClause = opts.where ? ` WHERE ${opts.where}` : "";
  return `SELECT * FROM ${table}${whereClause} LIMIT ${opts.limit}`;
};

export function registerSample(parent: Command): void {
  parent
    .command("sample")
    .description("Return sample rows from a table")
    .argument("<table>", "Table name (supports schema.table for PG)")
    .option("-c, --connection <alias>", "Connection to use")
    .option("--limit <n>", "Number of sample rows (default 5)")
    .option("--where <condition>", "WHERE clause filter")
    .option("--compact", "Use compact array-of-arrays output format")
    .option("--expand <fields>", "Comma-separated fields to show untruncated")
    .option("--full", "Show all fields untruncated")
    .action(async (table: string, opts: SampleOptions) => {
      try {
        configureTruncation({
          expand: opts.expand,
          full: opts.full,
          maxLength: getSetting("truncation.maxLength") as number | undefined,
        });

        const limit = opts.limit !== undefined ? Number(opts.limit) : DEFAULT_SAMPLE_SIZE;
        const sql = buildSampleSql(table, { limit, where: opts.where });

        const driver = await resolveDriver({ connection: opts.connection });

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
              truncated: compactResult.truncated,
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
        const enhanced = enhanceError(err instanceof Error ? err : new Error(String(err)));
        printError({
          message: enhanced.message,
          hint: enhanced.hint,
          fixableBy: enhanced.fixableBy,
        });
      }
    });
}
