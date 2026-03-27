import { describe, test, expect, mock, beforeEach, afterAll } from "bun:test";
import { SnowflakeClient } from "../../src/drivers/snowflake/client";
import type { SnowflakeQueryResponse } from "../../src/drivers/snowflake/types";

const originalFetch = global.fetch;

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const setFetchMock = (fn: (...args: unknown[]) => Promise<Response>) => {
  global.fetch = fn as unknown as typeof fetch;
};

const makeQueryResponse = (
  overrides?: Partial<SnowflakeQueryResponse>,
): SnowflakeQueryResponse => ({
  code: "090001",
  statementHandle: "handle-123",
  sqlState: "00000",
  message: "Statement executed successfully.",
  resultSetMetaData: {
    numRows: 1,
    format: "jsonv2",
    partitionInfo: [{ rowCount: 1, uncompressedSize: 100 }],
    rowType: [{ name: "col1", type: "text", nullable: true }],
  },
  data: [["hello"]],
  statementStatusUrl: "/api/v2/statements/handle-123",
  createdOn: Date.now(),
  ...overrides,
});

const jsonResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

const createClient = (opts?: { timeoutMs?: number }): SnowflakeClient =>
  new SnowflakeClient({
    baseUrl: "https://test.snowflakecomputing.com",
    authHeaders: {
      Authorization: "Bearer test-token",
      "X-Snowflake-Authorization-Token-Type": "PROGRAMMATIC_ACCESS_TOKEN",
    },
    timeoutMs: opts?.timeoutMs,
  });

describe("SnowflakeClient", () => {
  beforeEach(() => {
    global.fetch = originalFetch;
  });

  afterAll(() => {
    global.fetch = originalFetch;
  });

  describe("executeStatement", () => {
    test("sends correct POST request", async () => {
      const mockFetch = mock(() => Promise.resolve(jsonResponse(makeQueryResponse())));
      setFetchMock(mockFetch);

      const client = createClient();
      await client.executeStatement({ statement: "SELECT 1" });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0] as unknown as [string, RequestInit];
      expect(call[0]).toBe("https://test.snowflakecomputing.com/api/v2/statements");
      expect(call[1].method).toBe("POST");

      const headers = call[1].headers as Record<string, string>;
      expect(headers["Authorization"]).toBe("Bearer test-token");
      expect(headers["Content-Type"]).toBe("application/json");

      const body = JSON.parse(call[1].body as string);
      expect(body.statement).toBe("SELECT 1");
      expect(body.parameters.MULTI_STATEMENT_COUNT).toBe("1");
    });

    test("returns parsed response for sync results", async () => {
      const expected = makeQueryResponse();
      setFetchMock(mock(() => Promise.resolve(jsonResponse(expected))));

      const client = createClient();
      const result = await client.executeStatement({ statement: "SELECT 1" });

      expect(result.data).toEqual([["hello"]]);
      expect(result.statementHandle).toBe("handle-123");
      expect(result.resultSetMetaData.rowType).toHaveLength(1);
    });

    test("polls on async response (code 333334)", async () => {
      const asyncResp = {
        code: "333334",
        message: "Async execution in progress",
        statementHandle: "async-handle",
        statementStatusUrl: "/api/v2/statements/async-handle",
      };
      const finalResp = makeQueryResponse({ statementHandle: "async-handle" });

      let callCount = 0;
      setFetchMock(mock(() => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(jsonResponse(asyncResp, 202));
        }
        return Promise.resolve(jsonResponse(finalResp));
      }));

      const client = createClient();
      const result = await client.executeStatement({ statement: "SELECT * FROM big_table" });

      expect(result.statementHandle).toBe("async-handle");
      expect(callCount).toBeGreaterThanOrEqual(2);
    });

    test("throws on error response", async () => {
      const errorResp = {
        code: "002003",
        message: "SQL compilation error: Table does not exist",
        sqlState: "42S02",
      };
      setFetchMock(mock(() => Promise.resolve(jsonResponse(errorResp, 422))));

      const client = createClient();
      await expect(client.executeStatement({ statement: "SELECT * FROM nope" })).rejects.toThrow(
        "Table does not exist",
      );
    });
  });

  describe("fetchPartition", () => {
    test("sends GET with partition parameter", async () => {
      const mockFetch = mock(() => Promise.resolve(jsonResponse({ data: [["a"], ["b"]] })));
      setFetchMock(mockFetch);

      const client = createClient();
      const data = await client.fetchPartition("handle-456", 2);

      expect(data).toEqual([["a"], ["b"]]);
      const call = mockFetch.mock.calls[0] as unknown as [string, RequestInit];
      expect(call[0]).toBe(
        "https://test.snowflakecomputing.com/api/v2/statements/handle-456?partition=2",
      );
      expect(call[1].method).toBe("GET");
    });

    test("returns empty array when data is missing", async () => {
      setFetchMock(mock(() => Promise.resolve(jsonResponse({}))));

      const client = createClient();
      const data = await client.fetchPartition("handle-789", 0);
      expect(data).toEqual([]);
    });
  });

  describe("cancelStatement", () => {
    test("sends POST to cancel endpoint", async () => {
      const mockFetch = mock(() => Promise.resolve(jsonResponse({ code: "000000" })));
      setFetchMock(mockFetch);

      const client = createClient();
      await client.cancelStatement("handle-cancel");

      const call = mockFetch.mock.calls[0] as unknown as [string, RequestInit];
      expect(call[0]).toBe(
        "https://test.snowflakecomputing.com/api/v2/statements/handle-cancel/cancel",
      );
      expect(call[1].method).toBe("POST");
    });
  });

  describe("retry behavior", () => {
    test("retries on 429 status", async () => {
      let callCount = 0;
      setFetchMock(mock(() => {
        callCount++;
        if (callCount <= 2) {
          return Promise.resolve(new Response("Too Many Requests", { status: 429 }));
        }
        return Promise.resolve(jsonResponse(makeQueryResponse()));
      }));

      const client = createClient();
      const result = await client.executeStatement({ statement: "SELECT 1" });

      expect(result.data).toEqual([["hello"]]);
      expect(callCount).toBe(3);
    });

    test("retries on 500 status", async () => {
      let callCount = 0;
      setFetchMock(mock(() => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(new Response("Internal Server Error", { status: 500 }));
        }
        return Promise.resolve(jsonResponse(makeQueryResponse()));
      }));

      const client = createClient();
      const result = await client.executeStatement({ statement: "SELECT 1" });

      expect(result.data).toEqual([["hello"]]);
      expect(callCount).toBe(2);
    });
  });

  describe("timeout", () => {
    test("aborts via AbortController on timeout", async () => {
      let receivedSignal: AbortSignal | undefined;
      setFetchMock(mock((_url: unknown, init: unknown) => {
        receivedSignal = (init as RequestInit).signal ?? undefined;
        return Promise.resolve(jsonResponse(makeQueryResponse()));
      }));

      const client = createClient({ timeoutMs: 500 });
      await client.executeStatement({ statement: "SELECT 1" });

      expect(receivedSignal).toBeDefined();
      expect(receivedSignal).toBeInstanceOf(AbortSignal);
    });
  });
});
