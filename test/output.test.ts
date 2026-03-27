import { describe, expect, test, beforeEach, afterEach } from "bun:test";
import {
  printJson,
  printError,
  printPaginated,
  printCompact,
  resolvePageSize,
} from "../src/lib/output.ts";

function captureStdout(fn: () => void): string {
  const chunks: string[] = [];
  const original = process.stdout.write;
  process.stdout.write = (chunk: string | Uint8Array, ..._rest: unknown[]) => {
    chunks.push(typeof chunk === "string" ? chunk : new TextDecoder().decode(chunk));
    return true;
  };
  try {
    fn();
  } finally {
    process.stdout.write = original;
  }
  return chunks.join("");
}

function captureStderr(fn: () => void): string {
  const chunks: string[] = [];
  const original = process.stderr.write;
  process.stderr.write = (chunk: string | Uint8Array, ..._rest: unknown[]) => {
    chunks.push(typeof chunk === "string" ? chunk : new TextDecoder().decode(chunk));
    return true;
  };
  try {
    fn();
  } finally {
    process.stderr.write = original;
  }
  return chunks.join("");
}

describe("printJson", () => {
  test("outputs valid JSON to stdout with 2-space indent", () => {
    const data = { id: 1, name: "Alice" };
    const output = captureStdout(() => printJson(data));
    const parsed = JSON.parse(output);
    expect(parsed).toEqual({ id: 1, name: "Alice" });
    expect(output).toContain("  ");
  });

  test("preserves null values by default (query output)", () => {
    const data = { id: 1, name: "Alice", bio: null };
    const output = captureStdout(() => printJson(data));
    const parsed = JSON.parse(output);
    expect(parsed.bio).toBeNull();
    expect(output).toContain('"bio": null');
  });

  test("prunes null and empty values when prune option is set", () => {
    const data = { id: 1, name: "Alice", bio: null, tags: [] };
    const output = captureStdout(() => printJson(data, { prune: true }));
    const parsed = JSON.parse(output);
    expect(parsed.bio).toBeUndefined();
    expect(parsed.tags).toBeUndefined();
    expect(parsed).toEqual({ id: 1, name: "Alice" });
  });

  test("preserves empty strings when not pruning", () => {
    const data = { id: 1, name: "" };
    const output = captureStdout(() => printJson(data));
    const parsed = JSON.parse(output);
    expect(parsed.name).toBe("");
  });

  test("preserves zero and false values even when pruning", () => {
    const data = { count: 0, active: false, name: null };
    const output = captureStdout(() => printJson(data, { prune: true }));
    const parsed = JSON.parse(output);
    expect(parsed.count).toBe(0);
    expect(parsed.active).toBe(false);
    expect(parsed.name).toBeUndefined();
  });

  test("handles nested objects", () => {
    const data = { user: { id: 1, address: { city: "NYC" } } };
    const output = captureStdout(() => printJson(data));
    const parsed = JSON.parse(output);
    expect(parsed.user.address.city).toBe("NYC");
  });

  test("handles arrays of rows with nulls preserved", () => {
    const rows = [
      { id: 1, name: "Alice", bio: null },
      { id: 2, name: "Bob", bio: "Developer" },
    ];
    const output = captureStdout(() => printJson(rows));
    const parsed = JSON.parse(output);
    expect(parsed[0].bio).toBeNull();
    expect(parsed[1].bio).toBe("Developer");
  });
});

describe("printError", () => {
  let originalExitCode: typeof process.exitCode;

  beforeEach(() => {
    originalExitCode = process.exitCode;
  });

  afterEach(() => {
    process.exitCode = originalExitCode;
  });

  test("writes JSON error to stderr with correct shape", () => {
    const output = captureStderr(() => printError({ message: "Table not found" }));
    const parsed = JSON.parse(output);
    expect(parsed.error).toBe("Table not found");
  });

  test("sets process.exitCode to 1", () => {
    captureStderr(() => printError({ message: "fail" }));
    expect(process.exitCode).toBe(1);
  });

  test("includes hint when provided", () => {
    const output = captureStderr(() =>
      printError({
        message: "Connection failed",
        hint: "Check your credentials",
      }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.hint).toBe("Check your credentials");
  });

  test("includes fixable_by when provided", () => {
    const output = captureStderr(() =>
      printError({
        message: "Typo in table name",
        fixableBy: "agent",
      }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.fixable_by).toBe("agent");
  });

  test("includes all fields when all provided", () => {
    const output = captureStderr(() =>
      printError({
        message: "Read-only violation",
        hint: "Use a write credential",
        fixableBy: "human",
      }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.error).toBe("Read-only violation");
    expect(parsed.hint).toBe("Use a write credential");
    expect(parsed.fixable_by).toBe("human");
  });

  test("omits hint and fixable_by when not provided", () => {
    const output = captureStderr(() => printError({ message: "Something broke" }));
    const parsed = JSON.parse(output);
    expect(parsed).toEqual({ error: "Something broke" });
    expect(parsed.hint).toBeUndefined();
    expect(parsed.fixable_by).toBeUndefined();
  });
});

describe("printPaginated", () => {
  test("wraps items in rows key with columns", () => {
    const items = [{ id: 1 }, { id: 2 }];
    const output = captureStdout(() =>
      printPaginated({ columns: ["id"], items, hasMore: false, rowCount: 2 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.columns).toEqual(["id"]);
    expect(parsed.rows).toEqual([{ id: 1 }, { id: 2 }]);
  });

  test("includes pagination when hasMore is true", () => {
    const items = [{ id: 1 }];
    const output = captureStdout(() =>
      printPaginated({ columns: ["id"], items, hasMore: true, rowCount: 20 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.pagination).toEqual({ hasMore: true, rowCount: 20 });
  });

  test("omits pagination when hasMore is false", () => {
    const items = [{ id: 1 }];
    const output = captureStdout(() =>
      printPaginated({ columns: ["id"], items, hasMore: false, rowCount: 1 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.pagination).toBeUndefined();
  });

  test("preserves null values in items", () => {
    const items = [{ id: 1, name: null }];
    const output = captureStdout(() =>
      printPaginated({ columns: ["id", "name"], items, hasMore: false, rowCount: 1 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.rows[0].name).toBeNull();
  });

  test("handles empty items array", () => {
    const output = captureStdout(() =>
      printPaginated({ columns: ["id"], items: [], hasMore: false, rowCount: 0 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.rows).toEqual([]);
  });
});

describe("printCompact", () => {
  test("produces array-of-arrays format", () => {
    const columns = ["id", "name", "email"];
    const rows = [
      [1, "Alice", "alice@example.com"],
      [2, "Bob", "bob@example.com"],
    ];
    const output = captureStdout(() =>
      printCompact({ columns, rows, hasMore: false, rowCount: 2 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.columns).toEqual(["id", "name", "email"]);
    expect(parsed.rows).toEqual([
      [1, "Alice", "alice@example.com"],
      [2, "Bob", "bob@example.com"],
    ]);
  });

  test("preserves null values in compact output", () => {
    const columns = ["id", "bio"];
    const rows = [[1, null]];
    const output = captureStdout(() =>
      printCompact({ columns, rows, hasMore: false, rowCount: 1 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.rows[0]).toEqual([1, null]);
  });

  test("handles empty rows", () => {
    const columns = ["id", "name"];
    const output = captureStdout(() =>
      printCompact({ columns, rows: [], hasMore: false, rowCount: 0 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.columns).toEqual(["id", "name"]);
    expect(parsed.rows).toEqual([]);
  });

  test("outputs rows in correct order", () => {
    const columns = ["z", "a", "m"];
    const rows = [["zulu", "alpha", "mike"]];
    const output = captureStdout(() =>
      printCompact({ columns, rows, hasMore: false, rowCount: 1 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.rows[0]).toEqual(["zulu", "alpha", "mike"]);
  });

  test("includes pagination when hasMore is true", () => {
    const columns = ["id"];
    const rows = [[1]];
    const output = captureStdout(() =>
      printCompact({ columns, rows, hasMore: true, rowCount: 20 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.pagination).toEqual({ hasMore: true, rowCount: 20 });
  });

  test("includes @truncated metadata embedded in rows", () => {
    const columns = ["id", "bio", "@truncated"];
    const rows = [[1, "Short…", { bio: 5000 }]];
    const output = captureStdout(() =>
      printCompact({ columns, rows, hasMore: false, rowCount: 1 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.columns).toContain("@truncated");
    expect(parsed.rows[0][2]).toEqual({ bio: 5000 });
  });
});

describe("resolvePageSize", () => {
  test("returns CLI flag value when provided", () => {
    const result = resolvePageSize({ limit: 50 });
    expect(result).toBe(50);
  });

  test("returns config value when no CLI flag", () => {
    const result = resolvePageSize({ configLimit: 30 });
    expect(result).toBe(30);
  });

  test("returns default 20 when nothing provided", () => {
    const result = resolvePageSize({});
    expect(result).toBe(20);
  });

  test("CLI flag takes precedence over config", () => {
    const result = resolvePageSize({ limit: 10, configLimit: 30 });
    expect(result).toBe(10);
  });

  test("config takes precedence over default", () => {
    const result = resolvePageSize({ configLimit: 42 });
    expect(result).toBe(42);
  });
});

describe("NULL preservation", () => {
  test("null values stay in printJson output", () => {
    const data = {
      columns: ["id", "name", "deleted_at"],
      rows: [
        { id: 1, name: "Alice", deleted_at: null },
        { id: 2, name: "Bob", deleted_at: "2024-01-01" },
      ],
    };
    const output = captureStdout(() => printJson(data));
    const parsed = JSON.parse(output);
    expect(parsed.rows[0].deleted_at).toBeNull();
    expect(parsed.rows[1].deleted_at).toBe("2024-01-01");
  });

  test("null values stay in printCompact output", () => {
    const columns = ["id", "deleted_at"];
    const rows = [[1, null]];
    const output = captureStdout(() =>
      printCompact({ columns, rows, hasMore: false, rowCount: 1 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.rows[0][1]).toBeNull();
  });

  test("null values stay in printPaginated output", () => {
    const items = [{ id: 1, value: null }];
    const output = captureStdout(() =>
      printPaginated({ columns: ["id", "value"], items, hasMore: false, rowCount: 1 }),
    );
    const parsed = JSON.parse(output);
    expect(parsed.rows[0].value).toBeNull();
  });
});
