import type { TableInfo, ColumnInfo, IndexInfo, ConstraintInfo } from "../types";
import type { SnowflakeQueryResponse } from "./types";
import { extractColumns, parseRows } from "./parse-results";
import { quoteIdentPg } from "../../lib/quote-ident";
import { parseTableRef } from "../table-ref";

type ExecSql = (
  sql: string,
  bindings?: Record<string, { type: string; value: string }>,
) => Promise<SnowflakeQueryResponse>;

const responseToResult = (resp: SnowflakeQueryResponse) => {
  const { rowType } = resp.resultSetMetaData;
  return {
    columns: extractColumns(rowType),
    rows: parseRows(resp.data, rowType),
  };
};

// Snowflake SHOW command results may return column names in varying case
const getField = (row: Record<string, unknown>, name: string): string => {
  const val = row[name] ?? row[name.toUpperCase()] ?? row[name.toLowerCase()];
  return String(val ?? "");
};

const sortByKeySequence = (rows: Record<string, unknown>[]): Record<string, unknown>[] =>
  [...rows].sort(
    (a, b) => Number(getField(a, "key_sequence")) - Number(getField(b, "key_sequence")),
  );

const groupByConstraint = (
  rows: Record<string, unknown>[],
): Map<string, Record<string, unknown>[]> => groupByField(rows, "constraint_name");

const groupByField = (
  rows: Record<string, unknown>[],
  field: string,
): Map<string, Record<string, unknown>[]> => {
  const groups = new Map<string, Record<string, unknown>[]>();
  for (const row of rows) {
    const key = getField(row, field);
    const existing = groups.get(key);
    if (existing) {
      existing.push(row);
    } else {
      groups.set(key, [row]);
    }
  }
  return groups;
};

export const createSnowflakeSchema = (opts: { execSql: ExecSql; defaultSchema: string }) => {
  const { execSql, defaultSchema } = opts;

  const getTables = async (tableOpts?: { includeSystem?: boolean }): Promise<TableInfo[]> => {
    const systemFilter = tableOpts?.includeSystem
      ? ""
      : "AND TABLE_SCHEMA NOT IN ('INFORMATION_SCHEMA')";
    const resp = await execSql(`
			SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE
			FROM INFORMATION_SCHEMA.TABLES
			WHERE TABLE_CATALOG = CURRENT_DATABASE()
				${systemFilter}
			ORDER BY TABLE_SCHEMA, TABLE_NAME
		`);
    const result = responseToResult(resp);
    return result.rows.map((r) => ({
      name: `${r.TABLE_SCHEMA as string}.${r.TABLE_NAME as string}`,
      schema: r.TABLE_SCHEMA as string,
      type: (r.TABLE_TYPE as string) === "VIEW" ? ("view" as const) : ("table" as const),
    }));
  };

  const describeTable = async (table: string): Promise<ColumnInfo[]> => {
    const ref = parseTableRef(table, defaultSchema);
    const resp = await execSql(
      `
			SELECT
				COLUMN_NAME,
				DATA_TYPE,
				IS_NULLABLE,
				COLUMN_DEFAULT,
				ORDINAL_POSITION
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_CATALOG = CURRENT_DATABASE()
				AND TABLE_SCHEMA = ?
				AND TABLE_NAME = ?
			ORDER BY ORDINAL_POSITION
		`,
      { "1": { type: "TEXT", value: ref.schema }, "2": { type: "TEXT", value: ref.table } },
    );
    const result = responseToResult(resp);

    // Fetch primary key columns to mark them
    const pkCols = await getPrimaryKeyCols(ref.schema, ref.table);

    return result.rows.map((r) => ({
      name: r.COLUMN_NAME as string,
      type: r.DATA_TYPE as string,
      nullable: (r.IS_NULLABLE as string) === "YES",
      defaultValue: r.COLUMN_DEFAULT != null ? String(r.COLUMN_DEFAULT) : undefined,
      primaryKey: pkCols.has(r.COLUMN_NAME as string),
    }));
  };

  const getPrimaryKeyCols = async (schema: string, table: string): Promise<Set<string>> => {
    try {
      const resp = await execSql(`SHOW PRIMARY KEYS IN ${quoteIdentPg(`${schema}.${table}`)}`);
      const result = responseToResult(resp);
      return new Set(result.rows.map((r) => (r.column_name ?? r.COLUMN_NAME) as string));
    } catch {
      return new Set();
    }
  };

  const getIndexes = async (_table?: string): Promise<IndexInfo[]> => [];

  const getConstraints = async (table?: string): Promise<ConstraintInfo[]> => {
    const constraints: ConstraintInfo[] = [];

    const ref = table ? parseTableRef(table, defaultSchema) : undefined;
    const inClause = ref
      ? ` IN ${quoteIdentPg(`${ref.schema}.${ref.table}`)}`
      : ` IN SCHEMA ${quoteIdentPg(defaultSchema)}`;

    // Primary keys
    try {
      const pkResp = await execSql(`SHOW PRIMARY KEYS${inClause}`);
      const pkResult = responseToResult(pkResp);
      const pkGroups = groupByConstraint(pkResult.rows);
      for (const [, rows] of pkGroups) {
        const first = rows[0]!;
        constraints.push({
          name: getField(first, "constraint_name"),
          table: getField(first, "table_name"),
          schema: getField(first, "schema_name"),
          type: "primary_key",
          columns: sortByKeySequence(rows).map((r) => getField(r, "column_name")),
        });
      }
    } catch {
      // SHOW commands may fail if permissions are insufficient
    }

    // Foreign keys
    try {
      const fkResp = await execSql(`SHOW IMPORTED KEYS${inClause}`);
      const fkResult = responseToResult(fkResp);
      const fkGroups = groupByField(fkResult.rows, "fk_constraint_name");
      for (const [, rows] of fkGroups) {
        const first = rows[0]!;
        const sorted = sortByKeySequence(rows);
        constraints.push({
          name: getField(first, "fk_constraint_name"),
          table: getField(first, "fk_table_name"),
          schema: getField(first, "fk_schema_name"),
          type: "foreign_key",
          columns: sorted.map((r) => getField(r, "fk_column_name")),
          referencedTable: getField(first, "pk_table_name"),
          referencedColumns: sorted.map((r) => getField(r, "pk_column_name")),
        });
      }
    } catch {
      // Permissions may be insufficient
    }

    // Unique keys
    try {
      const ukResp = await execSql(`SHOW UNIQUE KEYS${inClause}`);
      const ukResult = responseToResult(ukResp);
      const ukGroups = groupByConstraint(ukResult.rows);
      for (const [, rows] of ukGroups) {
        const first = rows[0]!;
        constraints.push({
          name: getField(first, "constraint_name"),
          table: getField(first, "table_name"),
          schema: getField(first, "schema_name"),
          type: "unique",
          columns: sortByKeySequence(rows).map((r) => getField(r, "column_name")),
        });
      }
    } catch {
      // Permissions may be insufficient
    }

    return constraints;
  };

  const searchSchema = async (
    pattern: string,
  ): Promise<{
    tables: TableInfo[];
    columns: { table: string; column: string }[];
  }> => {
    const ilike = `%${pattern}%`;
    const ilikeBinding = { "1": { type: "TEXT", value: ilike } };

    const tableResp = await execSql(
      `
			SELECT TABLE_SCHEMA, TABLE_NAME
			FROM INFORMATION_SCHEMA.TABLES
			WHERE TABLE_CATALOG = CURRENT_DATABASE()
				AND TABLE_SCHEMA NOT IN ('INFORMATION_SCHEMA')
				AND TABLE_NAME ILIKE ?
			ORDER BY TABLE_SCHEMA, TABLE_NAME
		`,
      ilikeBinding,
    );
    const tableResult = responseToResult(tableResp);
    const tables = tableResult.rows.map((r) => ({
      name: `${r.TABLE_SCHEMA as string}.${r.TABLE_NAME as string}`,
      schema: r.TABLE_SCHEMA as string,
    }));

    const colResp = await execSql(
      `
			SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_CATALOG = CURRENT_DATABASE()
				AND TABLE_SCHEMA NOT IN ('INFORMATION_SCHEMA')
				AND COLUMN_NAME ILIKE ?
			ORDER BY TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME
		`,
      ilikeBinding,
    );
    const colResult = responseToResult(colResp);
    const columns = colResult.rows.map((r) => ({
      table: `${r.TABLE_SCHEMA as string}.${r.TABLE_NAME as string}`,
      column: r.COLUMN_NAME as string,
    }));

    return { tables, columns };
  };

  return { getTables, describeTable, getIndexes, getConstraints, searchSchema };
};
