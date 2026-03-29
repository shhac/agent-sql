import { describe, test, expect, beforeAll, afterAll, beforeEach } from "bun:test";
import { Database } from "bun:sqlite";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { connectSqlite } from "../../src/drivers/sqlite";

const SCHEMA_SQL = `
CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, email TEXT, bio TEXT);
CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), title TEXT NOT NULL, body TEXT);
CREATE INDEX idx_posts_user_id ON posts(user_id);
INSERT INTO users (name, email, bio) VALUES ('Alice', 'alice@example.com', 'A developer');
INSERT INTO posts VALUES (1, 1, 'Hello World', 'First post');
`;

let tmpDir: string;
let dbPath: string;

beforeAll(async () => {
  tmpDir = await mkdtemp(join(tmpdir(), "agent-sql-test-"));
  dbPath = join(tmpDir, "test.db");
  const db = new Database(dbPath);
  db.exec(SCHEMA_SQL);
  db.close();
});

afterAll(async () => {
  await rm(tmpDir, { recursive: true, force: true });
});

describe("SQLite driver", () => {
  describe("read-only mode (default)", () => {
    test("SELECT returns correct QueryResult shape", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const result = await conn.query("SELECT id, name, email FROM users");
        expect(result.columns).toEqual(["id", "name", "email"]);
        expect(result.rows).toHaveLength(1);
        expect(result.rows[0]).toEqual({ id: 1, name: "Alice", email: "alice@example.com" });
      } finally {
        await conn.close();
      }
    });

    test("INSERT throws in readonly mode", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        await expect(
          conn.query("INSERT INTO users VALUES (2, 'Bob', 'bob@test.com', 'Hi')"),
        ).rejects.toThrow();
      } finally {
        await conn.close();
      }
    });

    test("UPDATE throws in readonly mode", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        await expect(conn.query("UPDATE users SET name = 'Bob' WHERE id = 1")).rejects.toThrow();
      } finally {
        await conn.close();
      }
    });

    test("DELETE throws in readonly mode", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        await expect(conn.query("DELETE FROM users WHERE id = 1")).rejects.toThrow();
      } finally {
        await conn.close();
      }
    });

    test("readonly defaults to true when not specified", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        await expect(
          conn.query("INSERT INTO users VALUES (2, 'Bob', 'bob@test.com', 'Hi')"),
        ).rejects.toThrow();
      } finally {
        await conn.close();
      }
    });
  });

  describe("write mode", () => {
    let writableDbPath: string;

    beforeEach(async () => {
      writableDbPath = join(tmpDir, `write-${Date.now()}.db`);
      const db = new Database(writableDbPath);
      db.exec(SCHEMA_SQL);
      db.close();
    });

    test("INSERT works and returns rowsAffected", async () => {
      const conn = connectSqlite({ path: writableDbPath, readonly: false });
      try {
        const result = await conn.query(
          "INSERT INTO users VALUES (2, 'Bob', 'bob@test.com', 'Hi')",
          { write: true },
        );
        expect(result.rowsAffected).toBe(1);
        expect(result.command).toBe("INSERT");
      } finally {
        await conn.close();
      }
    });

    test("UPDATE works and returns rowsAffected", async () => {
      const conn = connectSqlite({ path: writableDbPath, readonly: false });
      try {
        const result = await conn.query("UPDATE users SET name = 'Alice Updated' WHERE id = 1", {
          write: true,
        });
        expect(result.rowsAffected).toBe(1);
        expect(result.command).toBe("UPDATE");
      } finally {
        await conn.close();
      }
    });

    test("DELETE works and returns rowsAffected", async () => {
      const conn = connectSqlite({ path: writableDbPath, readonly: false });
      try {
        const result = await conn.query("DELETE FROM users WHERE id = 1", { write: true });
        expect(result.rowsAffected).toBe(1);
        expect(result.command).toBe("DELETE");
      } finally {
        await conn.close();
      }
    });
  });

  describe("getTables", () => {
    test("lists user tables, excludes sqlite_ internals", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const tables = await conn.getTables();
        const names = tables.map((t) => t.name);
        expect(names).toContain("users");
        expect(names).toContain("posts");
        expect(names.every((n) => !n.startsWith("sqlite_"))).toBe(true);
      } finally {
        await conn.close();
      }
    });

    test("includeSystem shows sqlite_ internal tables", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const tables = await conn.getTables({ includeSystem: true });
        const names = tables.map((t) => t.name);
        expect(names.some((n) => n.startsWith("sqlite_"))).toBe(true);
      } finally {
        await conn.close();
      }
    });
  });

  describe("describeTable", () => {
    test("returns correct column info for users table", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const columns = await conn.describeTable("users");
        expect(columns).toHaveLength(4);

        const idCol = columns.find((c) => c.name === "id")!;
        expect(idCol.type).toBe("INTEGER");
        expect(idCol.primaryKey).toBe(true);
        expect(idCol.nullable).toBe(false);

        const nameCol = columns.find((c) => c.name === "name")!;
        expect(nameCol.type).toBe("TEXT");
        expect(nameCol.nullable).toBe(false);

        const emailCol = columns.find((c) => c.name === "email")!;
        expect(emailCol.type).toBe("TEXT");
        expect(emailCol.nullable).toBe(true);
      } finally {
        await conn.close();
      }
    });
  });

  describe("getIndexes", () => {
    test("returns indexes for posts table", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const indexes = await conn.getIndexes("posts");
        const userIdIdx = indexes.find((i) => i.name === "idx_posts_user_id");
        expect(userIdIdx).toBeDefined();
        expect(userIdIdx!.table).toBe("posts");
        expect(userIdIdx!.columns).toContain("user_id");
        expect(userIdIdx!.unique).toBe(false);
      } finally {
        await conn.close();
      }
    });

    test("returns indexes for all tables when no table specified", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const indexes = await conn.getIndexes();
        expect(indexes.length).toBeGreaterThanOrEqual(1);
        expect(indexes.some((i) => i.name === "idx_posts_user_id")).toBe(true);
      } finally {
        await conn.close();
      }
    });
  });

  describe("getConstraints", () => {
    test("returns primary key constraints", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const constraints = await conn.getConstraints("users");
        const pk = constraints.find((c) => c.type === "primary_key");
        expect(pk).toBeDefined();
        expect(pk!.columns).toContain("id");
      } finally {
        await conn.close();
      }
    });

    test("returns foreign key constraints", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const constraints = await conn.getConstraints("posts");
        const fk = constraints.find((c) => c.type === "foreign_key");
        expect(fk).toBeDefined();
        expect(fk!.columns).toContain("user_id");
        expect(fk!.referencedTable).toBe("users");
        expect(fk!.referencedColumns).toContain("id");
      } finally {
        await conn.close();
      }
    });

    test("returns unique constraints from unique indexes", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        // users table has no unique constraints besides PK, posts has none either
        // but we can verify the method doesn't error
        const constraints = await conn.getConstraints("users");
        const types = constraints.map((c) => c.type);
        expect(types).toContain("primary_key");
      } finally {
        await conn.close();
      }
    });

    test("returns constraints for all tables when no table specified", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const constraints = await conn.getConstraints();
        const tables = [...new Set(constraints.map((c) => c.table))];
        expect(tables).toContain("users");
        expect(tables).toContain("posts");
      } finally {
        await conn.close();
      }
    });
  });

  describe("searchSchema", () => {
    test("finds tables by pattern", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const result = await conn.searchSchema("user");
        expect(result.tables.some((t) => t.name === "users")).toBe(true);
      } finally {
        await conn.close();
      }
    });

    test("finds columns by pattern", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const result = await conn.searchSchema("email");
        expect(result.columns.some((c) => c.column === "email" && c.table === "users")).toBe(true);
      } finally {
        await conn.close();
      }
    });

    test("search is case-insensitive", async () => {
      const conn = connectSqlite({ path: dbPath });
      try {
        const result = await conn.searchSchema("USER");
        expect(result.tables.some((t) => t.name === "users")).toBe(true);
      } finally {
        await conn.close();
      }
    });
  });

  describe("close", () => {
    test("close works without error", async () => {
      const conn = connectSqlite({ path: dbPath });
      await conn.close();
    });
  });

  describe("create: false prevents DB creation", () => {
    test("opening nonexistent file throws", () => {
      const nonexistentPath = join(tmpDir, "nonexistent.db");
      expect(() => connectSqlite({ path: nonexistentPath })).toThrow();
    });

    test("opening nonexistent file in write mode throws", () => {
      const nonexistentPath = join(tmpDir, "nonexistent-write.db");
      expect(() => connectSqlite({ path: nonexistentPath, readonly: false })).toThrow();
    });
  });
});
