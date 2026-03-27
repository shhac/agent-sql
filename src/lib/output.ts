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
  truncated?: Record<string, (number | null)[]>;
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
    let hasKeys = false;
    for (const [key, value] of Object.entries(data as Record<string, unknown>)) {
      const pruned = pruneEmpty(value);
      if (pruned !== undefined) {
        result[key] = pruned;
        hasKeys = true;
      }
    }
    return hasKeys ? result : undefined;
  }
  return data;
}

function writeStdout(json: string): void {
  process.stdout.write(`${json}\n`);
}

function writeStderr(json: string): void {
  process.stderr.write(`${json}\n`);
}

export function printJson(data: unknown, options?: PrintJsonOptions): void {
  const output = options?.prune ? (pruneEmpty(data) ?? {}) : data;
  writeStdout(JSON.stringify(output, null, 2));
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
}

export function printCompact(payload: CompactPayload): void {
  const result: Record<string, unknown> = {
    columns: payload.columns,
    rows: payload.rows,
  };
  if (payload.truncated && Object.keys(payload.truncated).length > 0) {
    result.truncated = payload.truncated;
  }
  if (payload.hasMore) {
    result.pagination = {
      hasMore: true,
      rowCount: payload.rowCount,
    };
  }
  writeStdout(JSON.stringify(result, null, 2));
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
