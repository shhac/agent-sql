import type { TableInfo, ColumnInfo, IndexInfo, ConstraintInfo } from "../types";
import { parseTableRef } from "../table-ref";

type PgDb = { unsafe: (sql: string, params?: unknown[]) => Promise<unknown[]> };

// Bun.SQL returns PG array_agg results as string literals like "{a,b}" instead of arrays
const parsePgArray = (value: unknown): string[] => {
  if (Array.isArray(value)) {
    return value as string[];
  }
  if (typeof value === "string" && value.startsWith("{") && value.endsWith("}")) {
    const inner = value.slice(1, -1);
    return inner.length === 0 ? [] : inner.split(",");
  }
  return [];
};

export const createPgSchema = (db: PgDb) => {
  const getTables = async (tableOpts?: { includeSystem?: boolean }): Promise<TableInfo[]> => {
    const filter = tableOpts?.includeSystem
      ? ""
      : "WHERE table_schema NOT IN ('pg_catalog', 'information_schema')";
    const rows = await db.unsafe(`
      SELECT table_schema, table_name, table_type
      FROM information_schema.tables
      ${filter}
      ORDER BY table_schema, table_name
    `);
    return (rows as { table_schema: string; table_name: string; table_type: string }[]).map(
      (r) => ({
        name: `${r.table_schema}.${r.table_name}`,
        schema: r.table_schema,
        type: r.table_type === "VIEW" ? ("view" as const) : ("table" as const),
      }),
    );
  };

  const describeTable = async (table: string): Promise<ColumnInfo[]> => {
    const ref = parseTableRef(table, "public");
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
    const ref = table ? parseTableRef(table, "public") : undefined;
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
      columns: parsePgArray(r.columns),
      unique: r.indisunique,
    }));
  };

  const getConstraints = async (table?: string): Promise<ConstraintInfo[]> => {
    const ref = table ? parseTableRef(table, "public") : undefined;
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
        AND ccu.constraint_schema = tc.constraint_schema
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
      columns: parsePgArray(r.columns),
      ...(r.constraint_type === "FOREIGN KEY" && r.ref_table
        ? {
            referencedTable: `${r.ref_schema}.${r.ref_table}`,
            referencedColumns: parsePgArray(r.ref_columns),
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

  return { getTables, describeTable, getIndexes, getConstraints, searchSchema };
};
