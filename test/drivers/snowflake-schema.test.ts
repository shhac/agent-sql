import { describe, test, expect, mock, beforeEach, afterEach, afterAll } from "bun:test";
import { connectSnowflake } from "../../src/drivers/snowflake";
import { configureTimeout } from "../../src/lib/timeout";
import type { SnowflakeQueryResponse } from "../../src/drivers/snowflake/types";

let requests: { url: string; body: { statement: string } | null }[] = [];
let sqlResponder: (sql: string) => SnowflakeQueryResponse;

const makeQueryResponse = (
  columns: { name: string; type: string }[],
  data: (string | null)[][],
): SnowflakeQueryResponse => ({
  code: "090001",
  statementHandle: "test-handle",
  sqlState: "00000",
  message: "Statement executed successfully.",
  resultSetMetaData: {
    numRows: data.length,
    format: "jsonv2",
    partitionInfo: [{ rowCount: data.length, uncompressedSize: 100 }],
    rowType: columns.map((c) => ({ name: c.name, type: c.type, nullable: true })),
  },
  data,
  statementStatusUrl: "/api/v2/statements/test-handle",
  createdOn: Date.now(),
});

const SELECT_1 = makeQueryResponse([{ name: "1", type: "fixed" }], [["1"]]);
const originalFetch = globalThis.fetch;

const installFetchMock = () => {
  globalThis.fetch = mock(async (_input: unknown, init?: unknown) => {
    const url = typeof _input === "string" ? _input : String(_input);
    const reqInit = init as RequestInit | undefined;
    const body = reqInit?.body ? (JSON.parse(reqInit.body as string) as { statement: string }) : null;
    requests.push({ url, body });
    return new Response(JSON.stringify(sqlResponder(body?.statement ?? "")), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as unknown as typeof fetch;
};

const opts = {
  account: "test-account",
  database: "TEST_DB",
  schema: "PUBLIC",
  warehouse: "TEST_WH",
  token: "pat-test-token",
  readonly: true,
};

beforeEach(() => {
  requests = [];
  sqlResponder = () => SELECT_1;
  configureTimeout(5000);
  installFetchMock();
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

afterAll(() => {
  globalThis.fetch = originalFetch;
  configureTimeout(undefined);
});

describe("Snowflake schema discovery", () => {
  test("describeTable returns columns with primary keys", async () => {
    sqlResponder = (sql) => {
      if (sql.includes("INFORMATION_SCHEMA.COLUMNS")) {
        return makeQueryResponse(
          [
            { name: "COLUMN_NAME", type: "text" },
            { name: "DATA_TYPE", type: "text" },
            { name: "IS_NULLABLE", type: "text" },
            { name: "COLUMN_DEFAULT", type: "text" },
            { name: "ORDINAL_POSITION", type: "fixed" },
          ],
          [
            ["ID", "NUMBER", "NO", null, "1"],
            ["NAME", "VARCHAR", "YES", null, "2"],
            ["EMAIL", "VARCHAR", "YES", "'unknown'", "3"],
          ],
        );
      }
      if (sql.includes("SHOW PRIMARY KEYS")) {
        return makeQueryResponse(
          [
            { name: "column_name", type: "text" },
            { name: "key_sequence", type: "fixed" },
          ],
          [["ID", "1"]],
        );
      }
      return SELECT_1;
    };

    const conn = await connectSnowflake(opts);
    const columns = await conn.describeTable("USERS");

    expect(columns).toEqual([
      { name: "ID", type: "NUMBER", nullable: false, defaultValue: undefined, primaryKey: true },
      { name: "NAME", type: "VARCHAR", nullable: true, defaultValue: undefined, primaryKey: false },
      {
        name: "EMAIL",
        type: "VARCHAR",
        nullable: true,
        defaultValue: "'unknown'",
        primaryKey: false,
      },
    ]);
  });

  test("describeTable with dot notation parses schema.table", async () => {
    sqlResponder = (sql) => {
      if (sql.includes("INFORMATION_SCHEMA.COLUMNS")) {
        expect(sql).toContain("MYSCHEMA");
        expect(sql).toContain("MYTABLE");
        return makeQueryResponse(
          [
            { name: "COLUMN_NAME", type: "text" },
            { name: "DATA_TYPE", type: "text" },
            { name: "IS_NULLABLE", type: "text" },
            { name: "COLUMN_DEFAULT", type: "text" },
            { name: "ORDINAL_POSITION", type: "fixed" },
          ],
          [["COL1", "TEXT", "YES", null, "1"]],
        );
      }
      if (sql.includes("SHOW PRIMARY KEYS")) {
        return makeQueryResponse([{ name: "column_name", type: "text" }], []);
      }
      return SELECT_1;
    };

    const conn = await connectSnowflake(opts);
    const columns = await conn.describeTable("MYSCHEMA.MYTABLE");
    expect(columns).toHaveLength(1);
    expect(columns[0]!.name).toBe("COL1");
  });

  test("getConstraints returns PK, FK, and unique constraints", async () => {
    sqlResponder = (sql) => {
      if (sql.startsWith("SHOW PRIMARY KEYS")) {
        return makeQueryResponse(
          [
            { name: "constraint_name", type: "text" },
            { name: "table_name", type: "text" },
            { name: "schema_name", type: "text" },
            { name: "column_name", type: "text" },
            { name: "key_sequence", type: "fixed" },
          ],
          [["PK_USERS", "USERS", "PUBLIC", "ID", "1"]],
        );
      }
      if (sql.startsWith("SHOW IMPORTED KEYS")) {
        return makeQueryResponse(
          [
            { name: "fk_constraint_name", type: "text" },
            { name: "fk_table_name", type: "text" },
            { name: "fk_schema_name", type: "text" },
            { name: "fk_column_name", type: "text" },
            { name: "pk_table_name", type: "text" },
            { name: "pk_column_name", type: "text" },
            { name: "key_sequence", type: "fixed" },
          ],
          [["FK_ORDERS_USER", "ORDERS", "PUBLIC", "USER_ID", "USERS", "ID", "1"]],
        );
      }
      if (sql.startsWith("SHOW UNIQUE KEYS")) {
        return makeQueryResponse(
          [
            { name: "constraint_name", type: "text" },
            { name: "table_name", type: "text" },
            { name: "schema_name", type: "text" },
            { name: "column_name", type: "text" },
            { name: "key_sequence", type: "fixed" },
          ],
          [["UQ_EMAIL", "USERS", "PUBLIC", "EMAIL", "1"]],
        );
      }
      return SELECT_1;
    };

    const conn = await connectSnowflake(opts);
    const constraints = await conn.getConstraints("USERS");
    expect(constraints).toHaveLength(3);

    const pk = constraints.find((c) => c.type === "primary_key");
    expect(pk).toEqual({
      name: "PK_USERS",
      table: "USERS",
      schema: "PUBLIC",
      type: "primary_key",
      columns: ["ID"],
    });

    const fk = constraints.find((c) => c.type === "foreign_key");
    expect(fk).toEqual({
      name: "FK_ORDERS_USER",
      table: "ORDERS",
      schema: "PUBLIC",
      type: "foreign_key",
      columns: ["USER_ID"],
      referencedTable: "USERS",
      referencedColumns: ["ID"],
    });

    const uq = constraints.find((c) => c.type === "unique");
    expect(uq).toEqual({
      name: "UQ_EMAIL",
      table: "USERS",
      schema: "PUBLIC",
      type: "unique",
      columns: ["EMAIL"],
    });
  });

  test("searchSchema returns matching tables and columns", async () => {
    sqlResponder = (sql) => {
      if (sql.includes("INFORMATION_SCHEMA.TABLES") && sql.includes("ILIKE")) {
        return makeQueryResponse(
          [
            { name: "TABLE_SCHEMA", type: "text" },
            { name: "TABLE_NAME", type: "text" },
          ],
          [["PUBLIC", "USERS"]],
        );
      }
      if (sql.includes("INFORMATION_SCHEMA.COLUMNS") && sql.includes("ILIKE")) {
        return makeQueryResponse(
          [
            { name: "TABLE_SCHEMA", type: "text" },
            { name: "TABLE_NAME", type: "text" },
            { name: "COLUMN_NAME", type: "text" },
          ],
          [
            ["PUBLIC", "USERS", "USER_ID"],
            ["PUBLIC", "ORDERS", "USER_ID"],
          ],
        );
      }
      return SELECT_1;
    };

    const conn = await connectSnowflake(opts);
    const result = await conn.searchSchema("user");
    expect(result.tables).toEqual([{ name: "PUBLIC.USERS", schema: "PUBLIC" }]);
    expect(result.columns).toEqual([
      { table: "PUBLIC.USERS", column: "USER_ID" },
      { table: "PUBLIC.ORDERS", column: "USER_ID" },
    ]);
  });
});
