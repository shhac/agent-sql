// Snowflake driver: REST API v2 with PAT authentication
// Read-only by default via client-side keyword allowlist (no BEGIN TRANSACTION READ ONLY support)

import { detectCommand, type DriverConnection, type QueryResult } from "./types";
import type { SnowflakeOpts, SnowflakeQueryResponse } from "./snowflake/types";
import { buildPatHeaders, buildBaseUrl } from "./snowflake/auth";
import { SnowflakeClient } from "./snowflake/client";
import { parseRows, extractColumns } from "./snowflake/parse-results";
import { validateReadOnly } from "./snowflake/read-only-guard";
import { quoteIdentPg } from "../lib/quote-ident";
import { getTimeout } from "../lib/timeout";
import { createSnowflakeSchema } from "./snowflake/schema";

const WRITE_COMMANDS: ReadonlySet<string> = new Set([
  "INSERT",
  "UPDATE",
  "DELETE",
  "CREATE",
  "ALTER",
  "DROP",
  "TRUNCATE",
  "MERGE",
  "COPY",
  "PUT",
  "GET",
]);

const responseToResult = (resp: SnowflakeQueryResponse): QueryResult => {
  const { rowType } = resp.resultSetMetaData;
  return {
    columns: extractColumns(rowType),
    rows: parseRows(resp.data, rowType),
  };
};

export const connectSnowflake = async (opts: SnowflakeOpts): Promise<DriverConnection> => {
  const readonly = opts.readonly ?? true;
  const defaultSchema = opts.schema ?? "PUBLIC";
  const timeoutSeconds = Math.ceil(getTimeout() / 1000);

  const client = new SnowflakeClient({
    baseUrl: buildBaseUrl(opts.account),
    authHeaders: buildPatHeaders(opts.token),
    timeoutMs: getTimeout(),
  });

  const execSql = async (
    sql: string,
    bindings?: Record<string, { type: string; value: string }>,
  ): Promise<SnowflakeQueryResponse> =>
    client.executeStatement({
      statement: sql,
      timeout: timeoutSeconds,
      database: opts.database,
      schema: opts.schema,
      warehouse: opts.warehouse,
      role: opts.role,
      bindings,
    });

  // Verify connectivity
  await execSql("SELECT 1");

  const query = async (userSql: string, queryOpts?: { write?: boolean }): Promise<QueryResult> => {
    if (readonly) {
      validateReadOnly(userSql);
    }

    const command = detectCommand(userSql, WRITE_COMMANDS);
    if (command && queryOpts?.write) {
      const resp = await execSql(userSql);
      return {
        columns: [],
        rows: [],
        rowsAffected: resp.resultSetMetaData.numRows,
        command,
      };
    }

    const resp = await execSql(userSql);
    return responseToResult(resp);
  };

  const schema = createSnowflakeSchema({ execSql, defaultSchema });

  const close = async (): Promise<void> => {
    // REST API is stateless — no connection to close
  };

  return {
    quoteIdent: quoteIdentPg,
    query,
    ...schema,
    close,
  };
};
