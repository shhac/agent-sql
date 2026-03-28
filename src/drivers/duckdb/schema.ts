import type {
  TableInfo,
  ColumnInfo,
  IndexInfo,
  ConstraintInfo,
} from "../types";
import { execDuckDbJson } from "./subprocess";

type SchemaOpts = {
  dbPath: string | undefined;
  readonly: boolean;
};

const runQuery = async (
  opts: SchemaOpts,
  sql: string,
): Promise<Record<string, unknown>[]> =>
  execDuckDbJson({ dbPath: opts.dbPath, sql, readonly: opts.readonly });

export const createDuckDbSchema = (opts: SchemaOpts) => {
  const getTables = async (
    tableOpts?: { includeSystem?: boolean },
  ): Promise<TableInfo[]> => {
    const rows = await runQuery(
      opts,
      tableOpts?.includeSystem
        ? "SELECT table_name, table_type FROM information_schema.tables ORDER BY table_name"
        : "SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name",
    );
    return rows.map((r) => ({
      name: r.table_name as string,
      type:
        (r.table_type as string) === "VIEW"
          ? ("view" as const)
          : ("table" as const),
    }));
  };

  const describeTable = async (table: string): Promise<ColumnInfo[]> => {
    const escaped = table.replace(/'/g, "''");
    const rows = await runQuery(
      opts,
      `SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = 'main' AND table_name = '${escaped}' ORDER BY ordinal_position`,
    );
    return rows.map((r) => ({
      name: r.column_name as string,
      type: r.data_type as string,
      nullable: (r.is_nullable as string) === "YES",
      defaultValue: (r.column_default as string) ?? undefined,
    }));
  };

  const getIndexes = async (table?: string): Promise<IndexInfo[]> => {
    const where = table
      ? ` WHERE table_name = '${table.replace(/'/g, "''")}'`
      : "";
    const rows = await runQuery(
      opts,
      `SELECT index_name, table_name, is_unique, expressions FROM duckdb_indexes()${where} ORDER BY index_name`,
    );

    return rows.map((r) => ({
      name: r.index_name as string,
      table: r.table_name as string,
      columns: parseExpressionList(r.expressions as string),
      unique: (r.is_unique as string) === "true",
    }));
  };

  const getConstraints = async (
    table?: string,
  ): Promise<ConstraintInfo[]> => {
    const where = table
      ? ` WHERE table_name = '${table.replace(/'/g, "''")}'`
      : "";
    const rows = await runQuery(
      opts,
      `SELECT constraint_type, table_name, constraint_column_names, constraint_column_indexes FROM duckdb_constraints()${where} ORDER BY table_name`,
    );

    return rows
      .filter((r) => mapConstraintType(r.constraint_type as string))
      .map((r) => ({
        name: `${r.table_name}_${mapConstraintType(r.constraint_type as string)}`,
        table: r.table_name as string,
        type: mapConstraintType(r.constraint_type as string)!,
        columns: parseColumnList(r.constraint_column_names),
      }));
  };

  const searchSchema = async (
    pattern: string,
  ): Promise<{
    tables: TableInfo[];
    columns: { table: string; column: string }[];
  }> => {
    const escaped = pattern.replace(/'/g, "''");
    const [tableRows, colRows] = await Promise.all([
      runQuery(
        opts,
        `SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' AND table_name LIKE '%${escaped}%' ORDER BY table_name`,
      ),
      runQuery(
        opts,
        `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = 'main' AND column_name LIKE '%${escaped}%' ORDER BY table_name, column_name`,
      ),
    ]);

    return {
      tables: tableRows.map((r) => ({ name: r.table_name as string })),
      columns: colRows.map((r) => ({
        table: r.table_name as string,
        column: r.column_name as string,
      })),
    };
  };

  return { getTables, describeTable, getIndexes, getConstraints, searchSchema };
};

const mapConstraintType = (
  duckType: string,
): "primary_key" | "foreign_key" | "unique" | "check" | undefined => {
  const map: Record<string, "primary_key" | "foreign_key" | "unique" | "check"> = {
    "PRIMARY KEY": "primary_key",
    "FOREIGN KEY": "foreign_key",
    UNIQUE: "unique",
    CHECK: "check",
  };
  return map[duckType];
};

const parseColumnList = (value: unknown): string[] => {
  if (Array.isArray(value)) {
    return value.map(String);
  }
  if (typeof value === "string") {
    return value.replace(/^\[|\]$/g, "").split(",").map((s) => s.trim());
  }
  return [];
};

// DuckDB expressions field format: "[col1, col2]"
const parseExpressionList = (expr: string): string[] => {
  if (!expr) {
    return [];
  }
  return expr
    .replace(/^\[|\]$/g, "")
    .split(",")
    .map((c) => c.trim())
    .filter(Boolean);
};
