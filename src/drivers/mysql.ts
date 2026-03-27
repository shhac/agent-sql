// MySQL driver: per-query START TRANSACTION READ ONLY wrapping
// + protocol-level single-statement enforcement (COM_QUERY). No parser needed.

import { SQL } from "bun";
import {
  detectCommand,
  type DriverConnection,
  type QueryResult,
  type TableInfo,
  type ColumnInfo,
  type IndexInfo,
  type ConstraintInfo,
} from "./types";
import { quoteIdentMysql } from "../lib/quote-ident";
import { getTimeout } from "../lib/timeout";

type MysqlOpts = {
  host: string;
  port: number;
  database: string;
  username: string;
  password: string;
  readonly?: boolean;
};

const WRITE_COMMANDS: ReadonlySet<string> = new Set([
  "INSERT",
  "UPDATE",
  "DELETE",
  "REPLACE",
  "CREATE",
  "ALTER",
  "DROP",
  "TRUNCATE",
]);

const CONSTRAINT_TYPE_MAP: Record<string, ConstraintInfo["type"]> = {
  "PRIMARY KEY": "primary_key",
  "FOREIGN KEY": "foreign_key",
  UNIQUE: "unique",
  CHECK: "check",
};

const CONNECT_TIMEOUT_MS = 10_000;

const withConnectTimeout = <T>(promise: Promise<T>): Promise<T> => {
  const timeout = new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error("Connection timed out")), CONNECT_TIMEOUT_MS),
  );
  return Promise.race([promise, timeout]);
};

export const connectMysql = async (opts: MysqlOpts): Promise<DriverConnection> => {
  const readonly = opts.readonly ?? true;

  const db = new SQL({
    adapter: "mysql",
    hostname: opts.host,
    port: opts.port,
    database: opts.database,
    username: opts.username,
    password: opts.password,
    max: 1,
  });

  try {
    if (readonly) {
      await withConnectTimeout(db.unsafe(`SET SESSION TRANSACTION READ ONLY`));
      await db.unsafe(`SET SESSION MAX_EXECUTION_TIME = ${getTimeout()}`);
    }
  } catch (err) {
    await db.close().catch(() => {});
    throw err;
  }

  const query = async (userSql: string, queryOpts?: { write?: boolean }): Promise<QueryResult> => {
    if (readonly) {
      await db.unsafe("START TRANSACTION READ ONLY");
      try {
        const rows = await db.unsafe(userSql);
        await db.unsafe("COMMIT");
        const columns = rows.length > 0 ? Object.keys(rows[0] as Record<string, unknown>) : [];
        return { columns, rows: rows as Record<string, unknown>[] };
      } catch (err) {
        await db.unsafe("ROLLBACK").catch(() => {});
        throw err;
      }
    }

    const command = detectCommand(userSql, WRITE_COMMANDS);
    if (command && queryOpts?.write) {
      const rows = await db.unsafe(userSql);
      const result = rows as unknown as { count?: number };
      return {
        columns: [],
        rows: [],
        rowsAffected: result.count ?? 0,
        command,
      };
    }

    const rows = await db.unsafe(userSql);
    const columns = rows.length > 0 ? Object.keys(rows[0] as Record<string, unknown>) : [];
    return { columns, rows: rows as Record<string, unknown>[] };
  };

  const quoteIdent = quoteIdentMysql;

  const getTables = async (tableOpts?: { includeSystem?: boolean }): Promise<TableInfo[]> => {
    const filter = tableOpts?.includeSystem
      ? "WHERE table_schema = DATABASE()"
      : "WHERE table_schema = DATABASE() AND table_type IN ('BASE TABLE', 'VIEW')";
    const rows = await db.unsafe(`
      SELECT table_name, table_type
      FROM information_schema.tables
      ${filter}
      ORDER BY table_name
    `);
    return (rows as { table_name: string; table_type: string }[]).map((r) => ({
      name: r.table_name,
      type: r.table_type === "VIEW" ? ("view" as const) : ("table" as const),
    }));
  };

  const describeTable = async (table: string): Promise<ColumnInfo[]> => {
    const rows = await db.unsafe(
      `
      SELECT
        c.column_name,
        c.column_type,
        c.is_nullable,
        c.column_default,
        c.column_key
      FROM information_schema.columns c
      WHERE c.table_schema = DATABASE()
        AND c.table_name = ?
      ORDER BY c.ordinal_position
    `,
      [table],
    );
    return (
      rows as {
        column_name: string;
        column_type: string;
        is_nullable: string;
        column_default: string | null;
        column_key: string;
      }[]
    ).map((r) => ({
      name: r.column_name,
      type: r.column_type,
      nullable: r.is_nullable === "YES",
      defaultValue: r.column_default ?? undefined,
      primaryKey: r.column_key === "PRI",
    }));
  };

  const getIndexes = async (table?: string): Promise<IndexInfo[]> => {
    const tableFilter = table ? "AND table_name = ?" : "";
    const params = table ? [table] : [];
    const rows = await db.unsafe(
      `
      SELECT
        index_name,
        table_name,
        GROUP_CONCAT(column_name ORDER BY seq_in_index) AS columns,
        NOT non_unique AS is_unique
      FROM information_schema.statistics
      WHERE table_schema = DATABASE()
        ${tableFilter}
      GROUP BY table_name, index_name, non_unique
      ORDER BY table_name, index_name
    `,
      params,
    );
    return (
      rows as {
        index_name: string;
        table_name: string;
        columns: string;
        is_unique: number;
      }[]
    ).map((r) => ({
      name: r.index_name,
      table: r.table_name,
      columns: r.columns.split(","),
      unique: r.is_unique === 1,
    }));
  };

  const getConstraints = async (table?: string): Promise<ConstraintInfo[]> => {
    const tableFilter = table ? "AND tc.table_name = ?" : "";
    const params = table ? [table] : [];
    const rows = await db.unsafe(
      `
      SELECT
        tc.constraint_name,
        tc.table_name,
        tc.constraint_type,
        GROUP_CONCAT(kcu.column_name ORDER BY kcu.ordinal_position) AS columns,
        kcu.referenced_table_name,
        GROUP_CONCAT(kcu.referenced_column_name ORDER BY kcu.ordinal_position) AS referenced_columns
      FROM information_schema.table_constraints tc
      JOIN information_schema.key_column_usage kcu
        ON kcu.constraint_name = tc.constraint_name
        AND kcu.table_schema = tc.table_schema
        AND kcu.table_name = tc.table_name
      WHERE tc.table_schema = DATABASE()
        ${tableFilter}
      GROUP BY tc.constraint_name, tc.table_name, tc.constraint_type,
               kcu.referenced_table_name
      ORDER BY tc.table_name, tc.constraint_name
    `,
      params,
    );

    return (
      rows as {
        constraint_name: string;
        table_name: string;
        constraint_type: string;
        columns: string;
        referenced_table_name: string | null;
        referenced_columns: string | null;
      }[]
    ).map((r) => ({
      name: r.constraint_name,
      table: r.table_name,
      type: CONSTRAINT_TYPE_MAP[r.constraint_type] ?? "check",
      columns: r.columns.split(","),
      ...(r.constraint_type === "FOREIGN KEY" && r.referenced_table_name
        ? {
            referencedTable: r.referenced_table_name,
            referencedColumns: r.referenced_columns?.split(",") ?? [],
          }
        : {}),
    }));
  };

  const searchSchema = async (
    pattern: string,
  ): Promise<{ tables: TableInfo[]; columns: { table: string; column: string }[] }> => {
    const likePattern = `%${pattern}%`;

    const tableRows = await db.unsafe(
      `
      SELECT table_name
      FROM information_schema.tables
      WHERE table_schema = DATABASE()
        AND table_name LIKE ?
      ORDER BY table_name
    `,
      [likePattern],
    );

    const tables = (tableRows as { table_name: string }[]).map((r) => ({
      name: r.table_name,
    }));

    const colRows = await db.unsafe(
      `
      SELECT table_name, column_name
      FROM information_schema.columns
      WHERE table_schema = DATABASE()
        AND column_name LIKE ?
      ORDER BY table_name, column_name
    `,
      [likePattern],
    );

    const columns = (colRows as { table_name: string; column_name: string }[]).map((r) => ({
      table: r.table_name,
      column: r.column_name,
    }));

    return { tables, columns };
  };

  const close = async (): Promise<void> => {
    await db.close();
  };

  return {
    quoteIdent,
    query,
    getTables,
    describeTable,
    getIndexes,
    getConstraints,
    searchSchema,
    close,
  };
};
