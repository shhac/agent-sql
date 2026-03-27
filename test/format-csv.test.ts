import { describe, expect, it } from "bun:test";
import { formatCsv } from "../src/lib/format-csv.ts";

describe("formatCsv", () => {
  it("formats a simple query result", () => {
    const result = formatCsv({
      columns: ["id", "name"],
      rows: [
        { id: 1, name: "Alice" },
        { id: 2, name: "Bob" },
      ],
    });
    expect(result).toBe("id,name\n1,Alice\n2,Bob\n");
  });

  it("preserves column order from columns array", () => {
    const result = formatCsv({
      columns: ["name", "id", "email"],
      rows: [{ id: 1, name: "Alice", email: "alice@example.com" }],
    });
    expect(result).toBe("name,id,email\nAlice,1,alice@example.com\n");
  });

  it("renders null values as empty fields", () => {
    const result = formatCsv({
      columns: ["id", "name", "bio"],
      rows: [{ id: 1, name: null, bio: null }],
    });
    expect(result).toBe("id,name,bio\n1,,\n");
  });

  it("quotes strings containing commas", () => {
    const result = formatCsv({
      columns: ["id", "address"],
      rows: [{ id: 1, address: "123 Main St, Apt 4" }],
    });
    expect(result).toBe('id,address\n1,"123 Main St, Apt 4"\n');
  });

  it("quotes strings containing newlines", () => {
    const result = formatCsv({
      columns: ["id", "notes"],
      rows: [{ id: 1, notes: "line1\nline2" }],
    });
    expect(result).toBe('id,notes\n1,"line1\nline2"\n');
  });

  it("escapes and quotes strings containing double quotes", () => {
    const result = formatCsv({
      columns: ["id", "quote"],
      rows: [{ id: 1, quote: 'He said "hello"' }],
    });
    expect(result).toBe('id,quote\n1,"He said ""hello"""\n');
  });

  it("renders numeric values without quotes", () => {
    const result = formatCsv({
      columns: ["id", "score", "rating"],
      rows: [{ id: 1, score: 99.5, rating: -3 }],
    });
    expect(result).toBe("id,score,rating\n1,99.5,-3\n");
  });

  it("handles mixed types in a single row", () => {
    const result = formatCsv({
      columns: ["id", "name", "active", "score", "notes"],
      rows: [{ id: 42, name: "Bob, Jr.", active: true, score: 3.14, notes: null }],
    });
    expect(result).toBe('id,name,active,score,notes\n42,"Bob, Jr.",true,3.14,\n');
  });

  it("renders booleans as true/false strings", () => {
    const result = formatCsv({
      columns: ["a", "b"],
      rows: [{ a: true, b: false }],
    });
    expect(result).toBe("a,b\ntrue,false\n");
  });

  it("renders empty result set as header only with trailing newline", () => {
    const result = formatCsv({
      columns: ["id", "name"],
      rows: [],
    });
    expect(result).toBe("id,name\n");
  });

  it("renders a single row correctly", () => {
    const result = formatCsv({
      columns: ["x"],
      rows: [{ x: 42 }],
    });
    expect(result).toBe("x\n42\n");
  });

  it("serializes @truncated column as JSON", () => {
    const result = formatCsv({
      columns: ["id", "bio", "@truncated"],
      rows: [{ id: 1, bio: "short", "@truncated": { bio: 500 } }],
    });
    expect(result).toBe('id,bio,@truncated\n1,short,"{""bio"":500}"\n');
  });

  it("serializes arrays as JSON", () => {
    const result = formatCsv({
      columns: ["id", "tags"],
      rows: [{ id: 1, tags: ["a", "b"] }],
    });
    expect(result).toBe('id,tags\n1,"[""a"",""b""]"\n');
  });

  it("serializes nested objects as JSON", () => {
    const result = formatCsv({
      columns: ["id", "meta"],
      rows: [{ id: 1, meta: { key: "val" } }],
    });
    expect(result).toBe('id,meta\n1,"{""key"":""val""}"\n');
  });

  it("quotes header fields that contain special characters", () => {
    const result = formatCsv({
      columns: ["id", "user,name", 'say "hi"'],
      rows: [{ id: 1, "user,name": "Alice", 'say "hi"': "yo" }],
    });
    expect(result).toBe('id,"user,name","say ""hi"""\n1,Alice,yo\n');
  });

  it("handles undefined values as empty fields", () => {
    const result = formatCsv({
      columns: ["id", "missing"],
      rows: [{ id: 1 }],
    });
    expect(result).toBe("id,missing\n1,\n");
  });
});
