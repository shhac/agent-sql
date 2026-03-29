import { describe, test, expect } from "bun:test";
import { enhanceError } from "../src/lib/errors";

describe("Snowflake error mapping", () => {
  const makeSnowflakeError = (opts: { code: string; message: string; sqlState?: string }) => {
    const err = new Error(opts.message) as Error & { code?: string; sqlState?: string };
    err.code = opts.code;
    if (opts.sqlState) {
      err.sqlState = opts.sqlState;
    }
    return err;
  };

  test("002003 object not found → fixableBy agent", () => {
    const result = enhanceError(
      makeSnowflakeError({ code: "002003", message: "Object 'ORDERS' does not exist" }),
    );
    expect(result.fixableBy).toBe("agent");
    expect(result.hint).toContain("schema tables");
  });

  test("000904 column not found → fixableBy agent", () => {
    const result = enhanceError(
      makeSnowflakeError({ code: "000904", message: "invalid identifier 'EMAL'" }),
    );
    expect(result.fixableBy).toBe("agent");
    expect(result.hint).toContain("schema describe");
  });

  test("390100 auth failure → fixableBy human", () => {
    const result = enhanceError(
      makeSnowflakeError({
        code: "390100",
        message: "Incorrect username or password was specified",
      }),
    );
    expect(result.fixableBy).toBe("human");
    expect(result.hint).toContain("credential");
  });

  test("000625 timeout → fixableBy retry", () => {
    const result = enhanceError(
      makeSnowflakeError({ code: "000625", message: "Statement timed out" }),
    );
    expect(result.fixableBy).toBe("retry");
    expect(result.hint).toContain("--timeout");
  });

  test("003001 insufficient privileges → fixableBy human", () => {
    const result = enhanceError(
      makeSnowflakeError({
        code: "003001",
        message: "Insufficient privileges to operate on table",
      }),
    );
    expect(result.fixableBy).toBe("human");
    expect(result.hint).toContain("privileges");
  });

  test("999999 unknown Snowflake error → fixableBy agent", () => {
    const result = enhanceError(
      makeSnowflakeError({ code: "999999", message: "Some unknown Snowflake error" }),
    );
    expect(result.fixableBy).toBe("agent");
  });

  test("non-6-digit code is not matched as Snowflake", () => {
    const err = Object.assign(new Error('relation "orders" does not exist'), { code: "42P01" });
    const result = enhanceError(err);
    expect(result.fixableBy).toBe("agent");
    expect(result.hint).toContain("schema tables");
  });
});
