import { describe, test, expect, mock, beforeEach, afterEach, afterAll } from "bun:test";
import { connectSnowflake } from "../../src/drivers/snowflake";
import { configureTimeout } from "../../src/lib/timeout";
import type { SnowflakeQueryResponse } from "../../src/drivers/snowflake/types";

// Track all fetch calls for assertions
let requests: { url: string; body: SnowflakeStatementBody | null }[] = [];
let sqlResponder: (sql: string) => SnowflakeQueryResponse;

type SnowflakeStatementBody = {
  statement: string;
  timeout?: number;
  database?: string;
  schema?: string;
  warehouse?: string;
  role?: string;
  parameters?: Record<string, string>;
};

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
    rowType: columns.map((c) => ({
      name: c.name,
      type: c.type,
      nullable: true,
    })),
  },
  data,
  statementStatusUrl: "/api/v2/statements/test-handle",
  createdOn: Date.now(),
});

const SELECT_1_RESPONSE = makeQueryResponse([{ name: "1", type: "fixed" }], [["1"]]);

const originalFetch = globalThis.fetch;

const installFetchMock = () => {
  globalThis.fetch = mock(async (input: unknown, init?: unknown) => {
    const url = typeof input === "string" ? input : String(input);
    const reqInit = init as RequestInit | undefined;
    const body = reqInit?.body ? (JSON.parse(reqInit.body as string) as SnowflakeStatementBody) : null;
    requests.push({ url, body });

    const sql = body?.statement ?? "";
    const responseData = sqlResponder(sql);

    return new Response(JSON.stringify(responseData), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as unknown as typeof fetch;
};

const defaultResponder = (_sql: string): SnowflakeQueryResponse => SELECT_1_RESPONSE;

beforeEach(() => {
  requests = [];
  sqlResponder = defaultResponder;
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

const defaultOpts = {
  account: "test-account",
  database: "TEST_DB",
  schema: "PUBLIC",
  warehouse: "TEST_WH",
  token: "pat-test-token",
  readonly: true,
};

describe("Snowflake driver", () => {
  test("connection calls SELECT 1 and returns DriverConnection", async () => {
    const conn = await connectSnowflake(defaultOpts);
    expect(requests).toHaveLength(1);
    expect(requests[0]!.body?.statement).toBe("SELECT 1");
    expect(conn.query).toBeFunction();
    expect(conn.getTables).toBeFunction();
    expect(conn.describeTable).toBeFunction();
    expect(conn.getIndexes).toBeFunction();
    expect(conn.getConstraints).toBeFunction();
    expect(conn.searchSchema).toBeFunction();
    expect(conn.close).toBeFunction();
    expect(conn.quoteIdent).toBeFunction();
  });

  test("query (read-only) executes SELECT and returns typed results", async () => {
    sqlResponder = (sql) => {
      if (sql === "SELECT 1") {
        return SELECT_1_RESPONSE;
      }
      return makeQueryResponse(
        [
          { name: "ID", type: "fixed" },
          { name: "NAME", type: "text" },
        ],
        [
          ["1", "Alice"],
          ["2", "Bob"],
        ],
      );
    };

    const conn = await connectSnowflake(defaultOpts);
    const result = await conn.query("SELECT * FROM users");
    expect(result.columns).toEqual(["ID", "NAME"]);
    expect(result.rows).toEqual([
      { ID: 1, NAME: "Alice" },
      { ID: 2, NAME: "Bob" },
    ]);
  });

  test("query (read-only) blocks writes with fixableBy hint", async () => {
    const conn = await connectSnowflake(defaultOpts);
    try {
      await conn.query("INSERT INTO users (name) VALUES ('Mallory')");
      expect.unreachable("should have thrown");
    } catch (err: unknown) {
      const error = err as Error & { fixableBy?: string };
      expect(error.message).toContain("not allowed in read-only mode");
      expect(error.fixableBy).toBe("human");
    }
  });

  test("query (write mode) returns rowsAffected for write commands", async () => {
    sqlResponder = (sql) => {
      if (sql === "SELECT 1") {
        return SELECT_1_RESPONSE;
      }
      return makeQueryResponse([], []);
    };

    const conn = await connectSnowflake({ ...defaultOpts, readonly: false });
    const result = await conn.query("INSERT INTO users (name) VALUES ('Charlie')", { write: true });
    expect(result.rowsAffected).toBe(0);
    expect(result.command).toBe("INSERT");
  });

  test("getTables returns schema.table format", async () => {
    sqlResponder = (sql) => {
      if (sql === "SELECT 1") {
        return SELECT_1_RESPONSE;
      }
      return makeQueryResponse(
        [
          { name: "TABLE_SCHEMA", type: "text" },
          { name: "TABLE_NAME", type: "text" },
          { name: "TABLE_TYPE", type: "text" },
        ],
        [
          ["PUBLIC", "USERS", "BASE TABLE"],
          ["PUBLIC", "ORDERS", "VIEW"],
        ],
      );
    };

    const conn = await connectSnowflake(defaultOpts);
    const tables = await conn.getTables();
    expect(tables).toEqual([
      { name: "PUBLIC.USERS", schema: "PUBLIC", type: "table" },
      { name: "PUBLIC.ORDERS", schema: "PUBLIC", type: "view" },
    ]);
  });

  test("getIndexes returns empty array", async () => {
    const conn = await connectSnowflake(defaultOpts);
    expect(await conn.getIndexes("USERS")).toEqual([]);
  });

  test("close is a no-op", async () => {
    const conn = await connectSnowflake(defaultOpts);
    await expect(conn.close()).resolves.toBeUndefined();
  });

  test("quoteIdent uses PG-style double quoting", async () => {
    const conn = await connectSnowflake(defaultOpts);
    expect(conn.quoteIdent("users")).toBe('"users"');
    expect(conn.quoteIdent("PUBLIC.USERS")).toBe('"PUBLIC"."USERS"');
  });
});
