import { detectCommand, type DriverConnection, type QueryResult } from "../types";
import { execDuckDbJson } from "./subprocess";
import { createDuckDbSchema } from "./schema";
import { quoteIdentPg } from "../../lib/quote-ident";

type DuckDbOpts = {
  path?: string;
  readonly?: boolean;
};

const WRITE_COMMANDS: ReadonlySet<string> = new Set([
  "INSERT",
  "UPDATE",
  "DELETE",
  "CREATE",
  "DROP",
  "ALTER",
  "COPY",
  "TRUNCATE",
  "MERGE",
]);

export const connectDuckDb = async (opts: DuckDbOpts): Promise<DriverConnection> => {
  const readonly = opts.readonly ?? true;
  const dbPath = opts.path;

  // Verify CLI exists and database is accessible in one spawn
  await execDuckDbJson({ dbPath, sql: "SELECT 1", readonly });

  const query = async (sql: string, queryOpts?: { write?: boolean }): Promise<QueryResult> => {
    const command = detectCommand(sql, WRITE_COMMANDS);

    if (command && queryOpts?.write) {
      await execDuckDbJson({ dbPath, sql, readonly: false });
      // DuckDB jsonlines produces no output for write statements; rowsAffected unavailable
      return { columns: [], rows: [], rowsAffected: 0, command };
    }

    const rows = await execDuckDbJson({ dbPath, sql, readonly });
    const [first] = rows;
    const columns = first ? Object.keys(first) : [];
    return { columns, rows };
  };

  const schema = createDuckDbSchema({ dbPath, readonly });

  const close = async (): Promise<void> => {};

  return {
    quoteIdent: quoteIdentPg,
    query,
    ...schema,
    close,
  };
};
