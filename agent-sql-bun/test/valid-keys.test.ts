import { describe, test, expect } from "bun:test";
import { parseConfigValue, VALID_KEYS } from "../src/cli/config/valid-keys.ts";

describe("parseConfigValue", () => {
  describe("number keys", () => {
    test("parses a valid number within range", () => {
      expect(parseConfigValue("defaults.limit", "50")).toBe(50);
    });

    test("rejects non-integer values", () => {
      expect(() => parseConfigValue("defaults.limit", "3.5")).toThrow(/must be an integer/);
    });

    test("rejects non-numeric values", () => {
      expect(() => parseConfigValue("defaults.limit", "abc")).toThrow(/must be an integer/);
    });

    test("rejects values below minimum", () => {
      expect(() => parseConfigValue("defaults.limit", "0")).toThrow(/minimum is 1/);
    });

    test("rejects values above maximum", () => {
      expect(() => parseConfigValue("defaults.limit", "9999")).toThrow(/maximum is 1000/);
    });

    test("accepts boundary values", () => {
      expect(parseConfigValue("defaults.limit", "1")).toBe(1);
      expect(parseConfigValue("defaults.limit", "1000")).toBe(1000);
    });
  });

  describe("string keys", () => {
    test("accepts an allowed value", () => {
      expect(parseConfigValue("defaults.format", "jsonl")).toBe("jsonl");
      expect(parseConfigValue("defaults.format", "json")).toBe("json");
      expect(parseConfigValue("defaults.format", "yaml")).toBe("yaml");
      expect(parseConfigValue("defaults.format", "csv")).toBe("csv");
    });

    test("rejects a disallowed value", () => {
      expect(() => parseConfigValue("defaults.format", "xml")).toThrow(
        /must be one of: jsonl, json, yaml, csv.*Got: "xml"/,
      );
    });

    test("rejects empty string", () => {
      expect(() => parseConfigValue("defaults.format", "")).toThrow(/must be one of/);
    });

    test("is case-sensitive", () => {
      expect(() => parseConfigValue("defaults.format", "JSON")).toThrow(/must be one of/);
    });
  });

  describe("unknown keys", () => {
    test("rejects unknown keys with helpful message", () => {
      expect(() => parseConfigValue("unknown.key", "value")).toThrow(/Unknown config key/);
    });
  });
});

describe("VALID_KEYS", () => {
  test("includes defaults.format as a string key", () => {
    const formatDef = VALID_KEYS.find((k) => k.key === "defaults.format");
    expect(formatDef).toBeDefined();
    expect(formatDef!.type).toBe("string");
    expect(formatDef!.defaultValue).toBe("jsonl");
  });
});
