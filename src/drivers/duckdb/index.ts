import { detectCommand, type DriverConnection, type QueryResult } from "../types";
import { checkDuckDbAvailable, execDuckDbJson } from "./subprocess";
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
]);

export const connectDuckDb = async (opts: DuckDbOpts): Promise<DriverConnection> => {
  const readonly = opts.readonly ?? true;
  const dbPath = opts.path;

  checkDuckDbAvailable();

  // Verify the database is accessible
  await execDuckDbJson({ dbPath, sql: "SELECT 1", readonly });

  const query = async (sql: string, queryOpts?: { write?: boolean }): Promise<QueryResult> => {
    const command = detectCommand(sql, WRITE_COMMANDS);

    if (command && queryOpts?.write) {
      const rows = await execDuckDbJson({ dbPath, sql, readonly: false });
      const [first] = rows;
      return {
        columns: [],
        rows: [],
        rowsAffected: first ? ((first.Count as number) ?? 0) : 0,
        command,
      };
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
