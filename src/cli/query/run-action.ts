import { getSetting } from "../../lib/config.ts";
import { printJson, resolvePageSize } from "../../lib/output.ts";
import {
  handleActionError,
  configureTruncationFromOpts,
  printQueryResults,
  withDriver,
} from "../action-helpers.ts";

export type RunOptions = {
  connection?: string;
  limit?: string;
  write?: boolean;
  compact?: boolean;
  expand?: string;
  full?: boolean;
  timeout?: string;
};

const SQL_HAS_LIMIT = /\bLIMIT\s+\d+/i;

const appendLimit = (sql: string, limit: number): string => {
  if (SQL_HAS_LIMIT.test(sql)) {
    return sql;
  }
  return `${sql.replace(/;\s*$/, "")} LIMIT ${limit}`;
};

const WRITE_COMMANDS = new Set([
  "INSERT",
  "UPDATE",
  "DELETE",
  "CREATE",
  "ALTER",
  "DROP",
  "TRUNCATE",
]);

const isWriteResult = (result: { rowsAffected?: number; command?: string }): boolean =>
  WRITE_COMMANDS.has(result.command ?? "");

export async function executeRun(sql: string, opts: RunOptions): Promise<void> {
  try {
    configureTruncationFromOpts(opts);

    const maxRows = getSetting("query.maxRows") as number | undefined;
    const pageSize = resolvePageSize({
      limit: opts.limit !== undefined ? Number(opts.limit) : undefined,
      configLimit: getSetting("defaults.limit") as number | undefined,
    });
    const effectiveLimit = maxRows ? Math.min(pageSize, maxRows) : pageSize;

    await withDriver({ connection: opts.connection, write: opts.write }, async (driver) => {
      const effectiveSql = opts.write ? sql : appendLimit(sql, effectiveLimit + 1);
      const result = await driver.query(effectiveSql, { write: opts.write });

      if (opts.write && isWriteResult(result)) {
        printJson({
          result: "ok",
          rowsAffected: result.rowsAffected ?? 0,
          command: result.command,
        });
        return;
      }

      const hasMore = !opts.write && result.rows.length > effectiveLimit;
      const displayRows = hasMore ? result.rows.slice(0, effectiveLimit) : result.rows;

      printQueryResults({
        result,
        displayRows,
        hasMore,
        compact: opts.compact,
      });
    });
  } catch (err) {
    handleActionError(err, opts.connection);
  }
}
