import { describe, test, expect } from "bun:test";
import { validateReadOnly } from "../../src/drivers/snowflake/read-only-guard";

describe("validateReadOnly", () => {
  const allowed = [
    "SELECT * FROM users",
    "WITH cte AS (SELECT 1) SELECT * FROM cte",
    "SHOW TABLES",
    "DESCRIBE users",
    "DESC users",
    "EXPLAIN SELECT 1",
    "LIST @my_stage",
    "LS @my_stage",
  ];

  for (const sql of allowed) {
    test(`allows: ${sql.slice(0, 40)}`, () => {
      expect(() => validateReadOnly(sql)).not.toThrow();
    });
  }

  test("allows with leading whitespace", () => {
    expect(() => validateReadOnly("  SELECT 1")).not.toThrow();
    expect(() => validateReadOnly("\t\nSHOW TABLES")).not.toThrow();
  });

  test("SELECT followed by space, tab, newline, or paren", () => {
    expect(() => validateReadOnly("SELECT 1")).not.toThrow();
    expect(() => validateReadOnly("SELECT\t1")).not.toThrow();
    expect(() => validateReadOnly("SELECT\n1")).not.toThrow();
    expect(() => validateReadOnly("SELECT(1)")).not.toThrow();
  });

  const blocked = [
    "INSERT INTO users VALUES (1)",
    "UPDATE users SET name = 'x'",
    "DELETE FROM users",
    "CREATE TABLE t (id INT)",
    "ALTER TABLE t ADD COLUMN x INT",
    "DROP TABLE t",
    "TRUNCATE TABLE t",
    "MERGE INTO t USING s ON t.id = s.id",
    "COPY INTO t FROM @stage",
    "PUT file:///tmp/f @stage",
    "GET @stage file:///tmp/",
    "GRANT SELECT ON t TO role_x",
    "REVOKE SELECT ON t FROM role_x",
    "CALL my_procedure()",
  ];

  for (const sql of blocked) {
    test(`blocks: ${sql.slice(0, 40)}`, () => {
      expect(() => validateReadOnly(sql)).toThrow();
    });
  }

  test("blocked statements throw with fixableBy: human", () => {
    try {
      validateReadOnly("INSERT INTO t VALUES (1)");
      expect.unreachable("should have thrown");
    } catch (err: unknown) {
      const e = err as Error & { fixableBy?: string };
      expect(e.fixableBy).toBe("human");
      expect(e.message).toContain("INSERT");
      expect(e.message).toContain("not allowed in read-only mode");
    }
  });

  test("SELECTIVE does not match SELECT", () => {
    expect(() => validateReadOnly("SELECTIVE something")).toThrow();
  });

  test("case insensitive", () => {
    expect(() => validateReadOnly("select 1")).not.toThrow();
    expect(() => validateReadOnly("Select * FROM t")).not.toThrow();
    expect(() => validateReadOnly("show tables")).not.toThrow();
    expect(() => validateReadOnly("describe t")).not.toThrow();
  });
});
