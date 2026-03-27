// Double-quote identifier escaping (SQL standard, works for PG and SQLite).
// Handles schema.table dot notation by quoting each part separately.
export const quoteIdentPg = (name: string): string => {
  const parts = name.split(".");
  return parts.map((p) => `"${p.replace(/"/g, '""')}"`).join(".");
};

// Backtick identifier escaping for MySQL.
// MySQL does not use schema.table (single database context), so no dot splitting.
export const quoteIdentMysql = (name: string): string => `\`${name.replace(/`/g, "``")}\``;
