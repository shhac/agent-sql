import { getFormat } from "./format.js";
import { formatYaml } from "./format-yaml.js";
import { formatCsv } from "./format-csv.js";
import { formatJsonl } from "./format-jsonl.js";
import { assertNever } from "./assert-never.js";

const DEFAULT_PAGE_SIZE = 20;

type PrintJsonOptions = {
  prune?: boolean;
};

type ErrorPayload = {
  message: string;
  hint?: string;
  fixableBy?: "agent" | "human" | "retry";
};

type PaginatedPayload = {
  columns: string[];
  items: Record<string, unknown>[];
  hasMore: boolean;
  rowCount: number;
};

type CompactPayload = {
  columns: string[];
  rows: unknown[][];
  hasMore: boolean;
  rowCount: number;
};

type PageSizeOptions = {
  limit?: number;
  configLimit?: number;
};

function pruneEmpty(data: unknown): unknown {
  if (data === null || data === undefined) {
    return undefined;
  }
  if (Array.isArray(data)) {
    const pruned = data.map(pruneEmpty).filter((v) => v !== undefined);
    return pruned.length === 0 ? undefined : pruned;
  }
  if (typeof data === "object") {
    const result: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(data as Record<string, unknown>)) {
      const pruned = pruneEmpty(value);
      if (pruned !== undefined) {
        result[key] = pruned;
      }
    }
    return Object.keys(result).length > 0 ? result : undefined;
  }
  return data;
}

function writeStdout(json: string): void {
  process.stdout.write(`${json}\n`);
}

function writeStdoutRaw(text: string): void {
  process.stdout.write(text);
}

function writeStderr(json: string): void {
  process.stderr.write(`${json}\n`);
}

export function printJson(data: unknown, options?: PrintJsonOptions): void {
  const output = options?.prune ? (pruneEmpty(data) ?? {}) : data;
  const format = getFormat();
  switch (format) {
    case "yaml": {
      writeStdout(formatYaml(output).trimEnd());
      return;
    }
    case "jsonl":
    case "csv":
    case "json": {
      writeStdout(JSON.stringify(output, null, 2));
      return;
    }
    default:
      assertNever(format);
  }
}

export function printError(payload: ErrorPayload): void {
  const output: Record<string, string> = { error: payload.message };
  if (payload.hint !== undefined) {
    output.hint = payload.hint;
  }
  if (payload.fixableBy !== undefined) {
    output.fixable_by = payload.fixableBy;
  }
  writeStderr(JSON.stringify(output));
  process.exitCode = 1;
}

export function printPaginated(payload: PaginatedPayload): void {
  const format = getFormat();
  switch (format) {
    case "jsonl": {
      const output = formatJsonl({
        columns: payload.columns,
        rows: payload.items,
        hasMore: payload.hasMore,
        rowCount: payload.rowCount,
      });
      if (output) {
        writeStdoutRaw(output);
      }
      return;
    }
    case "csv": {
      writeStdout(formatCsv({ columns: payload.columns, rows: payload.items }).trimEnd());
      return;
    }
    case "yaml": {
      const result: Record<string, unknown> = {
        columns: payload.columns,
        rows: payload.items,
      };
      if (payload.hasMore) {
        result.pagination = {
          hasMore: true,
          rowCount: payload.rowCount,
        };
      }
      writeStdout(formatYaml(result).trimEnd());
      return;
    }
    case "json": {
      const result: Record<string, unknown> = {
        columns: payload.columns,
        rows: payload.items,
      };
      if (payload.hasMore) {
        result.pagination = {
          hasMore: true,
          rowCount: payload.rowCount,
        };
      }
      writeStdout(JSON.stringify(result, null, 2));
      return;
    }
    default:
      assertNever(format);
  }
}

const compactToNamed = (columns: string[], rows: unknown[][]): Record<string, unknown>[] =>
  rows.map((row) => {
    const obj: Record<string, unknown> = {};
    columns.forEach((col, i) => {
      obj[col] = row[i];
    });
    return obj;
  });

export function printCompact(payload: CompactPayload): void {
  const format = getFormat();
  switch (format) {
    case "jsonl": {
      const namedRows = compactToNamed(payload.columns, payload.rows);
      const output = formatJsonl({
        columns: payload.columns,
        rows: namedRows,
        hasMore: payload.hasMore,
        rowCount: payload.rowCount,
      });
      if (output) {
        writeStdoutRaw(output);
      }
      return;
    }
    case "csv": {
      const namedRows = compactToNamed(payload.columns, payload.rows);
      writeStdout(formatCsv({ columns: payload.columns, rows: namedRows }).trimEnd());
      return;
    }
    case "yaml": {
      const result: Record<string, unknown> = {
        columns: payload.columns,
        rows: payload.rows,
      };
      if (payload.hasMore) {
        result.pagination = {
          hasMore: true,
          rowCount: payload.rowCount,
        };
      }
      writeStdout(formatYaml(result).trimEnd());
      return;
    }
    case "json": {
      const result: Record<string, unknown> = {
        columns: payload.columns,
        rows: payload.rows,
      };
      if (payload.hasMore) {
        result.pagination = {
          hasMore: true,
          rowCount: payload.rowCount,
        };
      }
      writeStdout(JSON.stringify(result, null, 2));
      return;
    }
    default:
      assertNever(format);
  }
}

export function resolvePageSize(opts: PageSizeOptions): number {
  if (opts.limit !== undefined) {
    return opts.limit;
  }
  if (opts.configLimit !== undefined) {
    return opts.configLimit;
  }
  return DEFAULT_PAGE_SIZE;
}
