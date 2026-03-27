const DEFAULT_MAX_LENGTH = 200;
const ELLIPSIS = "\u2026";

let expandedFields: Set<string> | "all" = new Set();
let configuredMaxLength: number = DEFAULT_MAX_LENGTH;

type TruncationOpts = {
  expand?: string;
  full?: boolean;
  maxLength?: number;
};

type CompactInput = {
  columns: string[];
  rows: unknown[][];
};

type CompactResult = {
  columns: string[];
  rows: unknown[][];
};

export function configureTruncation(opts: TruncationOpts): void {
  if (opts.full) {
    expandedFields = "all";
  } else if (opts.expand) {
    expandedFields = new Set(opts.expand.split(",").map((s) => s.trim().toLowerCase()));
  } else {
    expandedFields = new Set();
  }
  configuredMaxLength = opts.maxLength ?? DEFAULT_MAX_LENGTH;
}

function shouldExpand(fieldName: string): boolean {
  return expandedFields === "all" || expandedFields.has(fieldName.toLowerCase());
}

function truncateValue(
  value: string,
  maxLength: number,
): { truncated: string; originalLength: number } | null {
  if (value.length <= maxLength) {
    return null;
  }
  return {
    truncated: `${value.slice(0, maxLength)}${ELLIPSIS}`,
    originalLength: value.length,
  };
}

export function applyTruncation(rows: Record<string, unknown>[]): Record<string, unknown>[] {
  return rows.map((row) => {
    const result: Record<string, unknown> = {};
    const truncatedMeta: Record<string, number> = {};

    for (const [key, value] of Object.entries(row)) {
      if (typeof value !== "string" || shouldExpand(key)) {
        result[key] = value;
        continue;
      }

      const t = truncateValue(value, configuredMaxLength);
      if (!t) {
        result[key] = value;
        continue;
      }

      result[key] = t.truncated;
      truncatedMeta[key] = t.originalLength;
    }

    result["@truncated"] = Object.keys(truncatedMeta).length > 0 ? truncatedMeta : null;

    return result;
  });
}

export function applyTruncationCompact(input: CompactInput): CompactResult {
  const { columns, rows } = input;
  const newColumns = [...columns, "@truncated"];

  const newRows = rows.map((row) => {
    const newRow = [...row];
    const truncatedMeta: Record<string, number> = {};

    for (let i = 0; i < columns.length; i++) {
      const col = columns[i]!;
      const value = row[i];

      if (typeof value !== "string" || shouldExpand(col)) {
        continue;
      }

      const t = truncateValue(value, configuredMaxLength);
      if (!t) {
        continue;
      }

      newRow[i] = t.truncated;
      truncatedMeta[col] = t.originalLength;
    }

    newRow.push(Object.keys(truncatedMeta).length > 0 ? truncatedMeta : null);
    return newRow;
  });

  return { columns: newColumns, rows: newRows };
}
