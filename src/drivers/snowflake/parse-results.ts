import type { SnowflakeColumnType } from "./types";

const parseValue = (raw: string | null, col: SnowflakeColumnType): unknown => {
  if (raw === null) {
    return null;
  }

  switch (col.type.toLowerCase()) {
    case "fixed": {
      // Integer if scale=0, float otherwise
      if ((col.scale ?? 0) === 0) {
        const n = Number(raw);
        // Keep as string if outside safe integer range
        return Number.isSafeInteger(n) ? n : raw;
      }
      return Number(raw);
    }
    case "real":
    case "float":
    case "double":
      return Number(raw);

    case "boolean":
      return raw.toLowerCase() === "true" || raw === "1";

    case "text":
    case "varchar":
    case "char":
    case "string":
      return raw;

    case "variant":
    case "object":
    case "array":
    case "map":
      try {
        return JSON.parse(raw);
      } catch {
        return raw;
      }

    // Date/time types: keep as strings for LLM readability
    case "date":
    case "time":
    case "timestamp_ltz":
    case "timestamp_ntz":
    case "timestamp_tz":
      return raw;

    case "binary":
      return raw; // hex string

    default:
      return raw;
  }
};

export const parseRows = (
  data: (string | null)[][],
  rowType: SnowflakeColumnType[],
): Record<string, unknown>[] =>
  data.map((row) => {
    const record: Record<string, unknown> = {};
    for (let i = 0; i < rowType.length; i++) {
      record[rowType[i]!.name] = parseValue(row[i] ?? null, rowType[i]!);
    }
    return record;
  });

export const extractColumns = (rowType: SnowflakeColumnType[]): string[] =>
  rowType.map((col) => col.name);
