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
import { loadPgParser, validateReadOnlyQuery } from "../lib/pg-session-guard";
import { getTimeout } from "../lib/timeout";

type PgOpts = {
  host: string;
  port: number;
  database: string;
  username: string;
  password: string;
  readonly?: boolean;
};

const WRITE_COMMANDS: ReadonlySet<string> = new Set(["INSERT", "UPDATE", "DELETE", "MERGE", "COPY"]);

const parseTableRef = (table: string): { schema: string; table: string } => {
  const parts = table.split(".");
  if (parts.length === 2) {
    return { schema: parts[0]!, table: parts[1]! };
  }
  return { schema: "public", table: parts[0]! };
};

const CONNECT_TIMEOUT_MS = 10_000;

const withConnectTimeout = <T>(promise: Promise<T>): Promise<T> => {
  const timeout = new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error("Connection timed out")), CONNECT_TIMEOUT_MS),
  );
  return Promise.race([promise, timeout]);
};

export const connectPg = async (opts: PgOpts): Promise<DriverConnection> => {
  const readonly = opts.readonly ?? true;

  const db = new SQL({
    hostname: opts.host,
    port: opts.port,
    database: opts.database,
    username: opts.username,
    password: opts.password,
    max: 1,
  });

  try {
    const timeoutMs = getTimeout();
    await withConnectTimeout(db.unsafe(`SET statement_timeout = ${timeoutMs}`));

    if (readonly) {
      await loadPgParser();
      await db.unsafe("SET default_transaction_read_only = on");
    }
  } catch (err) {
    await db.close().catch(() => {});
    throw err;
  }

  const query = async (userSql: string, queryOpts?: { write?: boolean }): Promise<QueryResult> => {
    if (readonly) {
      const validation = validateReadOnlyQuery(userSql);
      if (!validation.ok) {
        throw new Error(validation.error);
      }

      await db.unsafe("BEGIN READ ONLY");
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

  const getTables = async (tableOpts?: { includeSystem?: boolean }): Promise<TableInfo[]> => {
    const filter = tableOpts?.includeSystem
      ? ""
      : "WHERE table_schema NOT IN ('pg_catalog', 'information_schema')";
    const rows = await db.unsafe(`
      SELECT table_schema, table_name
      FROM information_schema.tables
      ${filter}
      ORDER BY table_schema, table_name
    `);
    return (rows as { table_schema: string; table_name: string }[]).map((r) => ({
      name: `${r.table_schema}.${r.table_name}`,
      schema: r.table_schema,
    }));
  };

  const describeTable = async (table: string): Promise<ColumnInfo[]> => {
    const ref = parseTableRef(table);
    const rows = await db.unsafe(
      `
      SELECT
        c.column_name,
        c.data_type,
        c.is_nullable,
        c.column_default,
        CASE WHEN tc.constraint_type = 'PRIMARY KEY' THEN true ELSE false END AS is_pk
      FROM information_schema.columns c
      LEFT JOIN information_schema.key_column_usage kcu
        ON kcu.table_schema = c.table_schema
        AND kcu.table_name = c.table_name
        AND kcu.column_name = c.column_name
      LEFT JOIN information_schema.table_constraints tc
        ON tc.constraint_name = kcu.constraint_name
        AND tc.table_schema = kcu.table_schema
        AND tc.constraint_type = 'PRIMARY KEY'
      WHERE c.table_schema = $1
        AND c.table_name = $2
      ORDER BY c.ordinal_position
    `,
      [ref.schema, ref.table],
    );
    return (
      rows as {
        column_name: string;
        data_type: string;
        is_nullable: string;
        column_default: string | null;
        is_pk: boolean;
      }[]
    ).map((r) => ({
      name: r.column_name,
      type: r.data_type,
      nullable: r.is_nullable === "YES",
      defaultValue: r.column_default ?? undefined,
      primaryKey: r.is_pk,
    }));
  };

  const getIndexes = async (table?: string): Promise<IndexInfo[]> => {
    const ref = table ? parseTableRef(table) : undefined;
    const whereClause = ref
      ? "WHERE i.schemaname = $1 AND i.tablename = $2"
      : "WHERE i.schemaname NOT IN ('pg_catalog', 'information_schema')";
    const params = ref ? [ref.schema, ref.table] : [];

    const rows = await db.unsafe(
      `
      SELECT
        i.schemaname,
        i.tablename,
        i.indexname,
        ix.indisunique,
        array_agg(a.attname ORDER BY k.n) AS columns
      FROM pg_indexes i
      JOIN pg_class c ON c.relname = i.indexname
      JOIN pg_namespace ns ON ns.nspname = i.schemaname AND ns.oid = c.relnamespace
      JOIN pg_index ix ON ix.indexrelid = c.oid
      CROSS JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, n)
      JOIN pg_attribute a
        ON a.attrelid = ix.indrelid AND a.attnum = k.attnum
      ${whereClause}
      GROUP BY i.schemaname, i.tablename, i.indexname, ix.indisunique
      ORDER BY i.schemaname, i.tablename, i.indexname
    `,
      params,
    );
    return (
      rows as {
        schemaname: string;
        tablename: string;
        indexname: string;
        indisunique: boolean;
        columns: string[];
      }[]
    ).map((r) => ({
      name: r.indexname,
      table: `${r.schemaname}.${r.tablename}`,
      schema: r.schemaname,
      columns: r.columns,
      unique: r.indisunique,
    }));
  };

  const getConstraints = async (table?: string): Promise<ConstraintInfo[]> => {
    const ref = table ? parseTableRef(table) : undefined;
    const whereClause = ref
      ? "WHERE tc.table_schema = $1 AND tc.table_name = $2"
      : "WHERE tc.table_schema NOT IN ('pg_catalog', 'information_schema')";
    const params = ref ? [ref.schema, ref.table] : [];

    const rows = await db.unsafe(
      `
      SELECT
        tc.constraint_name,
        tc.table_schema,
        tc.table_name,
        tc.constraint_type,
        array_agg(DISTINCT kcu.column_name ORDER BY kcu.column_name) AS columns,
        ccu.table_schema AS ref_schema,
        ccu.table_name AS ref_table,
        array_agg(DISTINCT ccu.column_name ORDER BY ccu.column_name)
          FILTER (WHERE tc.constraint_type = 'FOREIGN KEY') AS ref_columns
      FROM information_schema.table_constraints tc
      JOIN information_schema.key_column_usage kcu
        ON kcu.constraint_name = tc.constraint_name
        AND kcu.table_schema = tc.table_schema
      LEFT JOIN information_schema.constraint_column_usage ccu
        ON ccu.constraint_name = tc.constraint_name
        AND ccu.table_schema = tc.table_schema
      ${whereClause}
      GROUP BY tc.constraint_name, tc.table_schema, tc.table_name,
               tc.constraint_type, ccu.table_schema, ccu.table_name
      ORDER BY tc.table_schema, tc.table_name, tc.constraint_name
    `,
      params,
    );

    const typeMap: Record<string, ConstraintInfo["type"]> = {
      "PRIMARY KEY": "primary_key",
      "FOREIGN KEY": "foreign_key",
      UNIQUE: "unique",
      CHECK: "check",
    };

    return (
      rows as {
        constraint_name: string;
        table_schema: string;
        table_name: string;
        constraint_type: string;
        columns: string[];
        ref_schema: string | null;
        ref_table: string | null;
        ref_columns: string[] | null;
      }[]
    ).map((r) => ({
      name: r.constraint_name,
      table: `${r.table_schema}.${r.table_name}`,
      schema: r.table_schema,
      type: typeMap[r.constraint_type] ?? "check",
      columns: r.columns,
      ...(r.constraint_type === "FOREIGN KEY" && r.ref_table
        ? {
            referencedTable: `${r.ref_schema}.${r.ref_table}`,
            referencedColumns: r.ref_columns ?? [],
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
      SELECT table_schema, table_name
      FROM information_schema.tables
      WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
        AND table_name ILIKE $1
      ORDER BY table_schema, table_name
    `,
      [likePattern],
    );

    const tables = (tableRows as { table_schema: string; table_name: string }[]).map((r) => ({
      name: `${r.table_schema}.${r.table_name}`,
      schema: r.table_schema,
    }));

    const colRows = await db.unsafe(
      `
      SELECT table_schema, table_name, column_name
      FROM information_schema.columns
      WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
        AND column_name ILIKE $1
      ORDER BY table_schema, table_name, column_name
    `,
      [likePattern],
    );

    const columns = (
      colRows as { table_schema: string; table_name: string; column_name: string }[]
    ).map((r) => ({
      table: `${r.table_schema}.${r.table_name}`,
      column: r.column_name,
    }));

    return { tables, columns };
  };

  const close = async (): Promise<void> => {
    await db.close();
  };

  return {
    query,
    getTables,
    describeTable,
    getIndexes,
    getConstraints,
    searchSchema,
    close,
  };
};
