import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { connectMysql } from "../../src/drivers/mysql";
import type { DriverConnection } from "../../src/drivers/types";

const MYSQL_URL = process.env.AGENT_SQL_MYSQL_TEST_URL;
const describeMysql = MYSQL_URL ? describe : describe.skip;

const parseUrl = (
  url: string,
): { host: string; port: number; database: string; username: string; password: string } => {
  const parsed = new URL(url);
  return {
    host: parsed.hostname,
    port: Number(parsed.port) || 3306,
    database: parsed.pathname.slice(1),
    username: parsed.username,
    password: parsed.password,
  };
};

describeMysql("MySQL driver", () => {
  describe("read-only enforcement", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectMysql({ ...parseUrl(MYSQL_URL!), readonly: true });
    });

    afterAll(async () => {
      await conn.close();
    });

    test("SELECT works", async () => {
      const result = await conn.query("SELECT id, name, email FROM users ORDER BY id");
      expect(result.columns).toEqual(["id", "name", "email"]);
      expect(result.rows.length).toBeGreaterThanOrEqual(2);
      expect(result.rows[0]).toMatchObject({ name: "Alice", email: "alice@test.com" });
    });

    test("INSERT throws", async () => {
      await expect(
        conn.query("INSERT INTO users (name, email) VALUES ('Mallory', 'mal@test.com')"),
      ).rejects.toThrow();
    });

    test("UPDATE throws", async () => {
      await expect(conn.query("UPDATE users SET name = 'Evil' WHERE id = 1")).rejects.toThrow();
    });

    test("DELETE throws", async () => {
      await expect(conn.query("DELETE FROM users WHERE id = 1")).rejects.toThrow();
    });
  });

  describe("escape futility", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectMysql({ ...parseUrl(MYSQL_URL!), readonly: true });
    });

    afterAll(async () => {
      await conn.close();
    });

    test("SET SESSION TRANSACTION READ WRITE between queries has no effect", async () => {
      // Even if an attacker manages to run SET SESSION TRANSACTION READ WRITE,
      // the next query still runs inside its own START TRANSACTION READ ONLY wrapper
      try {
        await conn.query("SET SESSION TRANSACTION READ WRITE");
      } catch {
        // May throw — that's fine too
      }

      // Next write attempt should still fail because of per-query read-only transaction
      await expect(
        conn.query("INSERT INTO users (name, email) VALUES ('Hacker', 'hack@test.com')"),
      ).rejects.toThrow();
    });
  });

  describe("write mode", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectMysql({ ...parseUrl(MYSQL_URL!), readonly: false });
    });

    afterAll(async () => {
      try {
        await conn.query("DELETE FROM posts WHERE title = 'WriteTest'", { write: true });
        await conn.query("DELETE FROM users WHERE email = 'write-test@test.com'", { write: true });
        await conn.query("DELETE FROM users WHERE email = 'replace-test@test.com'", {
          write: true,
        });
      } catch {
        // ignore cleanup errors
      }
      await conn.close();
    });

    test("INSERT works and returns rowsAffected and command", async () => {
      const result = await conn.query(
        "INSERT INTO users (name, email) VALUES ('WriteTest', 'write-test@test.com')",
        { write: true },
      );
      expect(result.rowsAffected).toBe(1);
      expect(result.command).toBe("INSERT");
    });

    test("REPLACE works in write mode", async () => {
      const result = await conn.query(
        "REPLACE INTO users (id, name, email) VALUES (9999, 'ReplaceTest', 'replace-test@test.com')",
        { write: true },
      );
      expect(result.rowsAffected).toBeGreaterThanOrEqual(1);
      expect(result.command).toBe("REPLACE");
    });
  });

  describe("schema discovery", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectMysql({ ...parseUrl(MYSQL_URL!), readonly: true });
    });

    afterAll(async () => {
      await conn.close();
    });

    test("getTables returns user tables", async () => {
      const tables = await conn.getTables();
      const names = tables.map((t) => t.name);
      expect(names).toContain("users");
      expect(names).toContain("posts");
    });

    test("describeTable returns column info for users", async () => {
      const columns = await conn.describeTable("users");
      expect(columns.length).toBeGreaterThanOrEqual(4);

      const idCol = columns.find((c) => c.name === "id")!;
      expect(idCol).toBeDefined();
      expect(idCol.primaryKey).toBe(true);
      expect(idCol.nullable).toBe(false);

      const nameCol = columns.find((c) => c.name === "name")!;
      expect(nameCol).toBeDefined();
      expect(nameCol.nullable).toBe(false);

      const emailCol = columns.find((c) => c.name === "email")!;
      expect(emailCol).toBeDefined();
      expect(emailCol.nullable).toBe(true);

      const bioCol = columns.find((c) => c.name === "bio")!;
      expect(bioCol).toBeDefined();
      expect(bioCol.nullable).toBe(true);
    });

    test("getIndexes returns indexes for posts", async () => {
      const indexes = await conn.getIndexes("posts");
      const userIdIdx = indexes.find((i) => i.columns.includes("user_id") && i.name !== "PRIMARY");
      expect(userIdIdx).toBeDefined();
      expect(userIdIdx!.name).toBe("idx_posts_user_id");
      expect(userIdIdx!.unique).toBe(false);
    });

    test("getConstraints returns PK and unique constraints for users", async () => {
      const constraints = await conn.getConstraints("users");
      const pk = constraints.find((c) => c.type === "primary_key");
      expect(pk).toBeDefined();
      expect(pk!.columns).toContain("id");

      const unique = constraints.find((c) => c.type === "unique");
      expect(unique).toBeDefined();
      expect(unique!.columns).toContain("email");
    });

    test("getConstraints returns FK on posts", async () => {
      const constraints = await conn.getConstraints("posts");
      const fk = constraints.find((c) => c.type === "foreign_key");
      expect(fk).toBeDefined();
      expect(fk!.columns).toContain("user_id");
      expect(fk!.referencedTable).toBe("users");
      expect(fk!.referencedColumns).toContain("id");
    });
  });

  describe("searchSchema", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectMysql({ ...parseUrl(MYSQL_URL!), readonly: true });
    });

    afterAll(async () => {
      await conn.close();
    });

    test("finds tables by pattern", async () => {
      const result = await conn.searchSchema("user");
      expect(result.tables.some((t) => t.name === "users")).toBe(true);
    });

    test("finds columns by pattern", async () => {
      const result = await conn.searchSchema("email");
      expect(result.columns.some((c) => c.column === "email" && c.table === "users")).toBe(true);
    });
  });

  describe("close", () => {
    test("close works without error", async () => {
      const conn = await connectMysql({ ...parseUrl(MYSQL_URL!), readonly: true });
      await conn.close();
    });
  });
});
