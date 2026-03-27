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
  truncated?: Record<string, (number | null)[]>;
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

    if (Object.keys(truncatedMeta).length > 0) {
      result["@truncated"] = truncatedMeta;
    }

    return result;
  });
}

export function applyTruncationCompact(input: CompactInput): CompactResult {
  const { columns, rows } = input;
  const truncatedMap: Record<string, (number | null)[]> = {};
  let hasTruncation = false;

  const newRows = rows.map((row) => {
    const newRow = [...row];

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
      hasTruncation = true;

      if (!truncatedMap[col]) {
        truncatedMap[col] = [];
      }
    }

    return newRow;
  });

  if (!hasTruncation) {
    return { columns, rows: newRows };
  }

  // Build parallel arrays: fill in lengths per row
  for (const col of Object.keys(truncatedMap)) {
    const colIdx = columns.indexOf(col);
    truncatedMap[col] = rows.map((row) => {
      const value = row[colIdx];
      if (typeof value !== "string") {
        return null;
      }
      const t = truncateValue(value, configuredMaxLength);
      return t ? t.originalLength : null;
    });
  }

  return { columns, rows: newRows, truncated: truncatedMap };
}
