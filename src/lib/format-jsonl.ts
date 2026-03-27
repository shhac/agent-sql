type JsonlInput = {
  columns: string[];
  rows: Record<string, unknown>[];
  hasMore: boolean;
  rowCount: number;
};

export const formatJsonl = (data: JsonlInput): string => {
  const lines = data.rows.map((row) => JSON.stringify(row));
  if (data.hasMore) {
    lines.push(JSON.stringify({ "@pagination": { hasMore: true, rowCount: data.rowCount } }));
  }
  return lines.length === 0 ? "" : `${lines.join("\n")}\n`;
};
