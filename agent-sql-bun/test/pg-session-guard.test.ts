import { describe, expect, test, beforeAll } from "bun:test";
import { validateReadOnlyQuery, loadPgParser } from "../src/lib/pg-session-guard.ts";

beforeAll(async () => {
  await loadPgParser();
});

describe("validateReadOnlyQuery", () => {
  describe("allowed queries", () => {
    test("basic SELECT", () => {
      const result = validateReadOnlyQuery("SELECT 1");
      expect(result).toEqual({ ok: true });
    });

    test("SELECT with WHERE/ORDER BY/GROUP BY/HAVING", () => {
      const sql =
        "SELECT name, COUNT(*) FROM users WHERE active = true GROUP BY name HAVING COUNT(*) > 1 ORDER BY name";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SELECT with JOINs", () => {
      const sql =
        "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id LEFT JOIN items i ON o.id = i.order_id";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SELECT with subqueries", () => {
      const sql = "SELECT * FROM users WHERE id IN (SELECT user_id FROM orders WHERE total > 100)";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SELECT with CTEs (WITH ... SELECT)", () => {
      const sql =
        "WITH active_users AS (SELECT * FROM users WHERE active = true) SELECT * FROM active_users";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("EXPLAIN SELECT", () => {
      const sql = "EXPLAIN SELECT * FROM users";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("EXPLAIN ANALYZE SELECT", () => {
      const sql = "EXPLAIN ANALYZE SELECT * FROM users WHERE id = 1";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SHOW statement", () => {
      const sql = "SHOW server_version";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("COPY ... TO STDOUT", () => {
      const sql = "COPY users TO STDOUT";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SELECT with LIMIT and OFFSET", () => {
      const sql = "SELECT * FROM users LIMIT 10 OFFSET 20";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SELECT with UNION", () => {
      const sql = "SELECT id FROM users UNION SELECT id FROM admins";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SELECT with DISTINCT", () => {
      const sql = "SELECT DISTINCT name FROM users";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SELECT with aggregate functions", () => {
      const sql = "SELECT COUNT(*), AVG(age), MAX(salary) FROM employees";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SELECT with window functions", () => {
      const sql =
        "SELECT name, salary, ROW_NUMBER() OVER (PARTITION BY dept ORDER BY salary DESC) FROM employees";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("SELECT with CASE expression", () => {
      const sql = "SELECT CASE WHEN age > 18 THEN 'adult' ELSE 'minor' END FROM users";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });

    test("dollar-quoted strings containing dangerous SQL are fine in SELECT", () => {
      const sql = "SELECT $$DROP TABLE users; DELETE FROM orders$$ AS dangerous_looking_string";
      expect(validateReadOnlyQuery(sql)).toEqual({ ok: true });
    });
  });

  describe("blocked queries", () => {
    test("INSERT", () => {
      const result = validateReadOnlyQuery("INSERT INTO users (name) VALUES ('alice')");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("InsertStmt");
      }
    });

    test("UPDATE", () => {
      const result = validateReadOnlyQuery("UPDATE users SET name = 'bob' WHERE id = 1");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("UpdateStmt");
      }
    });

    test("DELETE", () => {
      const result = validateReadOnlyQuery("DELETE FROM users WHERE id = 1");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("DeleteStmt");
      }
    });

    test("DROP TABLE", () => {
      const result = validateReadOnlyQuery("DROP TABLE users");
      expect(result.ok).toBe(false);
    });

    test("CREATE TABLE", () => {
      const result = validateReadOnlyQuery("CREATE TABLE evil (id int PRIMARY KEY)");
      expect(result.ok).toBe(false);
    });

    test("ALTER TABLE", () => {
      const result = validateReadOnlyQuery("ALTER TABLE users ADD COLUMN evil text");
      expect(result.ok).toBe(false);
    });

    test("TRUNCATE", () => {
      const result = validateReadOnlyQuery("TRUNCATE users");
      expect(result.ok).toBe(false);
    });

    test("SET default_transaction_read_only = off", () => {
      const result = validateReadOnlyQuery("SET default_transaction_read_only = off");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("VariableSetStmt");
      }
    });

    test("SET transaction_read_only = off", () => {
      const result = validateReadOnlyQuery("SET transaction_read_only = off");
      expect(result.ok).toBe(false);
    });

    test("SET LOCAL transaction_read_only = off", () => {
      const result = validateReadOnlyQuery("SET LOCAL transaction_read_only = off");
      expect(result.ok).toBe(false);
    });

    test("RESET default_transaction_read_only", () => {
      const result = validateReadOnlyQuery("RESET default_transaction_read_only");
      expect(result.ok).toBe(false);
    });

    test("RESET ALL", () => {
      const result = validateReadOnlyQuery("RESET ALL");
      expect(result.ok).toBe(false);
    });

    test("DISCARD ALL", () => {
      const result = validateReadOnlyQuery("DISCARD ALL");
      expect(result.ok).toBe(false);
    });

    test("BEGIN", () => {
      const result = validateReadOnlyQuery("BEGIN");
      expect(result.ok).toBe(false);
    });

    test("BEGIN READ WRITE", () => {
      const result = validateReadOnlyQuery("BEGIN READ WRITE");
      expect(result.ok).toBe(false);
    });

    test("LOAD 'library'", () => {
      const result = validateReadOnlyQuery("LOAD 'library'");
      expect(result.ok).toBe(false);
    });

    test("SELECT INTO", () => {
      const result = validateReadOnlyQuery("SELECT * INTO newtable FROM users");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("INTO");
      }
    });

    test("SELECT ... FOR UPDATE", () => {
      const result = validateReadOnlyQuery("SELECT * FROM users FOR UPDATE");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("locking");
      }
    });

    test("SELECT ... FOR SHARE", () => {
      const result = validateReadOnlyQuery("SELECT * FROM users FOR SHARE");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("locking");
      }
    });

    test("EXPLAIN ANALYZE DELETE FROM users", () => {
      const result = validateReadOnlyQuery("EXPLAIN ANALYZE DELETE FROM users");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("DeleteStmt");
      }
    });

    test("EXPLAIN ANALYZE INSERT INTO users", () => {
      const result = validateReadOnlyQuery("EXPLAIN ANALYZE INSERT INTO users (name) VALUES ('x')");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("InsertStmt");
      }
    });

    test("multi-statement: SELECT 1; DROP TABLE users", () => {
      const result = validateReadOnlyQuery("SELECT 1; DROP TABLE users");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("multiple statements");
      }
    });

    test("writable CTE: WITH x AS (DELETE FROM users) SELECT * FROM x", () => {
      const result = validateReadOnlyQuery(
        "WITH x AS (DELETE FROM users RETURNING *) SELECT * FROM x",
      );
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("DeleteStmt");
      }
    });

    test("COPY FROM (import, not export)", () => {
      const result = validateReadOnlyQuery("COPY users FROM STDIN");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("COPY FROM");
      }
    });

    test("CREATE INDEX", () => {
      const result = validateReadOnlyQuery("CREATE INDEX idx_users_name ON users (name)");
      expect(result.ok).toBe(false);
    });

    test("GRANT", () => {
      const result = validateReadOnlyQuery("GRANT SELECT ON users TO readonly_user");
      expect(result.ok).toBe(false);
    });

    test("DO block (anonymous code)", () => {
      const result = validateReadOnlyQuery("DO $$ BEGIN PERFORM 1; END $$");
      expect(result.ok).toBe(false);
    });

    test("CREATE FUNCTION", () => {
      const result = validateReadOnlyQuery(
        "CREATE FUNCTION evil() RETURNS void AS $$ BEGIN DELETE FROM users; END $$ LANGUAGE plpgsql",
      );
      expect(result.ok).toBe(false);
    });

    test("VACUUM", () => {
      const result = validateReadOnlyQuery("VACUUM");
      expect(result.ok).toBe(false);
    });

    test("COMMIT", () => {
      const result = validateReadOnlyQuery("COMMIT");
      expect(result.ok).toBe(false);
    });

    test("ROLLBACK", () => {
      const result = validateReadOnlyQuery("ROLLBACK");
      expect(result.ok).toBe(false);
    });
  });

  describe("error messages", () => {
    test("blocked queries return descriptive error messages", () => {
      const result = validateReadOnlyQuery("DELETE FROM users");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toBeTruthy();
        expect(result.error.length).toBeGreaterThan(10);
      }
    });

    test("multi-statement has specific message", () => {
      const result = validateReadOnlyQuery("SELECT 1; SELECT 2");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("multiple statements");
      }
    });

    test("error message mentions the blocked statement type", () => {
      const result = validateReadOnlyQuery("UPDATE users SET x = 1");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("UpdateStmt");
      }
    });

    test("SELECT INTO error mentions INTO clause", () => {
      const result = validateReadOnlyQuery("SELECT id INTO backup FROM users");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("INTO");
      }
    });

    test("locking clause error mentions locking", () => {
      const result = validateReadOnlyQuery("SELECT * FROM users FOR UPDATE");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toContain("locking");
      }
    });

    test("parse error for invalid SQL returns error", () => {
      const result = validateReadOnlyQuery("NOT VALID SQL AT ALL ???");
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error).toBeTruthy();
      }
    });

    test("empty query returns error", () => {
      const result = validateReadOnlyQuery("");
      expect(result.ok).toBe(false);
    });
  });
});
