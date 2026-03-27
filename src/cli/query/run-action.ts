import { resolveDriver } from "../../drivers/resolve.ts";
import { getSetting } from "../../lib/config.ts";
import { enhanceError } from "../../lib/errors.ts";
import {
  printCompact,
  printError,
  printJson,
  printPaginated,
  resolvePageSize,
} from "../../lib/output.ts";
import {
  applyTruncation,
  applyTruncationCompact,
  configureTruncation,
} from "../../lib/truncation.ts";

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
    configureTruncation({
      expand: opts.expand,
      full: opts.full,
      maxLength: getSetting("truncation.maxLength") as number | undefined,
    });

    const maxRows = getSetting("query.maxRows") as number | undefined;
    const pageSize = resolvePageSize({
      limit: opts.limit !== undefined ? Number(opts.limit) : undefined,
      configLimit: getSetting("defaults.limit") as number | undefined,
    });
    const effectiveLimit = maxRows ? Math.min(pageSize, maxRows) : pageSize;

    const driver = await resolveDriver({
      connection: opts.connection,
      write: opts.write,
    });

    try {
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

      if (opts.compact) {
        const arrayRows = displayRows.map((row) => result.columns.map((col) => row[col] ?? null));
        const compactResult = applyTruncationCompact({
          columns: result.columns,
          rows: arrayRows,
        });
        printCompact({
          columns: compactResult.columns,
          rows: compactResult.rows,
          truncated: compactResult.truncated,
          hasMore,
          rowCount: displayRows.length,
        });
        return;
      }

      const truncatedRows = applyTruncation(displayRows);
      printPaginated({
        columns: result.columns,
        items: truncatedRows,
        hasMore,
        rowCount: displayRows.length,
      });
    } finally {
      await driver.close();
    }
  } catch (err) {
    const enhanced = enhanceError(err instanceof Error ? err : new Error(String(err)), {
      connectionAlias: opts.connection,
    });
    printError({
      message: enhanced.message,
      hint: enhanced.hint,
      fixableBy: enhanced.fixableBy,
    });
  }
}
