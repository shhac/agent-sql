// Snowflake read-only guard: keyword allowlist
// Snowflake has no BEGIN TRANSACTION READ ONLY, so we use a client-side allowlist
// of permitted statement types. This is the same enforcement level as MySQL.

const READ_ONLY_ALLOWED = new Set([
  "SELECT",
  "WITH", // CTEs resolve to SELECT
  "SHOW", // SHOW TABLES, SHOW COLUMNS, etc.
  "DESCRIBE", // table metadata
  "DESC", // alias for DESCRIBE
  "EXPLAIN", // query plans
  "LIST", // stage listing (read operation)
  "LS", // alias for LIST
]);

export const validateReadOnly = (sql: string): void => {
  const trimmed = sql.trimStart();
  const upper = trimmed.toUpperCase();

  for (const keyword of READ_ONLY_ALLOWED) {
    // Must match keyword followed by whitespace, ( or end of string
    // This prevents false matches like "SELECTIVE_INSERT" starting with "SELECT"
    if (
      upper.startsWith(keyword) &&
      (trimmed.length === keyword.length || /\s|\(/.test(trimmed[keyword.length]!))
    ) {
      return; // allowed
    }
  }

  // Extract the first word for the error message
  const firstWord = upper.match(/^\S+/)?.[0] ?? "UNKNOWN";
  throw Object.assign(
    new Error(
      `Statement type '${firstWord}' is not allowed in read-only mode. Allowed: SELECT, SHOW, DESCRIBE, EXPLAIN.`,
    ),
    {
      hint: "To execute write operations, use a connection with a write-enabled credential and pass --write.",
      fixableBy: "human" as const,
    },
  );
};
