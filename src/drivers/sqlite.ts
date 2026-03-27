import { Database } from "bun:sqlite";
import {
  detectCommand,
  type DriverConnection,
  type QueryResult,
  type TableInfo,
  type ColumnInfo,
  type IndexInfo,
  type ConstraintInfo,
} from "./types";

type SqliteOpts = {
  path: string;
  readonly?: boolean;
};

const WRITE_COMMANDS: ReadonlySet<string> = new Set(["INSERT", "UPDATE", "DELETE", "REPLACE"]);

export const connectSqlite = (opts: SqliteOpts): DriverConnection => {
  const readonly = opts.readonly ?? true;
  const db = readonly
    ? new Database(opts.path, { readonly: true })
    : new Database(opts.path, { readwrite: true, create: false });

  const query = async (sql: string, queryOpts?: { write?: boolean }): Promise<QueryResult> => {
    const command = detectCommand(sql, WRITE_COMMANDS);

    if (command && queryOpts?.write) {
      const stmt = db.query(sql);
      const runResult = stmt.run();
      return {
        columns: [],
        rows: [],
        rowsAffected: runResult.changes,
        command,
      };
    }

    const stmt = db.query(sql);
    const columns = stmt.columnNames;
    const rows = stmt.all() as Record<string, unknown>[];
    return { columns, rows };
  };

  const getTables = async (tableOpts?: { includeSystem?: boolean }): Promise<TableInfo[]> => {
    const rows = db
      .query("SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') ORDER BY name")
      .all() as { name: string; type: string }[];

    if (tableOpts?.includeSystem) {
      return rows.map((r) => ({
        name: r.name,
        type: r.type === "view" ? ("view" as const) : ("table" as const),
      }));
    }
    return rows
      .filter((r) => !r.name.startsWith("sqlite_"))
      .map((r) => ({
        name: r.name,
        type: r.type === "view" ? ("view" as const) : ("table" as const),
      }));
  };

  const describeTable = async (table: string): Promise<ColumnInfo[]> => {
    const rows = db.query(`PRAGMA table_info(${quoteIdent(table)})`).all() as PragmaTableInfo[];
    return rows.map((r) => ({
      name: r.name,
      type: r.type,
      nullable: r.notnull === 0 && r.pk === 0,
      defaultValue: r.dflt_value ?? undefined,
      primaryKey: r.pk > 0,
    }));
  };

  const getIndexes = async (table?: string): Promise<IndexInfo[]> => {
    const tables = table ? [table] : await getUserTableNames();
    const results: IndexInfo[] = [];

    for (const tbl of tables) {
      const indexRows = db
        .query(`PRAGMA index_list(${quoteIdent(tbl)})`)
        .all() as PragmaIndexList[];
      for (const idx of indexRows) {
        const colRows = db
          .query(`PRAGMA index_info(${quoteIdent(idx.name)})`)
          .all() as PragmaIndexInfo[];
        results.push({
          name: idx.name,
          table: tbl,
          columns: colRows.map((c) => c.name),
          unique: idx.unique === 1,
        });
      }
    }

    return results;
  };

  const getConstraints = async (table?: string): Promise<ConstraintInfo[]> => {
    const tables = table ? [table] : await getUserTableNames();
    const results: ConstraintInfo[] = [];

    for (const tbl of tables) {
      const cols = db.query(`PRAGMA table_info(${quoteIdent(tbl)})`).all() as PragmaTableInfo[];
      const pkCols = cols.filter((c) => c.pk > 0).sort((a, b) => a.pk - b.pk);
      if (pkCols.length > 0) {
        results.push({
          name: `${tbl}_pkey`,
          table: tbl,
          type: "primary_key",
          columns: pkCols.map((c) => c.name),
        });
      }

      const fks = db
        .query(`PRAGMA foreign_key_list(${quoteIdent(tbl)})`)
        .all() as PragmaForeignKey[];
      for (const fk of fks) {
        results.push({
          name: `${tbl}_${fk.from}_fkey`,
          table: tbl,
          type: "foreign_key",
          columns: [fk.from],
          referencedTable: fk.table,
          referencedColumns: [fk.to],
        });
      }

      const indexRows = db
        .query(`PRAGMA index_list(${quoteIdent(tbl)})`)
        .all() as PragmaIndexList[];
      for (const idx of indexRows) {
        if (!idx.unique || idx.origin === "pk") {
          continue;
        }
        const colRows = db
          .query(`PRAGMA index_info(${quoteIdent(idx.name)})`)
          .all() as PragmaIndexInfo[];
        results.push({
          name: idx.name,
          table: tbl,
          type: "unique",
          columns: colRows.map((c) => c.name),
        });
      }
    }

    return results;
  };

  const searchSchema = async (
    pattern: string,
  ): Promise<{ tables: TableInfo[]; columns: { table: string; column: string }[] }> => {
    const likePattern = `%${pattern}%`;

    const tableRows = db
      .query(
        "SELECT name FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' AND name LIKE ? COLLATE NOCASE",
      )
      .all(likePattern) as { name: string }[];

    const tables: TableInfo[] = tableRows.map((r) => ({ name: r.name }));
    const columns: { table: string; column: string }[] = [];

    const allTables = await getUserTableNames();
    for (const tbl of allTables) {
      const cols = db.query(`PRAGMA table_info(${quoteIdent(tbl)})`).all() as PragmaTableInfo[];
      for (const col of cols) {
        if (col.name.toLowerCase().includes(pattern.toLowerCase())) {
          columns.push({ table: tbl, column: col.name });
        }
      }
    }

    return { tables, columns };
  };

  const close = async (): Promise<void> => {
    db.close();
  };

  const getUserTableNames = async (): Promise<string[]> => {
    const rows = db
      .query(
        "SELECT name FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name",
      )
      .all() as { name: string }[];
    return rows.map((r) => r.name);
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

const quoteIdent = (name: string): string => `"${name.replace(/"/g, '""')}"`;

type PragmaTableInfo = {
  cid: number;
  name: string;
  type: string;
  notnull: number;
  dflt_value: string | null;
  pk: number;
};

type PragmaIndexList = {
  seq: number;
  name: string;
  unique: number;
  origin: string;
  partial: number;
};

type PragmaIndexInfo = {
  seqno: number;
  cid: number;
  name: string;
};

type PragmaForeignKey = {
  id: number;
  seq: number;
  table: string;
  from: string;
  to: string;
  on_update: string;
  on_delete: string;
  match: string;
};
