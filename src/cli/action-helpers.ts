import { enhanceError } from "../lib/errors.ts";
import { printError, printPaginated, printCompact } from "../lib/output.ts";
import { applyTruncation, applyTruncationCompact, configureTruncation } from "../lib/truncation.ts";
import { getSetting } from "../lib/config.ts";
import type { QueryResult, DriverConnection } from "../drivers/types.ts";
import type { Command } from "commander";
import { resolveDriver } from "../drivers/resolve.ts";

type WithDriverOpts = {
  connection?: string;
  write?: boolean;
};

export const withDriver = async <T>(
  opts: WithDriverOpts,
  fn: (driver: DriverConnection) => Promise<T>,
): Promise<T> => {
  const driver = await resolveDriver(opts);
  try {
    return await fn(driver);
  } finally {
    await driver.close();
  }
};

export const withDriverAction = async (
  opts: WithDriverOpts & { connectionAlias?: string },
  fn: (driver: DriverConnection) => Promise<void>,
): Promise<void> => {
  try {
    await withDriver(opts, fn);
  } catch (err) {
    handleActionError(err, opts.connectionAlias ?? opts.connection);
  }
};

export const handleActionError = (err: unknown, connectionAlias?: string): void => {
  const enhanced = enhanceError(err instanceof Error ? err : new Error(String(err)), {
    connectionAlias,
  });
  printError({
    message: enhanced.message,
    hint: enhanced.hint,
    fixableBy: enhanced.fixableBy,
  });
};

export const resolveConnectionAlias = (
  opts: { connection?: string },
  command: Command,
): string | undefined =>
  opts.connection ?? (command.parent?.getOptionValue("connection") as string | undefined);

type FormatOpts = { compact?: boolean; expand?: string; full?: boolean };

export const configureTruncationFromOpts = (opts: FormatOpts): void => {
  configureTruncation({
    expand: opts.expand,
    full: opts.full,
    maxLength: getSetting("truncation.maxLength") as number | undefined,
  });
};

type PrintResultOpts = {
  result: QueryResult;
  displayRows: Record<string, unknown>[];
  hasMore: boolean;
  compact?: boolean;
};

export const printQueryResults = (opts: PrintResultOpts): void => {
  if (opts.compact) {
    const arrayRows = opts.displayRows.map((row) =>
      opts.result.columns.map((col) => row[col] ?? null),
    );
    const compactResult = applyTruncationCompact({
      columns: opts.result.columns,
      rows: arrayRows,
    });
    printCompact({
      columns: compactResult.columns,
      rows: compactResult.rows,
      hasMore: opts.hasMore,
      rowCount: opts.displayRows.length,
    });
    return;
  }
  const truncatedRows = applyTruncation(opts.displayRows);
  printPaginated({
    columns: opts.result.columns,
    items: truncatedRows,
    hasMore: opts.hasMore,
    rowCount: opts.displayRows.length,
  });
};
