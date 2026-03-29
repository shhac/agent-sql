import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { connectPg } from "../../src/drivers/pg";
import { configureTimeout } from "../../src/lib/timeout";
import type { DriverConnection } from "../../src/drivers/types";

const PG_URL = process.env.AGENT_SQL_PG_TEST_URL;
const describePg = PG_URL ? describe : describe.skip;

const parseUrl = (
  url: string,
): { host: string; port: number; database: string; username: string; password: string } => {
  const parsed = new URL(url);
  return {
    host: parsed.hostname,
    port: Number(parsed.port) || 5432,
    database: parsed.pathname.slice(1),
    username: parsed.username,
    password: parsed.password,
  };
};

describePg("PostgreSQL driver", () => {
  describe("read-only enforcement", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectPg({ ...parseUrl(PG_URL!), readonly: true });
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

  describe("session guard", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectPg({ ...parseUrl(PG_URL!), readonly: true });
    });

    afterAll(async () => {
      await conn.close();
    });

    test("SET default_transaction_read_only = off is blocked", async () => {
      await expect(conn.query("SET default_transaction_read_only = off")).rejects.toThrow(
        /not allowed in read-only mode/,
      );
    });

    test("DROP TABLE is blocked", async () => {
      await expect(conn.query("DROP TABLE users")).rejects.toThrow(/not allowed in read-only mode/);
    });

    test("multi-statement is blocked", async () => {
      await expect(conn.query("SELECT 1; SELECT 2")).rejects.toThrow(/multiple statements/);
    });
  });

  describe("write mode", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectPg({ ...parseUrl(PG_URL!), readonly: false });
    });

    afterAll(async () => {
      // Clean up any test-inserted rows
      try {
        await conn.query("DELETE FROM test_schema.events WHERE type = 'test_write'", {
          write: true,
        });
        await conn.query("DELETE FROM users WHERE email = 'write-test@test.com'", { write: true });
      } catch {
        // ignore cleanup errors
      }
      await conn.close();
    });

    test("INSERT works and returns rowsAffected", async () => {
      const result = await conn.query(
        "INSERT INTO users (name, email) VALUES ('WriteTest', 'write-test@test.com')",
        { write: true },
      );
      expect(result.rowsAffected).toBe(1);
      expect(result.command).toBe("INSERT");
    });

    test("UPDATE works", async () => {
      const result = await conn.query(
        "UPDATE users SET name = 'WriteTestUpdated' WHERE email = 'write-test@test.com'",
        { write: true },
      );
      expect(result.rowsAffected).toBe(1);
      expect(result.command).toBe("UPDATE");
    });

    test("DELETE works", async () => {
      const result = await conn.query("DELETE FROM users WHERE email = 'write-test@test.com'", {
        write: true,
      });
      expect(result.rowsAffected).toBe(1);
      expect(result.command).toBe("DELETE");
    });
  });

  describe("schema discovery", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectPg({ ...parseUrl(PG_URL!), readonly: true });
    });

    afterAll(async () => {
      await conn.close();
    });

    test("getTables returns user tables without system tables", async () => {
      const tables = await conn.getTables();
      const names = tables.map((t) => t.name);
      expect(names).toContain("public.users");
      expect(names).toContain("test_schema.events");
      expect(names.every((n) => !n.startsWith("pg_catalog."))).toBe(true);
      expect(names.every((n) => !n.startsWith("information_schema."))).toBe(true);
    });

    test("getTables with includeSystem returns system tables", async () => {
      const tables = await conn.getTables({ includeSystem: true });
      const names = tables.map((t) => t.name);
      expect(names.some((n) => n.startsWith("pg_catalog."))).toBe(true);
      expect(names.some((n) => n.startsWith("information_schema."))).toBe(true);
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
      expect(nameCol.type).toBe("text");
      expect(nameCol.nullable).toBe(false);

      const emailCol = columns.find((c) => c.name === "email")!;
      expect(emailCol).toBeDefined();
      expect(emailCol.nullable).toBe(true);

      const bioCol = columns.find((c) => c.name === "bio")!;
      expect(bioCol).toBeDefined();
      expect(bioCol.nullable).toBe(true);
    });

    test("getIndexes returns indexes for users", async () => {
      const indexes = await conn.getIndexes("users");
      expect(indexes.length).toBeGreaterThanOrEqual(1);
      // PK index and unique on email
      const emailIdx = indexes.find((i) => i.columns.includes("email"));
      expect(emailIdx).toBeDefined();
      expect(emailIdx!.unique).toBe(true);
    });

    test("getConstraints returns constraints for users", async () => {
      const constraints = await conn.getConstraints("users");
      const pk = constraints.find((c) => c.type === "primary_key");
      expect(pk).toBeDefined();
      expect(pk!.columns).toContain("id");

      const unique = constraints.find((c) => c.type === "unique");
      expect(unique).toBeDefined();
      expect(unique!.columns).toContain("email");
    });

    test("getConstraints returns FK on test_schema.events", async () => {
      const constraints = await conn.getConstraints("test_schema.events");
      const fk = constraints.find((c) => c.type === "foreign_key");
      expect(fk).toBeDefined();
      expect(fk!.columns).toContain("user_id");
      expect(fk!.referencedTable).toBe("public.users");
      expect(fk!.referencedColumns).toContain("id");
    });
  });

  describe("namespace handling", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await connectPg({ ...parseUrl(PG_URL!), readonly: true });
    });

    afterAll(async () => {
      await conn.close();
    });

    test("describeTable with dot notation for test_schema.events", async () => {
      const columns = await conn.describeTable("test_schema.events");
      expect(columns.length).toBeGreaterThanOrEqual(4);

      const typeCol = columns.find((c) => c.name === "type")!;
      expect(typeCol).toBeDefined();
      expect(typeCol.type).toBe("text");
      expect(typeCol.nullable).toBe(false);

      const dataCol = columns.find((c) => c.name === "data")!;
      expect(dataCol).toBeDefined();
      expect(dataCol.type).toBe("jsonb");
    });

    test("getIndexes with dot notation for test_schema.events", async () => {
      const indexes = await conn.getIndexes("test_schema.events");
      const userIdIdx = indexes.find((i) => i.name === "idx_events_user_id");
      expect(userIdIdx).toBeDefined();
      expect(userIdIdx!.columns).toContain("user_id");
    });
  });

  describe("query timeout", () => {
    test("pg_sleep exceeding timeout is cancelled", async () => {
      // Set a very short timeout before connecting so statement_timeout = 200ms
      configureTimeout(200);
      const conn = await connectPg({ ...parseUrl(PG_URL!), readonly: true });
      try {
        await expect(conn.query("SELECT pg_sleep(10)")).rejects.toThrow();
      } finally {
        await conn.close();
        // Restore default timeout for other tests
        configureTimeout(30000);
      }
    });
  });

  describe("close", () => {
    test("close works without error", async () => {
      const conn = await connectPg({ ...parseUrl(PG_URL!), readonly: true });
      await conn.close();
    });
  });
});
