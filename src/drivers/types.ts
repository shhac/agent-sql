export type Driver = "pg" | "sqlite" | "mysql";

export type QueryResult = {
  columns: string[];
  rows: Record<string, unknown>[];
  rowsAffected?: number;
  command?: string;
};

export type TableInfo = {
  name: string;
  schema?: string;
  rowCount?: number;
};

export type ColumnInfo = {
  name: string;
  type: string;
  nullable: boolean;
  defaultValue?: string;
  primaryKey?: boolean;
};

export type IndexInfo = {
  name: string;
  table: string;
  schema?: string;
  columns: string[];
  unique: boolean;
};

export type ConstraintInfo = {
  name: string;
  table: string;
  schema?: string;
  type: "primary_key" | "foreign_key" | "unique" | "check";
  columns: string[];
  referencedTable?: string;
  referencedColumns?: string[];
  definition?: string;
};

export const detectCommand = (
  sql: string,
  commands: ReadonlySet<string>,
): string | undefined => {
  const trimmed = sql.trimStart().toUpperCase();
  for (const cmd of commands) {
    if (trimmed.startsWith(cmd)) {
      return cmd;
    }
  }
  return undefined;
};

export type DriverConnection = {
  query(sql: string, opts?: { write?: boolean }): Promise<QueryResult>;
  getTables(opts?: { includeSystem?: boolean }): Promise<TableInfo[]>;
  describeTable(table: string): Promise<ColumnInfo[]>;
  getIndexes(table?: string): Promise<IndexInfo[]>;
  getConstraints(table?: string): Promise<ConstraintInfo[]>;
  searchSchema(pattern: string): Promise<{
    tables: TableInfo[];
    columns: { table: string; column: string }[];
  }>;
  close(): Promise<void>;
};
