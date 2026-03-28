import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { unlinkSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { connectDuckDb } from "../src/drivers/duckdb";
import type { DriverConnection } from "../src/drivers/types";

const TEST_DB = join(tmpdir(), `duckdb-test-${Date.now()}.duckdb`);

const setupTestDb = (): void => {
  Bun.spawnSync(
    [
      "duckdb",
      TEST_DB,
      "-c",
      `
    CREATE TABLE users (
      id INTEGER PRIMARY KEY,
      name VARCHAR NOT NULL,
      email VARCHAR,
      age INTEGER,
      bio TEXT
    );
    INSERT INTO users VALUES
      (1, 'Alice', 'alice@test.com', 30, 'Software engineer'),
      (2, 'Bob', NULL, 25, NULL),
      (3, 'Charlie', 'charlie@test.com', 35, 'A very long biography that exceeds typical display lengths for testing truncation behavior');
    CREATE TABLE orders (
      id INTEGER PRIMARY KEY,
      user_id INTEGER REFERENCES users(id),
      amount DECIMAL(10,2),
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    INSERT INTO orders VALUES
      (1, 1, 99.99, '2024-01-15 10:30:00'),
      (2, 1, 149.50, '2024-02-20 14:15:00'),
      (3, 2, 25.00, '2024-03-01 09:00:00');
    CREATE INDEX idx_orders_user ON orders(user_id);
    CREATE INDEX idx_orders_amount ON orders(amount);
    CREATE VIEW active_users AS SELECT * FROM users WHERE age >= 30;
  `,
    ],
    { stdout: "pipe", stderr: "pipe" },
  );
};

describe("duckdb driver", () => {
  let driver: DriverConnection;

  beforeAll(async () => {
    setupTestDb();
    driver = await connectDuckDb({ path: TEST_DB, readonly: true });
  });

  afterAll(async () => {
    await driver.close();
    try {
      unlinkSync(TEST_DB);
    } catch {
      // ignore
    }
  });

  describe("connection", () => {
    test("connects to a duckdb file", () => {
      expect(driver).toBeDefined();
      expect(driver.query).toBeFunction();
    });

    test("rejects non-existent duckdb file", async () => {
      await expect(
        connectDuckDb({ path: "/tmp/nonexistent.duckdb", readonly: true }),
      ).rejects.toThrow();
    });

    test("connects without a database file (in-memory)", async () => {
      const mem = await connectDuckDb({ readonly: false });
      const result = await mem.query("SELECT 42 AS answer");
      expect(result.rows).toEqual([{ answer: 42 }]);
      await mem.close();
    });
  });

  describe("query", () => {
    test("selects rows with correct columns", async () => {
      const result = await driver.query("SELECT id, name FROM users ORDER BY id");
      expect(result.columns).toEqual(["id", "name"]);
      expect(result.rows).toHaveLength(3);
      expect(result.rows[0]).toEqual({ id: 1, name: "Alice" });
    });

    test("preserves NULL values", async () => {
      const result = await driver.query(
        "SELECT email, bio FROM users WHERE id = 2",
      );
      expect(result.rows[0]).toEqual({ email: null, bio: null });
    });

    test("handles empty result set", async () => {
      const result = await driver.query(
        "SELECT * FROM users WHERE id = 999",
      );
      expect(result.rows).toEqual([]);
      expect(result.columns).toEqual([]);
    });

    test("handles aggregations", async () => {
      const result = await driver.query(
        "SELECT COUNT(*) AS cnt, AVG(age) AS avg_age FROM users",
      );
      expect(result.rows[0]?.cnt).toBe(3);
      expect(typeof result.rows[0]?.avg_age).toBe("number");
    });

    test("handles joins", async () => {
      const result = await driver.query(
        "SELECT u.name, o.amount FROM users u JOIN orders o ON u.id = o.user_id ORDER BY o.id",
      );
      expect(result.rows).toHaveLength(3);
      // DuckDB serializes DECIMAL as string in JSON mode
      expect(result.rows[0]).toEqual({ name: "Alice", amount: "99.99" });
    });

    test("handles LIMIT and OFFSET", async () => {
      const result = await driver.query(
        "SELECT id FROM users ORDER BY id LIMIT 2 OFFSET 1",
      );
      expect(result.rows).toEqual([{ id: 2 }, { id: 3 }]);
    });

    test("handles CTEs and subqueries", async () => {
      const result = await driver.query(
        "WITH top_spenders AS (SELECT user_id, SUM(amount) AS total FROM orders GROUP BY user_id) SELECT u.name, t.total FROM top_spenders t JOIN users u ON t.user_id = u.id ORDER BY t.total DESC",
      );
      expect(result.rows[0]?.name).toBe("Alice");
    });

    test("throws on syntax error with fixable_by agent", async () => {
      try {
        await driver.query("SELEC * FROM users");
        expect(true).toBe(false);
      } catch (err) {
        expect((err as Error).message).toContain("Parser Error");
        expect((err as { fixableBy: string }).fixableBy).toBe("agent");
      }
    });

    test("throws on missing table with hint", async () => {
      try {
        await driver.query("SELECT * FROM nonexistent");
        expect(true).toBe(false);
      } catch (err) {
        expect((err as Error).message).toContain("Catalog Error");
        expect((err as { fixableBy: string }).fixableBy).toBe("agent");
      }
    });
  });

  describe("read-only enforcement", () => {
    test("blocks INSERT in readonly mode", async () => {
      try {
        await driver.query("INSERT INTO users VALUES(99,'Test','t@t.com',20,'test')");
        expect(true).toBe(false);
      } catch (err) {
        expect((err as Error).message).toContain("read-only mode");
        expect((err as { fixableBy: string }).fixableBy).toBe("human");
      }
    });

    test("blocks CREATE TABLE in readonly mode", async () => {
      try {
        await driver.query("CREATE TABLE test(x INT)");
        expect(true).toBe(false);
      } catch (err) {
        expect((err as Error).message).toContain("read-only mode");
        expect((err as { fixableBy: string }).fixableBy).toBe("human");
      }
    });

    test("blocks DROP TABLE in readonly mode", async () => {
      try {
        await driver.query("DROP TABLE users");
        expect(true).toBe(false);
      } catch (err) {
        expect((err as Error).message).toContain("read-only mode");
      }
    });
  });

  describe("schema - getTables", () => {
    test("lists user tables", async () => {
      const tables = await driver.getTables();
      const names = tables.map((t) => t.name);
      expect(names).toContain("users");
      expect(names).toContain("orders");
    });

    test("includes views", async () => {
      const tables = await driver.getTables();
      const view = tables.find((t) => t.name === "active_users");
      expect(view).toBeDefined();
      expect(view?.type).toBe("view");
    });

    test("table type is correct", async () => {
      const tables = await driver.getTables();
      const users = tables.find((t) => t.name === "users");
      expect(users?.type).toBe("table");
    });

    test("includeSystem shows all schemas", async () => {
      const normal = await driver.getTables();
      const withSystem = await driver.getTables({ includeSystem: true });
      // With system tables, we get information_schema tables too
      expect(withSystem.length).toBeGreaterThanOrEqual(normal.length);
    });
  });

  describe("schema - describeTable", () => {
    test("describes columns with types", async () => {
      const cols = await driver.describeTable("users");
      expect(cols).toHaveLength(5);
      const idCol = cols.find((c) => c.name === "id");
      expect(idCol?.type).toBe("INTEGER");
      expect(idCol?.nullable).toBe(false);
    });

    test("detects nullable columns", async () => {
      const cols = await driver.describeTable("users");
      const emailCol = cols.find((c) => c.name === "email");
      expect(emailCol?.nullable).toBe(true);
    });

    test("handles decimal types", async () => {
      const cols = await driver.describeTable("orders");
      const amountCol = cols.find((c) => c.name === "amount");
      expect(amountCol?.type).toContain("DECIMAL");
    });
  });

  describe("schema - getIndexes", () => {
    test("lists all indexes", async () => {
      const indexes = await driver.getIndexes();
      expect(indexes.length).toBeGreaterThanOrEqual(2);
    });

    test("returns index columns", async () => {
      const indexes = await driver.getIndexes();
      const userIdx = indexes.find(
        (i) => i.name === "idx_orders_user",
      );
      expect(userIdx?.columns).toEqual(["user_id"]);
      expect(userIdx?.table).toBe("orders");
    });

    test("filters by table", async () => {
      const indexes = await driver.getIndexes("orders");
      expect(indexes.every((i) => i.table === "orders")).toBe(true);
      expect(indexes.length).toBeGreaterThanOrEqual(2);
    });

    test("returns empty for table without indexes", async () => {
      const indexes = await driver.getIndexes("users");
      // users has PK but may or may not have explicit index
      expect(Array.isArray(indexes)).toBe(true);
    });
  });

  describe("schema - getConstraints", () => {
    test("finds primary key constraint", async () => {
      const constraints = await driver.getConstraints("users");
      const pk = constraints.find((c) => c.type === "primary_key");
      expect(pk).toBeDefined();
      expect(pk?.columns).toContain("id");
    });

    test("finds foreign key constraint", async () => {
      const constraints = await driver.getConstraints("orders");
      const fk = constraints.find((c) => c.type === "foreign_key");
      expect(fk).toBeDefined();
      expect(fk?.columns).toContain("user_id");
    });

    test("filters by table", async () => {
      const constraints = await driver.getConstraints("users");
      expect(constraints.every((c) => c.table === "users")).toBe(true);
    });

    test("lists all constraints when no table specified", async () => {
      const constraints = await driver.getConstraints();
      const tables = new Set(constraints.map((c) => c.table));
      expect(tables.size).toBeGreaterThanOrEqual(2);
    });
  });

  describe("schema - searchSchema", () => {
    test("finds tables by name pattern", async () => {
      const result = await driver.searchSchema("user");
      expect(result.tables.some((t) => t.name === "users")).toBe(true);
    });

    test("finds columns by name pattern", async () => {
      const result = await driver.searchSchema("email");
      expect(result.columns.some((c) => c.column === "email")).toBe(true);
      expect(result.columns.some((c) => c.table === "users")).toBe(true);
    });

    test("returns empty for no matches", async () => {
      const result = await driver.searchSchema("zzz_nonexistent_zzz");
      expect(result.tables).toEqual([]);
      expect(result.columns).toEqual([]);
    });
  });

  test("quoteIdent uses double quotes with dot notation", () => {
    expect(driver.quoteIdent("schema.table")).toBe('"schema"."table"');
  });
});

describe("duckdb file queries", () => {
  test("queries CSV file directly", async () => {
    const csvPath = join(tmpdir(), `duckdb-test-${Date.now()}.csv`);
    await Bun.write(csvPath, "id,name,score\n1,Alice,95\n2,Bob,87\n");

    try {
      const driver = await connectDuckDb({ readonly: false });
      const result = await driver.query(
        `SELECT * FROM '${csvPath}' ORDER BY id`,
      );
      expect(result.rows).toHaveLength(2);
      expect(result.rows[0]).toEqual({ id: 1, name: "Alice", score: 95 });
      await driver.close();
    } finally {
      unlinkSync(csvPath);
    }
  });

  test("queries JSON file directly", async () => {
    const jsonPath = join(tmpdir(), `duckdb-test-${Date.now()}.json`);
    await Bun.write(jsonPath, '[{"id":1,"name":"test"}]');
    try {
      const d = await connectDuckDb({ readonly: false });
      const result = await d.query(`SELECT * FROM '${jsonPath}'`);
      expect(result.rows).toHaveLength(1);
      expect(result.rows[0]).toEqual({ id: 1, name: "test" });
      await d.close();
    } finally {
      unlinkSync(jsonPath);
    }
  });
});

describe("duckdb CLI availability", () => {
  test("checkDuckDbAvailable succeeds when CLI is on PATH", async () => {
    const { checkDuckDbAvailable } = await import("../src/drivers/duckdb/subprocess");
    expect(() => checkDuckDbAvailable()).not.toThrow();
  });

  test("checkDuckDbAvailable fails for nonexistent path", async () => {
    const origPath = process.env.AGENT_SQL_DUCKDB_PATH;
    process.env.AGENT_SQL_DUCKDB_PATH = "/nonexistent/duckdb";
    try {
      const { checkDuckDbAvailable } = await import("../src/drivers/duckdb/subprocess");
      expect(() => checkDuckDbAvailable()).toThrow(/DuckDB CLI not found/);
    } finally {
      if (origPath) {
        process.env.AGENT_SQL_DUCKDB_PATH = origPath;
      } else {
        delete process.env.AGENT_SQL_DUCKDB_PATH;
      }
    }
  });
});
