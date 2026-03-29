import { describe, test, expect } from "bun:test";
import { withCatch, withCatchSync, withRetry } from "../src/lib/with-catch";

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

describe("withRetry", () => {
  test("returns result on first success", async () => {
    const fn = () => Promise.resolve(42);
    const result = await withRetry(fn, {
      maxRetries: 3,
      shouldRetry: () => true,
      delay: () => 0,
    });
    expect(result).toBe(42);
  });

  test("retries on failure then succeeds", async () => {
    let calls = 0;
    const fn = () => {
      calls++;
      if (calls < 3) {
        throw new Error("fail");
      }
      return Promise.resolve("ok");
    };
    const result = await withRetry(fn, {
      maxRetries: 3,
      shouldRetry: () => true,
      delay: () => 0,
    });
    expect(result).toBe("ok");
    expect(calls).toBe(3);
  });

  test("throws after exhausting retries", async () => {
    const fn = () => Promise.reject(new Error("always fails"));
    await expect(
      withRetry(fn, { maxRetries: 2, shouldRetry: () => true, delay: () => 0 }),
    ).rejects.toThrow("always fails");
  });

  test("respects shouldRetry — stops early on non-retryable error", async () => {
    let calls = 0;
    const fn = () => {
      calls++;
      return Promise.reject(new Error("fatal"));
    };
    await expect(
      withRetry(fn, {
        maxRetries: 5,
        shouldRetry: (err) => err.message !== "fatal",
        delay: () => 0,
      }),
    ).rejects.toThrow("fatal");
    expect(calls).toBe(1);
  });

  test("calls delay with attempt number", async () => {
    const delays: number[] = [];
    let calls = 0;
    const fn = () => {
      calls++;
      if (calls < 3) {
        return Promise.reject(new Error("retry"));
      }
      return Promise.resolve("done");
    };
    await withRetry(fn, {
      maxRetries: 3,
      shouldRetry: () => true,
      delay: (attempt) => {
        delays.push(attempt);
        return 0;
      },
    });
    expect(delays).toEqual([0, 1]);
  });
});
