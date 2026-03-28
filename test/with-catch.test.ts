import { describe, test, expect } from "bun:test";
import { withCatch, withCatchSync } from "../src/lib/with-catch";

describe("withCatch", () => {
  test("resolving promise returns [undefined, value]", async () => {
    const [err, value] = await withCatch(Promise.resolve(42));
    expect(err).toBeUndefined();
    expect(value).toBe(42);
  });

  test("rejecting promise returns [Error]", async () => {
    const result = await withCatch(Promise.reject(new Error("boom")));
    expect(result).toHaveLength(1);
    expect(result[0]).toBeInstanceOf(Error);
    expect((result[0] as Error).message).toBe("boom");
  });

  test("non-Error rejection is wrapped in Error", async () => {
    const result = await withCatch(Promise.reject("string failure"));
    expect(result).toHaveLength(1);
    expect(result[0]).toBeInstanceOf(Error);
    expect((result[0] as Error).message).toBe("string failure");
  });
});

describe("withCatchSync", () => {
  test("successful function returns [undefined, value]", () => {
    const [err, value] = withCatchSync(() => "hello");
    expect(err).toBeUndefined();
    expect(value).toBe("hello");
  });

  test("throwing function returns [Error]", () => {
    const result = withCatchSync(() => {
      throw new Error("sync boom");
    });
    expect(result).toHaveLength(1);
    expect(result[0]).toBeInstanceOf(Error);
    expect((result[0] as Error).message).toBe("sync boom");
  });

  test("non-Error throw is wrapped in Error", () => {
    const result = withCatchSync(() => {
      throw 404;
    });
    expect(result).toHaveLength(1);
    expect(result[0]).toBeInstanceOf(Error);
    expect((result[0] as Error).message).toBe("404");
  });
});
