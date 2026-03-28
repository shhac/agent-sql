import { SQL } from "bun";
import { detectCommand, type DriverConnection, type QueryResult } from "../types";
import { loadPgParser, validateReadOnlyQuery } from "../../lib/pg-session-guard";
import { quoteIdentPg } from "../../lib/quote-ident";
import { getTimeout } from "../../lib/timeout";
import { withConnectTimeout } from "../connect-timeout";
import { createPgSchema } from "./schema";

type PgOpts = {
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
  "MERGE",
  "COPY",
]);

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

  const quoteIdent = quoteIdentPg;

  const schema = createPgSchema(db);

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
