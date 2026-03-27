import { describe, test, expect } from "bun:test";
import { enhanceError } from "../src/lib/errors.ts";

describe("PostgreSQL error mapping", () => {
  const makePgError = (opts: {
    code: string;
    message: string;
    extra?: Record<string, unknown>;
  }) => {
    const err = new Error(opts.message) as Error & { code?: string; severity?: string };
    err.code = opts.code;
    if (opts.extra) {
      Object.assign(err, opts.extra);
    }
    return err;
  };

  test("42P01 undefined table → fixableBy agent", () => {
    const result = enhanceError(
      makePgError({ code: "42P01", message: 'relation "orders" does not exist' }),
    );
    expect(result.fixableBy).toBe("agent");
    expect(result.message).toContain("orders");
  });

  test("42P01 with available tables in context", () => {
    const result = enhanceError(
      makePgError({ code: "42P01", message: 'relation "orders" does not exist' }),
      {
        availableTables: ["users", "products", "invoices"],
      },
    );
    expect(result.fixableBy).toBe("agent");
    expect(result.hint).toContain("users");
    expect(result.hint).toContain("products");
  });

  test("42703 undefined column → fixableBy agent", () => {
    const result = enhanceError(
      makePgError({ code: "42703", message: 'column "emal" does not exist' }),
    );
    expect(result.fixableBy).toBe("agent");
    expect(result.message).toContain("emal");
  });

  test("25006 read-only violation → fixableBy human, mentions writePermission", () => {
    const result = enhanceError(
      makePgError({ code: "25006", message: "cannot execute INSERT in a read-only transaction" }),
    );
    expect(result.fixableBy).toBe("human");
    expect(result.hint).toContain("writePermission");
  });

  test("57014 query cancelled/timeout → fixableBy retry", () => {
    const result = enhanceError(
      makePgError({ code: "57014", message: "canceling statement due to statement timeout" }),
    );
    expect(result.fixableBy).toBe("retry");
    expect(result.hint).toContain("--timeout");
  });

  test("28P01 auth failed → fixableBy human", () => {
    const result = enhanceError(
      makePgError({ code: "28P01", message: 'password authentication failed for user "reader"' }),
    );
    expect(result.fixableBy).toBe("human");
    expect(result.hint).toContain("credential");
  });

  test("08006 connection failed → fixableBy human", () => {
    const result = enhanceError(
      makePgError({
        code: "08006",
        message:
          "could not connect to server: Connection refused. Is the server running on host db.internal.corp port 5432?",
      }),
      { connectionAlias: "prod" },
    );
    expect(result.fixableBy).toBe("human");
  });

  test("08001 connection failed → fixableBy human", () => {
    const result = enhanceError(
      makePgError({
        code: "08001",
        message: "could not connect to host db.internal.corp port 5432",
      }),
      { connectionAlias: "staging" },
    );
    expect(result.fixableBy).toBe("human");
  });

  test("generic PG error → fixableBy agent", () => {
    const result = enhanceError(
      makePgError({ code: "42601", message: 'syntax error at or near "SELEC"' }),
    );
    expect(result.fixableBy).toBe("agent");
  });
});

describe("SQLite error mapping", () => {
  const makeSqliteError = (code: number | undefined, message: string) => {
    const err = new Error(message) as Error & { code?: number; errno?: number };
    if (code !== undefined) {
      err.code = code;
      err.errno = code;
    }
    return err;
  };

  test("SQLITE_READONLY (code 8) → fixableBy human, mentions writePermission", () => {
    const result = enhanceError(makeSqliteError(8, "attempt to write a readonly database"));
    expect(result.fixableBy).toBe("human");
    expect(result.hint).toContain("writePermission");
  });

  test("SQLITE_READONLY by message → fixableBy human", () => {
    const result = enhanceError(makeSqliteError(undefined, "attempt to write a readonly database"));
    expect(result.fixableBy).toBe("human");
    expect(result.hint).toContain("writePermission");
  });

  test("SQLITE_ERROR no such table → fixableBy agent", () => {
    const result = enhanceError(makeSqliteError(1, "no such table: orders"));
    expect(result.fixableBy).toBe("agent");
    expect(result.hint).toContain("schema tables");
  });

  test("SQLITE_ERROR no such column → fixableBy agent", () => {
    const result = enhanceError(makeSqliteError(1, "no such column: emal"));
    expect(result.fixableBy).toBe("agent");
  });

  test("SQLITE_BUSY → fixableBy retry", () => {
    const result = enhanceError(makeSqliteError(5, "database is locked"));
    expect(result.fixableBy).toBe("retry");
  });

  test("generic SQLite error → fixableBy agent", () => {
    const result = enhanceError(makeSqliteError(1, 'near "SELEC": syntax error'));
    expect(result.fixableBy).toBe("agent");
  });
});

describe("connection not found", () => {
  test("lists available connections when provided", () => {
    const err = new Error('Connection "staging" not found');
    const result = enhanceError(err, { availableConnections: ["prod", "dev", "local"] });
    expect(result.fixableBy).toBe("agent");
    expect(result.hint).toContain("prod");
    expect(result.hint).toContain("dev");
    expect(result.hint).toContain("local");
  });
});

describe("hostname sanitization", () => {
  test("replaces host:port with alias in PG connection errors", () => {
    const err = new Error(
      "could not connect to server: Connection refused. Is the server running on host db.internal.corp port 5432?",
    );
    (err as Error & { code?: string }).code = "08006";
    const result = enhanceError(err, { connectionAlias: "prod" });
    expect(result.message).not.toContain("db.internal.corp");
    expect(result.message).toContain("prod");
  });

  test("replaces hostname in generic connection error messages", () => {
    const err = new Error("getaddrinfo ENOTFOUND secret-db.internal.example.com");
    const result = enhanceError(err, { connectionAlias: "mydb" });
    expect(result.message).not.toContain("secret-db.internal.example.com");
    expect(result.message).toContain("mydb");
  });

  test("no sanitization when alias is not provided", () => {
    const err = new Error("getaddrinfo ENOTFOUND secret-db.internal.example.com");
    const result = enhanceError(err);
    expect(result.message).toContain("secret-db.internal.example.com");
  });
});

describe("generic/unknown errors", () => {
  test("unknown error defaults to fixableBy agent", () => {
    const result = enhanceError(new Error("something went wrong"));
    expect(result.fixableBy).toBe("agent");
    expect(result.message).toBe("something went wrong");
  });

  test("error with no code defaults to fixableBy agent", () => {
    const result = enhanceError(new Error("unexpected error"));
    expect(result.fixableBy).toBe("agent");
  });
});

describe("return shape", () => {
  test("always returns message and fixableBy", () => {
    const result = enhanceError(new Error("test"));
    expect(result).toHaveProperty("message");
    expect(result).toHaveProperty("fixableBy");
    expect(["agent", "human", "retry"]).toContain(result.fixableBy);
  });

  test("hint is optional", () => {
    const result = enhanceError(new Error("something unknown"));
    expect(typeof result.message).toBe("string");
    expect(typeof result.fixableBy).toBe("string");
    // hint may or may not be present
    if (result.hint !== undefined) {
      expect(typeof result.hint).toBe("string");
    }
  });
});
