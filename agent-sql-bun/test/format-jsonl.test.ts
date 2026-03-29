import { describe, test, expect } from "bun:test";
import { formatJsonl } from "../src/lib/format-jsonl.ts";

const base = { columns: [] as string[], hasMore: false, rowCount: 0 };

describe("formatJsonl", () => {
  test("single row — outputs one JSON line ending with newline", () => {
    const result = formatJsonl({
      ...base,
      columns: ["id", "name"],
      rows: [{ id: 1, name: "alice" }],
      rowCount: 1,
    });
    expect(result).toBe('{"id":1,"name":"alice"}\n');
  });

  test("multiple rows — one JSON object per line, each parseable independently", () => {
    const rows = [
      { id: 1, name: "alice" },
      { id: 2, name: "bob" },
      { id: 3, name: "charlie" },
    ];
    const result = formatJsonl({
      ...base,
      columns: ["id", "name"],
      rows,
      rowCount: 3,
    });
    const lines = result.trimEnd().split("\n");
    expect(lines).toHaveLength(3);
    for (const [i, line] of lines.entries()) {
      expect(JSON.parse(line)).toEqual(rows[i]);
    }
  });

  test("rows with null values — nulls preserved", () => {
    const result = formatJsonl({
      ...base,
      columns: ["id", "email"],
      rows: [{ id: 1, email: null }],
      rowCount: 1,
    });
    const parsed = JSON.parse(result.trimEnd());
    expect(parsed.email).toBeNull();
  });

  test("rows with @truncated — included in each line", () => {
    const result = formatJsonl({
      ...base,
      columns: ["id", "bio", "@truncated"],
      rows: [{ id: 1, bio: "short...", "@truncated": { bio: 5000 } }],
      rowCount: 1,
    });
    const parsed = JSON.parse(result.trimEnd());
    expect(parsed["@truncated"]).toEqual({ bio: 5000 });
  });

  test("hasMore=true — adds @pagination metadata line at end", () => {
    const result = formatJsonl({
      columns: ["id"],
      rows: [{ id: 1 }],
      hasMore: true,
      rowCount: 100,
    });
    const lines = result.trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    const meta = JSON.parse(lines[1]!);
    expect(meta).toEqual({ "@pagination": { hasMore: true, rowCount: 100 } });
  });

  test("hasMore=false — no @pagination line", () => {
    const result = formatJsonl({
      ...base,
      columns: ["id"],
      rows: [{ id: 1 }, { id: 2 }],
      rowCount: 2,
    });
    const lines = result.trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    for (const line of lines) {
      const parsed = JSON.parse(line);
      expect(parsed["@pagination"]).toBeUndefined();
    }
  });

  test("empty rows — returns empty string", () => {
    const result = formatJsonl({ ...base, columns: ["id"], rows: [] });
    expect(result).toBe("");
  });

  test("round-trip — each line can be JSON.parsed back", () => {
    const rows = [
      { id: 1, name: "alice", active: true },
      { id: 2, name: "bob", active: false },
    ];
    const result = formatJsonl({
      columns: ["id", "name", "active"],
      rows,
      hasMore: true,
      rowCount: 50,
    });
    const lines = result.trimEnd().split("\n");
    expect(lines).toHaveLength(3);
    expect(JSON.parse(lines[0]!)).toEqual(rows[0]);
    expect(JSON.parse(lines[1]!)).toEqual(rows[1]);
    expect(JSON.parse(lines[2]!)).toEqual({
      "@pagination": { hasMore: true, rowCount: 50 },
    });
  });

  test("special characters in values — properly escaped", () => {
    const result = formatJsonl({
      ...base,
      columns: ["msg"],
      rows: [{ msg: 'line1\nline2\t"quoted"\\back' }],
      rowCount: 1,
    });
    const parsed = JSON.parse(result.trimEnd());
    expect(parsed.msg).toBe('line1\nline2\t"quoted"\\back');
  });

  test("numeric and boolean values — not stringified", () => {
    const result = formatJsonl({
      ...base,
      columns: ["count", "rate", "active"],
      rows: [{ count: 42, rate: 3.14, active: true }],
      rowCount: 1,
    });
    const parsed = JSON.parse(result.trimEnd());
    expect(parsed.count).toBe(42);
    expect(parsed.rate).toBe(3.14);
    expect(parsed.active).toBe(true);
  });
});
