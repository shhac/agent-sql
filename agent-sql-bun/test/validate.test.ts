import { describe, test, expect } from "bun:test";
import { validateConfig, validateCredential } from "../src/lib/validate";

describe("validateConfig", () => {
  test("valid config returns parsed shape", () => {
    const result = validateConfig({
      default_connection: "local",
      connections: { local: { driver: "sqlite" } },
      settings: { defaults: { limit: 20 } },
    });
    expect(result.default_connection).toBe("local");
    expect(result.connections).toBeDefined();
    expect(result.settings).toBeDefined();
  });

  test("missing fields get safe defaults", () => {
    const result = validateConfig({});
    expect(result.connections).toEqual({});
    expect(result.settings).toEqual({});
    expect(result.default_connection).toBeUndefined();
  });

  test("null throws", () => {
    expect(() => validateConfig(null)).toThrow("must be an object");
  });

  test("string throws", () => {
    expect(() => validateConfig("not an object")).toThrow("must be an object");
  });

  test("number throws", () => {
    expect(() => validateConfig(42)).toThrow("must be an object");
  });
});

describe("validateCredential", () => {
  test("valid credential returns parsed shape", () => {
    const result = validateCredential({
      username: "user",
      password: "pass",
      writePermission: true,
    });
    expect(result.username).toBe("user");
    expect(result.password).toBe("pass");
    expect(result.writePermission).toBe(true);
  });

  test("missing fields return undefined", () => {
    const result = validateCredential({});
    expect(result.username).toBeUndefined();
    expect(result.password).toBeUndefined();
    expect(result.writePermission).toBeUndefined();
  });

  test("wrong types are coerced to undefined", () => {
    const result = validateCredential({ username: 42, password: true, writePermission: "yes" });
    expect(result.username).toBeUndefined();
    expect(result.password).toBeUndefined();
    expect(result.writePermission).toBeUndefined();
  });

  test("null throws", () => {
    expect(() => validateCredential(null)).toThrow("must be an object");
  });
});
