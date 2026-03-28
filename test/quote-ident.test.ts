import { describe, test, expect } from "bun:test";
import { quoteIdentPg, quoteIdentMysql } from "../src/lib/quote-ident";

describe("quoteIdentPg", () => {
  test("simple name is double-quoted", () => {
    expect(quoteIdentPg("name")).toBe('"name"');
  });

  test("dot notation quotes each part separately", () => {
    expect(quoteIdentPg("schema.table")).toBe('"schema"."table"');
  });

  test("double quotes in name are escaped", () => {
    expect(quoteIdentPg('col"name')).toBe('"col""name"');
  });

  test("multiple dots quote all parts", () => {
    expect(quoteIdentPg("a.b.c")).toBe('"a"."b"."c"');
  });
});

describe("quoteIdentMysql", () => {
  test("simple name is backtick-quoted", () => {
    expect(quoteIdentMysql("name")).toBe("`name`");
  });

  test("backticks in name are escaped", () => {
    expect(quoteIdentMysql("col`name")).toBe("`col``name`");
  });
});
