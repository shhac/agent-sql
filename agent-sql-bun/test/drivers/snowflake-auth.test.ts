import { describe, test, expect } from "bun:test";
import { buildPatHeaders, buildBaseUrl } from "../../src/drivers/snowflake/auth";

describe("buildPatHeaders", () => {
  test("returns correct Authorization header", () => {
    const headers = buildPatHeaders("my-secret-token");
    expect(headers["Authorization"]).toBe("Bearer my-secret-token");
  });

  test("returns correct token type header", () => {
    const headers = buildPatHeaders("tok123");
    expect(headers["X-Snowflake-Authorization-Token-Type"]).toBe("PROGRAMMATIC_ACCESS_TOKEN");
  });

  test("returns exactly two headers", () => {
    const headers = buildPatHeaders("tok");
    expect(Object.keys(headers)).toHaveLength(2);
  });
});

describe("buildBaseUrl", () => {
  test("constructs correct URL from account identifier", () => {
    expect(buildBaseUrl("myaccount")).toBe("https://myaccount.snowflakecomputing.com");
  });

  test("handles account with hyphens", () => {
    expect(buildBaseUrl("my-org-account")).toBe("https://my-org-account.snowflakecomputing.com");
  });

  test("handles account with dots", () => {
    expect(buildBaseUrl("xy12345.us-east-1")).toBe(
      "https://xy12345.us-east-1.snowflakecomputing.com",
    );
  });

  test("handles org-account format", () => {
    expect(buildBaseUrl("myorg-myaccount")).toBe("https://myorg-myaccount.snowflakecomputing.com");
  });
});
