import { describe, test, expect } from "bun:test";
import { parseRows, extractColumns } from "../../src/drivers/snowflake/parse-results";
import type { SnowflakeColumnType } from "../../src/drivers/snowflake/types";

type ColOpts = { name: string; type: string; scale?: number };

const col = (opts: ColOpts): SnowflakeColumnType => ({
  name: opts.name,
  type: opts.type,
  nullable: true,
  scale: opts.scale,
});

describe("parseRows", () => {
  test("integer parsing (fixed type, scale=0) returns number", () => {
    const rows = parseRows([["42"]], [col({ name: "id", type: "fixed", scale: 0 })]);
    expect(rows).toEqual([{ id: 42 }]);
  });

  test("float parsing (fixed type, scale>0) returns number", () => {
    const rows = parseRows([["3.14"]], [col({ name: "price", type: "fixed", scale: 2 })]);
    expect(rows).toEqual([{ price: 3.14 }]);
  });

  test("real/double returns number", () => {
    const realRows = parseRows([["1.5"]], [col({ name: "val", type: "real" })]);
    expect(realRows).toEqual([{ val: 1.5 }]);

    const doubleRows = parseRows([["2.7"]], [col({ name: "val", type: "double" })]);
    expect(doubleRows).toEqual([{ val: 2.7 }]);

    const floatRows = parseRows([["9.9"]], [col({ name: "val", type: "float" })]);
    expect(floatRows).toEqual([{ val: 9.9 }]);
  });

  test("text/varchar returns string", () => {
    const rows = parseRows([["hello"]], [col({ name: "name", type: "text" })]);
    expect(rows).toEqual([{ name: "hello" }]);

    const varcharRows = parseRows([["world"]], [col({ name: "name", type: "varchar" })]);
    expect(varcharRows).toEqual([{ name: "world" }]);
  });

  test("boolean parsing", () => {
    const rows = parseRows([["true"], ["false"], ["1"]], [col({ name: "flag", type: "boolean" })]);
    expect(rows).toEqual([{ flag: true }, { flag: false }, { flag: true }]);
  });

  test("date/timestamp types stay as strings", () => {
    const types = ["date", "time", "timestamp_ltz", "timestamp_ntz", "timestamp_tz"];
    for (const t of types) {
      const rows = parseRows([["2024-01-15 10:30:00"]], [col({ name: "ts", type: t })]);
      expect(rows).toEqual([{ ts: "2024-01-15 10:30:00" }]);
    }
  });

  test("variant/object/array are JSON parsed", () => {
    const variantRows = parseRows([['{"a":1}']], [col({ name: "data", type: "variant" })]);
    expect(variantRows).toEqual([{ data: { a: 1 } }]);

    const arrayRows = parseRows([["[1,2,3]"]], [col({ name: "arr", type: "array" })]);
    expect(arrayRows).toEqual([{ arr: [1, 2, 3] }]);

    const objectRows = parseRows([['{"key":"val"}']], [col({ name: "obj", type: "object" })]);
    expect(objectRows).toEqual([{ obj: { key: "val" } }]);
  });

  test("variant with invalid JSON falls back to string", () => {
    const rows = parseRows([["not json"]], [col({ name: "data", type: "variant" })]);
    expect(rows).toEqual([{ data: "not json" }]);
  });

  test("null values are preserved", () => {
    const rows = parseRows(
      [[null, "hello"]],
      [col({ name: "a", type: "text" }), col({ name: "b", type: "text" })],
    );
    expect(rows).toEqual([{ a: null, b: "hello" }]);
  });

  test("large integers beyond MAX_SAFE_INTEGER stay as strings", () => {
    const big = "9007199254740993"; // MAX_SAFE_INTEGER + 2
    const rows = parseRows([[big]], [col({ name: "id", type: "fixed", scale: 0 })]);
    expect(rows).toEqual([{ id: big }]);
  });

  test("mixed types in single result", () => {
    const rowType = [
      col({ name: "id", type: "fixed", scale: 0 }),
      col({ name: "name", type: "text" }),
      col({ name: "active", type: "boolean" }),
      col({ name: "score", type: "real" }),
    ];
    const rows = parseRows([["1", "alice", "true", "9.5"]], rowType);
    expect(rows).toEqual([{ id: 1, name: "alice", active: true, score: 9.5 }]);
  });

  test("empty data array returns empty array", () => {
    const rows = parseRows([], [col({ name: "id", type: "fixed" })]);
    expect(rows).toEqual([]);
  });
});

describe("extractColumns", () => {
  test("returns column names from rowType", () => {
    const rowType = [
      col({ name: "id", type: "fixed" }),
      col({ name: "name", type: "text" }),
      col({ name: "created_at", type: "timestamp_ntz" }),
    ];
    expect(extractColumns(rowType)).toEqual(["id", "name", "created_at"]);
  });

  test("returns empty array for empty rowType", () => {
    expect(extractColumns([])).toEqual([]);
  });
});
