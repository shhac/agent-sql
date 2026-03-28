// MySQL driver: per-query START TRANSACTION READ ONLY wrapping
// + protocol-level single-statement enforcement (COM_QUERY). No parser needed.

import { SQL } from "bun";
import { detectCommand, type DriverConnection, type QueryResult } from "../types";
import { quoteIdentMysql } from "../../lib/quote-ident";
import { getTimeout } from "../../lib/timeout";
import { withConnectTimeout } from "../connect-timeout";
import { createMysqlSchema } from "./schema";

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
      const result = rows as unknown as { count?: number; affectedRows?: number };
      return {
        columns: [],
        rows: [],
        rowsAffected: result.affectedRows ?? result.count ?? 0,
        command,
      };
    }

    const rows = await db.unsafe(userSql);
    const columns = rows.length > 0 ? Object.keys(rows[0] as Record<string, unknown>) : [];
    return { columns, rows: rows as Record<string, unknown>[] };
  };

  const quoteIdent = quoteIdentMysql;

  const schema = createMysqlSchema(db);

  const close = async (): Promise<void> => {
    await db.close();
  };

  return {
    quoteIdent,
    query,
    ...schema,
    close,
  };
};
