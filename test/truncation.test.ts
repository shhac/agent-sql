import { describe, expect, it, beforeEach } from "bun:test";
import {
  configureTruncation,
  applyTruncation,
  applyTruncationCompact,
} from "../src/lib/truncation.ts";

const long = (n: number) => "x".repeat(n);

beforeEach(() => {
  configureTruncation({});
});

describe("applyTruncation", () => {
  it("truncates strings exceeding default threshold (200)", () => {
    const rows = [{ id: 1, bio: long(250) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect((result[0]!.bio as string).length).toBe(201); // 200 + ellipsis
    expect((result[0]!.bio as string).endsWith("\u2026")).toBe(true);
  });

  it("adds @truncated metadata with column name and original length", () => {
    const rows = [{ id: 1, bio: long(250) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!["@truncated"]).toEqual({ bio: 250 });
  });

  it("@truncated is always present as null when no truncation occurs", () => {
    const rows = [{ id: 1, name: "Alice" }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!["@truncated"]).toBeNull();
  });

  it("does not truncate strings exactly at threshold", () => {
    const rows = [{ id: 1, bio: long(200) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!.bio).toBe(long(200));
    expect(result[0]!["@truncated"]).toBeNull();
  });

  it("truncates strings one char over threshold", () => {
    const rows = [{ id: 1, bio: long(201) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect((result[0]!.bio as string).length).toBe(201);
    expect((result[0]!.bio as string).endsWith("\u2026")).toBe(true);
    expect(result[0]!["@truncated"]).toEqual({ bio: 201 });
  });

  it("passes through empty strings without truncation", () => {
    const rows = [{ id: 1, bio: "" }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!.bio).toBe("");
    expect(result[0]!["@truncated"]).toBeNull();
  });

  it("passes through null values without truncation", () => {
    const rows = [{ id: 1, bio: null }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!.bio).toBeNull();
    expect(result[0]!["@truncated"]).toBeNull();
  });

  it("passes through numeric values without truncation", () => {
    const rows = [{ id: 1, score: 99999 }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!.score).toBe(99999);
    expect(result[0]!["@truncated"]).toBeNull();
  });

  it("truncates multiple columns in the same row", () => {
    const rows = [{ id: 1, bio: long(300), notes: long(400) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!["@truncated"]).toEqual({ bio: 300, notes: 400 });
    expect((result[0]!.bio as string).endsWith("\u2026")).toBe(true);
    expect((result[0]!.notes as string).endsWith("\u2026")).toBe(true);
  });

  it("handles multiple rows with mixed truncation", () => {
    const rows = [
      { id: 1, bio: long(300) },
      { id: 2, bio: "short" },
      { id: 3, bio: long(500) },
    ];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!["@truncated"]).toEqual({ bio: 300 });
    expect(result[1]!["@truncated"]).toBeNull();
    expect(result[2]!["@truncated"]).toEqual({ bio: 500 });
  });
});

describe("configureTruncation with --full", () => {
  it("skips all truncation when full=true", () => {
    configureTruncation({ full: true });
    const rows = [{ id: 1, bio: long(500) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!.bio).toBe(long(500));
    expect(result[0]!["@truncated"]).toBeNull();
  });
});

describe("configureTruncation with --expand", () => {
  it("skips truncation for expanded fields only", () => {
    configureTruncation({ expand: "bio" });
    const rows = [{ id: 1, bio: long(500), notes: long(500) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!.bio).toBe(long(500));
    expect((result[0]!.notes as string).endsWith("\u2026")).toBe(true);
    expect(result[0]!["@truncated"]).toEqual({ notes: 500 });
  });

  it("handles comma-separated expand fields", () => {
    configureTruncation({ expand: "bio,notes" });
    const rows = [{ id: 1, bio: long(500), notes: long(500) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!.bio).toBe(long(500));
    expect(result[0]!.notes).toBe(long(500));
    expect(result[0]!["@truncated"]).toBeNull();
  });

  it("expand is case-insensitive", () => {
    configureTruncation({ expand: "Bio" });
    const rows = [{ id: 1, bio: long(500) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect(result[0]!.bio).toBe(long(500));
    expect(result[0]!["@truncated"]).toBeNull();
  });
});

describe("configureTruncation with custom maxLength", () => {
  it("uses custom maxLength", () => {
    configureTruncation({ maxLength: 50 });
    const rows = [{ id: 1, bio: long(60) }];
    const result = applyTruncation(rows) as Record<string, unknown>[];
    expect((result[0]!.bio as string).length).toBe(51); // 50 + ellipsis
    expect(result[0]!["@truncated"]).toEqual({ bio: 60 });
  });
});

describe("applyTruncationCompact", () => {
  it("truncates values and adds @truncated as last column", () => {
    const columns = ["id", "bio"];
    const rows = [
      [1, long(300)],
      [2, "short"],
    ] as unknown[][];
    const result = applyTruncationCompact({ columns, rows });
    expect(result.columns).toEqual(["id", "bio", "@truncated"]);
    expect((result.rows[0]![1] as string).endsWith("\u2026")).toBe(true);
    expect(result.rows[0]![2]).toEqual({ bio: 300 });
    expect(result.rows[1]![1]).toBe("short");
    expect(result.rows[1]![2]).toBeNull();
  });

  it("@truncated column is always last in columns array", () => {
    const columns = ["id", "name"];
    const rows = [[1, "Alice"]] as unknown[][];
    const result = applyTruncationCompact({ columns, rows });
    expect(result.columns.at(-1)).toBe("@truncated");
  });

  it("@truncated value is null for rows with no truncation", () => {
    const columns = ["id", "name"];
    const rows = [[1, "Alice"]] as unknown[][];
    const result = applyTruncationCompact({ columns, rows });
    expect(result.rows[0]![2]).toBeNull();
  });

  it("handles multiple truncated columns", () => {
    const columns = ["id", "bio", "notes"];
    const rows = [
      [1, long(300), long(400)],
      [2, "short", long(500)],
    ] as unknown[][];
    const result = applyTruncationCompact({ columns, rows });
    expect(result.columns).toEqual(["id", "bio", "notes", "@truncated"]);
    expect(result.rows[0]![3]).toEqual({ bio: 300, notes: 400 });
    expect(result.rows[1]![3]).toEqual({ notes: 500 });
  });

  it("respects --full bypass in compact mode", () => {
    configureTruncation({ full: true });
    const columns = ["id", "bio"];
    const rows = [[1, long(300)]] as unknown[][];
    const result = applyTruncationCompact({ columns, rows });
    expect(result.rows[0]![1]).toBe(long(300));
    expect(result.columns).toEqual(["id", "bio", "@truncated"]);
    expect(result.rows[0]![2]).toBeNull();
  });

  it("respects --expand in compact mode", () => {
    configureTruncation({ expand: "bio" });
    const columns = ["id", "bio", "notes"];
    const rows = [[1, long(300), long(400)]] as unknown[][];
    const result = applyTruncationCompact({ columns, rows });
    expect(result.rows[0]![1]).toBe(long(300));
    expect((result.rows[0]![2] as string).endsWith("\u2026")).toBe(true);
    expect(result.columns).toEqual(["id", "bio", "notes", "@truncated"]);
    expect(result.rows[0]![3]).toEqual({ notes: 400 });
  });

  it("passes through null values in compact mode", () => {
    const columns = ["id", "bio"];
    const rows = [[1, null]] as unknown[][];
    const result = applyTruncationCompact({ columns, rows });
    expect(result.rows[0]![1]).toBeNull();
    expect(result.columns).toEqual(["id", "bio", "@truncated"]);
    expect(result.rows[0]![2]).toBeNull();
  });
});
