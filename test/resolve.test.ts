import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { writeFileSync, unlinkSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  detectDriverFromUrl,
  isConnectionUrl,
  isFilePath,
  resolveDriver,
} from "../src/drivers/resolve";

describe("detectDriverFromUrl", () => {
  test("detects postgres:// URLs", () => {
    expect(detectDriverFromUrl("postgres://localhost/db")).toBe("pg");
  });

  test("detects postgresql:// URLs", () => {
    expect(detectDriverFromUrl("postgresql://localhost/db")).toBe("pg");
  });

  test("detects mysql:// URLs", () => {
    expect(detectDriverFromUrl("mysql://localhost/db")).toBe("mysql");
  });

  test("detects mariadb:// URLs as mysql", () => {
    expect(detectDriverFromUrl("mariadb://localhost/db")).toBe("mysql");
  });

  test("detects sqlite:// URLs", () => {
    expect(detectDriverFromUrl("sqlite:///path/to/db")).toBe("sqlite");
  });

  test("detects .sqlite file extension", () => {
    expect(detectDriverFromUrl("/data/app.sqlite")).toBe("sqlite");
  });

  test("detects .db file extension", () => {
    expect(detectDriverFromUrl("/data/app.db")).toBe("sqlite");
  });

  test("detects .sqlite3 file extension", () => {
    expect(detectDriverFromUrl("/data/app.sqlite3")).toBe("sqlite");
  });

  test("detects .db3 file extension", () => {
    expect(detectDriverFromUrl("/data/app.db3")).toBe("sqlite");
  });

  test("detects snowflake:// URLs", () => {
    expect(detectDriverFromUrl("snowflake://myorg-myaccount/DB/SCHEMA")).toBe("snowflake");
  });

  test("detects cockroachdb:// URLs", () => {
    expect(detectDriverFromUrl("cockroachdb://localhost:26257/mydb")).toBe("cockroachdb");
  });

  test("detects duckdb:// URLs", () => {
    expect(detectDriverFromUrl("duckdb:///path/to/db.duckdb")).toBe("duckdb");
  });

  test("detects .duckdb file extension", () => {
    expect(detectDriverFromUrl("/data/app.duckdb")).toBe("duckdb");
  });

  test("returns undefined for unrecognized URLs", () => {
    expect(detectDriverFromUrl("http://example.com")).toBeUndefined();
  });

  test("file extension detection is case-insensitive", () => {
    expect(detectDriverFromUrl("/data/APP.DB")).toBe("sqlite");
  });
});

describe("isConnectionUrl", () => {
  test("recognizes postgres:// URLs", () => {
    expect(isConnectionUrl("postgres://user:pass@localhost/db")).toBe(true);
  });

  test("recognizes postgresql:// URLs", () => {
    expect(isConnectionUrl("postgresql://user:pass@localhost/db")).toBe(true);
  });

  test("recognizes mysql:// URLs", () => {
    expect(isConnectionUrl("mysql://user:pass@localhost/db")).toBe(true);
  });

  test("recognizes mariadb:// URLs", () => {
    expect(isConnectionUrl("mariadb://user:pass@localhost/db")).toBe(true);
  });

  test("recognizes sqlite:// URLs", () => {
    expect(isConnectionUrl("sqlite:///path/to/db")).toBe(true);
  });

  test("recognizes snowflake:// URLs", () => {
    expect(isConnectionUrl("snowflake://myorg-myaccount/DB/SCHEMA")).toBe(true);
  });

  test("recognizes cockroachdb:// URLs", () => {
    expect(isConnectionUrl("cockroachdb://user:pass@localhost:26257/mydb")).toBe(true);
  });

  test("recognizes duckdb:// URLs", () => {
    expect(isConnectionUrl("duckdb:///path/to/db")).toBe(true);
  });

  test("rejects plain strings", () => {
    expect(isConnectionUrl("my-connection")).toBe(false);
  });

  test("rejects file paths", () => {
    expect(isConnectionUrl("./test.db")).toBe(false);
  });
});

describe("isFilePath", () => {
  test("recognizes .db extension", () => {
    expect(isFilePath("./test.db")).toBe(true);
  });

  test("recognizes .sqlite extension", () => {
    expect(isFilePath("/data/test.sqlite")).toBe(true);
  });

  test("recognizes .sqlite3 extension", () => {
    expect(isFilePath("test.sqlite3")).toBe(true);
  });

  test("recognizes .db3 extension", () => {
    expect(isFilePath("test.db3")).toBe(true);
  });

  test("recognizes .duckdb extension", () => {
    expect(isFilePath("data.duckdb")).toBe(true);
  });

  test("recognizes existing files on disk", () => {
    const tmpPath = join(tmpdir(), "resolve-test-exists.txt");
    writeFileSync(tmpPath, "test");
    try {
      expect(isFilePath(tmpPath)).toBe(true);
    } finally {
      unlinkSync(tmpPath);
    }
  });

  test("rejects plain alias strings", () => {
    expect(isFilePath("my-connection")).toBe(false);
  });

  test("rejects URLs", () => {
    expect(isFilePath("postgres://localhost/db")).toBe(false);
  });
});

describe("resolveDriver ad-hoc connections", () => {
  const testDbPath = join(tmpdir(), "resolve-adhoc-test.db");

  beforeAll(() => {
    const { Database } = require("bun:sqlite");
    const db = new Database(testDbPath, { create: true });
    db.run("CREATE TABLE t(x INTEGER)");
    db.run("INSERT INTO t VALUES(1)");
    db.close();
  });

  afterAll(() => {
    try {
      unlinkSync(testDbPath);
    } catch {
      // ignore
    }
  });

  test("-c ./test.db resolves to ad-hoc SQLite (file path)", async () => {
    const driver = await resolveDriver({ connection: testDbPath });
    try {
      const result = await driver.query("SELECT * FROM t");
      expect(result.rows).toEqual([{ x: 1 }]);
    } finally {
      await driver.close();
    }
  });

  test("ad-hoc SQLite defaults to read-only", async () => {
    const driver = await resolveDriver({ connection: testDbPath });
    try {
      expect(() => driver.query("INSERT INTO t VALUES(2)")).toThrow();
    } finally {
      await driver.close();
    }
  });

  test("ad-hoc SQLite with --write works", async () => {
    const driver = await resolveDriver({ connection: testDbPath, write: true });
    try {
      const result = await driver.query("INSERT INTO t VALUES(99)", { write: true });
      expect(result.rowsAffected).toBe(1);
    } finally {
      await driver.close();
    }
  });

  test("-c nonexistent-alias errors with available connections", async () => {
    try {
      await resolveDriver({ connection: "nonexistent-alias" });
      expect(true).toBe(false); // should not reach
    } catch (err) {
      const msg = (err as Error).message;
      expect(msg).toContain("Unknown connection");
      expect(msg).toContain("nonexistent-alias");
      expect(msg).toContain("Tip:");
      expect(msg).toContain("file paths");
      expect(msg).toContain("connection URLs");
    }
  });

  // These tests verify URL parsing without actually connecting
  test("-c postgres://user:pass@localhost/db is detected as connection URL", () => {
    expect(isConnectionUrl("postgres://user:pass@localhost/db")).toBe(true);
    expect(detectDriverFromUrl("postgres://user:pass@localhost/db")).toBe("pg");
  });

  test("-c cockroachdb://user:pass@localhost:26257/db is detected as connection URL", () => {
    expect(isConnectionUrl("cockroachdb://user:pass@localhost:26257/db")).toBe(true);
    expect(detectDriverFromUrl("cockroachdb://user:pass@localhost:26257/db")).toBe("cockroachdb");
  });

  test("-c mysql://user:pass@localhost/db is detected as connection URL", () => {
    expect(isConnectionUrl("mysql://user:pass@localhost/db")).toBe(true);
    expect(detectDriverFromUrl("mysql://user:pass@localhost/db")).toBe("mysql");
  });

  test("ad-hoc PG URL with --write is rejected", async () => {
    try {
      await resolveDriver({ connection: "postgres://user:pass@localhost/db", write: true });
      expect(true).toBe(false); // should not reach
    } catch (err) {
      const msg = (err as Error).message;
      expect(msg).toContain("Write mode is not available for ad-hoc connections");
      expect(msg).toContain("named connection");
      expect((err as { fixableBy: string }).fixableBy).toBe("human");
    }
  });

  test("ad-hoc MySQL URL with --write is rejected", async () => {
    try {
      await resolveDriver({ connection: "mysql://user:pass@localhost/db", write: true });
      expect(true).toBe(false); // should not reach
    } catch (err) {
      const msg = (err as Error).message;
      expect(msg).toContain("Write mode is not available for ad-hoc connections");
      expect(msg).toContain("named connection");
      expect((err as { fixableBy: string }).fixableBy).toBe("human");
    }
  });
});
