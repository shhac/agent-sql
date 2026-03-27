type CsvInput = {
  columns: string[];
  rows: Record<string, unknown>[];
};

const formatField = (value: unknown): string => {
  if (value === null || value === undefined) {return "";}
  if (typeof value === "number") {return String(value);}
  if (typeof value === "boolean") {return value ? "true" : "false";}
  if (typeof value === "object") {return quoteIfNeeded(JSON.stringify(value));}
  const str = String(value);
  return quoteIfNeeded(str);
};

const quoteIfNeeded = (str: string): string => {
  if (str.includes(",") || str.includes("\n") || str.includes('"')) {
    return `"${str.replace(/"/g, '""')}"`;
  }
  return str;
};

export const formatCsv = (data: CsvInput): string => {
  const header = data.columns.map((col) => quoteIfNeeded(col)).join(",");
  const rows = data.rows.map((row) =>
    data.columns.map((col) => formatField(row[col])).join(","),
  );
  return rows.length === 0
    ? `${header}\n`
    : `${header}\n${rows.join("\n")}\n`;
};
