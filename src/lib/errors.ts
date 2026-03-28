export type FixableBy = "agent" | "human" | "retry";

export type EnhancedError = {
  message: string;
  hint?: string;
  fixableBy: FixableBy;
};

export type ErrorContext = {
  connectionAlias?: string;
  availableTables?: string[];
  availableConnections?: string[];
};

const sanitizeHostname = (message: string, alias: string): string => {
  // Replace host:port patterns (e.g., "host db.internal.corp port 5432")
  const pgHostPort = message.replace(/host\s+[\w.-]+\s+port\s+\d+/gi, `connection '${alias}'`);
  if (pgHostPort !== message) {
    return pgHostPort;
  }

  // Replace FQDN-style hostnames (2+ dots or common patterns)
  const fqdnReplaced = message.replace(/[\w-]+\.[\w.-]+\.[\w]+/g, `'${alias}'`);
  if (fqdnReplaced !== message) {
    return fqdnReplaced;
  }

  return message;
};

const handlePgError = (
  err: Error & { code?: string },
  context?: ErrorContext,
): EnhancedError | undefined => {
  const { code } = err;
  if (typeof code !== "string" || !/^[0-9A-Z]{5}$/.test(code)) {
    return undefined;
  }

  const message = context?.connectionAlias
    ? sanitizeHostname(err.message, context.connectionAlias)
    : err.message;

  if (code === "42P01") {
    const hint = context?.availableTables?.length
      ? `Available tables: ${context.availableTables.join(", ")}. Use 'schema tables' to see all tables.`
      : "Use 'schema tables' to see available tables.";
    return { message, hint, fixableBy: "agent" };
  }

  if (code === "42703") {
    return {
      message,
      hint: "Check column names with 'schema describe <table>'.",
      fixableBy: "agent",
    };
  }

  if (code === "25006") {
    return {
      message,
      hint: "This connection is read-only. To enable writes, use a credential with writePermission: true and pass --write.",
      fixableBy: "human",
    };
  }

  if (code === "57014") {
    return {
      message,
      hint: "Query timed out. Increase with --timeout <ms> or 'config set query.timeout <ms>'.",
      fixableBy: "retry",
    };
  }

  if (code === "28P01") {
    return {
      message,
      hint: "Authentication failed. Check the credential with 'credential list' and verify the username/password.",
      fixableBy: "human",
    };
  }

  if (code === "08006" || code === "08001") {
    return {
      message,
      hint: "Could not connect to the database. Verify the host, port, and that the server is running.",
      fixableBy: "human",
    };
  }

  // Generic PG error (has valid SQLSTATE but no specific handler)
  return { message, fixableBy: "agent" };
};

const handleSqliteError = (
  err: Error & { code?: number; errno?: number },
): EnhancedError | undefined => {
  const code = typeof err.code === "number" ? err.code : err.errno;
  const msg = err.message;

  // SQLITE_READONLY (code 8) or message match
  if (code === 8 || msg.includes("attempt to write a readonly database")) {
    return {
      message: msg,
      hint: "This database is opened read-only. To enable writes, use a credential with writePermission: true and pass --write.",
      fixableBy: "human",
    };
  }

  // SQLITE_BUSY (code 5)
  if (code === 5 || msg.includes("database is locked")) {
    return {
      message: msg,
      hint: "The database is locked by another process. Try again shortly.",
      fixableBy: "retry",
    };
  }

  // SQLITE_ERROR (code 1) with specific patterns
  if (code === 1 || code === undefined) {
    if (msg.includes("no such table")) {
      return {
        message: msg,
        hint: "Table not found. Use 'schema tables' to see available tables.",
        fixableBy: "agent",
      };
    }

    if (msg.includes("no such column")) {
      return {
        message: msg,
        hint: "Column not found. Use 'schema describe <table>' to see available columns.",
        fixableBy: "agent",
      };
    }
  }

  // Only return a generic SQLite result if we have evidence this is a SQLite error
  if (typeof code === "number") {
    return { message: msg, fixableBy: "agent" };
  }

  return undefined;
};

const isMysqlError = (err: Error & { errno?: number; sqlState?: string }): boolean => {
  const { errno, sqlState } = err;
  if (typeof errno !== "number") {
    return false;
  }
  // MySQL errno values are 1000+; SQLite uses small codes (1, 5, 8, etc.)
  if (errno >= 1000) {
    return true;
  }
  // Connection-level MySQL errors (2000-range) caught above
  // Also detect via sqlState presence (MySQL drivers typically set this)
  if (typeof sqlState === "string") {
    return true;
  }
  return false;
};

const handleMysqlError = (
  err: Error & { errno?: number; sqlState?: string },
  context?: ErrorContext,
): EnhancedError | undefined => {
  if (!isMysqlError(err)) {
    return undefined;
  }

  const { errno } = err;
  const message = context?.connectionAlias
    ? sanitizeHostname(err.message, context.connectionAlias)
    : err.message;

  if (errno === 1792) {
    return {
      message,
      hint: "This connection is read-only. To enable writes, use a credential with writePermission: true and pass --write.",
      fixableBy: "human",
    };
  }

  if (errno === 1146) {
    return {
      message,
      hint: "Table not found. Use 'schema tables' to see available tables.",
      fixableBy: "agent",
    };
  }

  if (errno === 1054) {
    return {
      message,
      hint: "Column not found. Use 'schema describe <table>' to see available columns.",
      fixableBy: "agent",
    };
  }

  if (errno === 2002 || errno === 2003) {
    return {
      message,
      hint: "Could not connect to the database. Verify the host, port, and that the server is running.",
      fixableBy: "human",
    };
  }

  if (errno === 1045) {
    return {
      message,
      hint: "Authentication failed. Check the credential with 'credential list' and verify the username/password.",
      fixableBy: "human",
    };
  }

  if (errno === 1568) {
    return {
      message,
      hint: "Transaction characteristics can't be changed while a transaction is in progress. Check the connection configuration.",
      fixableBy: "human",
    };
  }

  return { message, fixableBy: "agent" };
};

const isSnowflakeError = (err: Error & { code?: string; sqlState?: string }): boolean => {
  const { code } = err;
  if (typeof code !== "string") {
    return false;
  }
  return /^\d{6}$/.test(code);
};

const handleSnowflakeError = (
  err: Error & { code?: string; sqlState?: string },
  context?: ErrorContext,
): EnhancedError | undefined => {
  if (!isSnowflakeError(err)) {
    return undefined;
  }

  const message = context?.connectionAlias
    ? sanitizeHostname(err.message, context.connectionAlias)
    : err.message;

  const { code } = err;

  if (code === "002003") {
    return {
      message,
      hint: "Table or object not found. Use 'schema tables' to see available tables.",
      fixableBy: "agent",
    };
  }

  if (code === "000904") {
    return {
      message,
      hint: "Column not found. Use 'schema describe <table>' to see available columns.",
      fixableBy: "agent",
    };
  }

  if (code === "390100" || code === "390144") {
    return {
      message,
      hint: "Authentication failed. Check the credential PAT with 'credential list' and verify it hasn't expired.",
      fixableBy: "human",
    };
  }

  if (code === "000625") {
    return {
      message,
      hint: "Query timed out. Increase with --timeout <ms> or 'config set query.timeout <ms>'.",
      fixableBy: "retry",
    };
  }

  if (code === "003001") {
    return {
      message,
      hint: "Insufficient privileges. The connected role may not have access. Check Snowflake role grants.",
      fixableBy: "human",
    };
  }

  return { message, fixableBy: "agent" };
};

const handleConnectionNotFound = (
  err: Error,
  context?: ErrorContext,
): EnhancedError | undefined => {
  if (!err.message.includes("not found")) {
    return undefined;
  }
  if (!err.message.toLowerCase().includes("connection")) {
    return undefined;
  }

  const hint = context?.availableConnections?.length
    ? `Available connections: ${context.availableConnections.join(", ")}.`
    : undefined;

  return { message: err.message, hint, fixableBy: "agent" };
};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const handlers: ((err: any, context?: ErrorContext) => EnhancedError | undefined)[] = [
  handlePgError,
  handleMysqlError,
  handleSnowflakeError,
  handleSqliteError,
  handleConnectionNotFound,
];

export const enhanceError = (err: Error, context?: ErrorContext): EnhancedError => {
  for (const handler of handlers) {
    const result = handler(err, context);
    if (result) {
      return result;
    }
  }

  const message = context?.connectionAlias
    ? sanitizeHostname(err.message, context.connectionAlias)
    : err.message;

  return { message, fixableBy: "agent" };
};
