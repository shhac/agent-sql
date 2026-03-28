import { describe, test, expect, beforeEach, spyOn } from "bun:test";
import { configureFormat } from "../src/lib/format";
import {
  handleActionError,
  resolveConnectionAlias,
  printQueryResults,
  configureTruncationFromOpts,
} from "../src/cli/action-helpers";
import type { QueryResult } from "../src/drivers/types";

// Capture stderr/stdout writes
let stderrOutput: string;
let stdoutOutput: string;

beforeEach(() => {
  stderrOutput = "";
  stdoutOutput = "";
  process.exitCode = 0;
  configureFormat("json");

  spyOn(process.stderr, "write").mockImplementation((chunk: string | Uint8Array) => {
    stderrOutput += String(chunk);
    return true;
  });
  spyOn(process.stdout, "write").mockImplementation((chunk: string | Uint8Array) => {
    stdoutOutput += String(chunk);
    return true;
  });
});

describe("handleActionError", () => {
  test("calls printError with enhanced error message", () => {
    handleActionError(new Error("connection refused"));
    const parsed = JSON.parse(stderrOutput.trim());
    expect(parsed.error).toContain("connection refused");
  });

  test("sets process.exitCode to 1", () => {
    handleActionError(new Error("fail"));
    expect(process.exitCode).toBe(1);
  });

  test("handles non-Error objects", () => {
    handleActionError("string error");
    const parsed = JSON.parse(stderrOutput.trim());
    expect(parsed.error).toContain("string error");
    expect(process.exitCode).toBe(1);
  });
});

describe("resolveConnectionAlias", () => {
  test("returns opts.connection when provided", () => {
    const fakeCmd = { parent: { getOptionValue: () => "parent-conn" } } as never;
    const result = resolveConnectionAlias({ connection: "my-conn" }, fakeCmd);
    expect(result).toBe("my-conn");
  });

  test("falls back to parent command getOptionValue", () => {
    const fakeCmd = {
      parent: {
        getOptionValue: (key: string) => (key === "connection" ? "parent-conn" : undefined),
      },
    } as never;
    const result = resolveConnectionAlias({}, fakeCmd);
    expect(result).toBe("parent-conn");
  });

  test("returns undefined when neither is set", () => {
    const fakeCmd = {
      parent: { getOptionValue: () => undefined },
    } as never;
    const result = resolveConnectionAlias({}, fakeCmd);
    expect(result).toBeUndefined();
  });
});

describe("printQueryResults", () => {
  const makeResult = (): { result: QueryResult; displayRows: Record<string, unknown>[] } => ({
    result: { columns: ["id", "name"], rows: [{ id: 1, name: "Alice" }] },
    displayRows: [{ id: 1, name: "Alice" }],
  });

  test("calls printPaginated for non-compact mode", () => {
    const { result, displayRows } = makeResult();
    printQueryResults({ result, displayRows, hasMore: false });
    const parsed = JSON.parse(stdoutOutput.trim());
    expect(parsed.columns).toEqual(["id", "name"]);
    expect(parsed.rows).toBeDefined();
  });

  test("calls printCompact for compact mode", () => {
    const { result, displayRows } = makeResult();
    printQueryResults({ result, displayRows, hasMore: false, compact: true });
    const parsed = JSON.parse(stdoutOutput.trim());
    expect(parsed.columns).toContain("id");
    expect(parsed.columns).toContain("name");
    expect(parsed.rows).toBeDefined();
  });
});

describe("configureTruncationFromOpts", () => {
  test("configures truncation from opts without throwing", () => {
    expect(() => configureTruncationFromOpts({})).not.toThrow();
  });

  test("accepts expand and full options", () => {
    expect(() => configureTruncationFromOpts({ expand: "name,bio", full: true })).not.toThrow();
  });
});
