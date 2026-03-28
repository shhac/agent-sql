import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import type { DriverConnection } from "../../src/drivers/types";

export type ContractSetup = {
  connect: () => Promise<DriverConnection>;
  seedSql: string[];
  tableName: string;
  columnName: string;
  supportsIndexes?: boolean;
};

export const driverContractTests = (setup: ContractSetup): void => {
  describe("contract: DriverConnection", () => {
    let conn: DriverConnection;

    beforeAll(async () => {
      conn = await setup.connect();
      for (const sql of setup.seedSql) {
        await conn.query(sql, { write: true });
      }
    });

    afterAll(async () => {
      await conn.close();
    });

    test("quoteIdent returns a string", () => {
      const result = conn.quoteIdent("test_table");
      expect(typeof result).toBe("string");
      expect(result.length).toBeGreaterThan(0);
    });

    test("query SELECT returns columns and rows", async () => {
      const result = await conn.query(`SELECT * FROM ${conn.quoteIdent(setup.tableName)}`);
      expect(Array.isArray(result.columns)).toBe(true);
      expect(result.columns.length).toBeGreaterThan(0);
      expect(Array.isArray(result.rows)).toBe(true);
      expect(result.rows.length).toBeGreaterThan(0);
      for (const row of result.rows) {
        for (const col of result.columns) {
          expect(col in row).toBe(true);
        }
      }
    });

    test("getTables returns array of TableInfo", async () => {
      const tables = await conn.getTables();
      expect(Array.isArray(tables)).toBe(true);
      expect(tables.length).toBeGreaterThan(0);
      const found = tables.find(
        (t) => t.name.includes(setup.tableName) || t.name.endsWith(setup.tableName),
      );
      expect(found).toBeDefined();
      expect(typeof found!.name).toBe("string");
      if (found!.type) {
        expect(["table", "view"]).toContain(found!.type);
      }
    });

    test("describeTable returns array of ColumnInfo", async () => {
      const columns = await conn.describeTable(setup.tableName);
      expect(Array.isArray(columns)).toBe(true);
      expect(columns.length).toBeGreaterThan(0);
      for (const col of columns) {
        expect(typeof col.name).toBe("string");
        expect(typeof col.type).toBe("string");
        expect(typeof col.nullable).toBe("boolean");
      }
      const found = columns.find((c) => c.name.toLowerCase() === setup.columnName.toLowerCase());
      expect(found).toBeDefined();
    });

    test("getIndexes returns array of IndexInfo", async () => {
      if (setup.supportsIndexes === false) {
        const indexes = await conn.getIndexes();
        expect(Array.isArray(indexes)).toBe(true);
        return;
      }
      const indexes = await conn.getIndexes(setup.tableName);
      expect(Array.isArray(indexes)).toBe(true);
      for (const idx of indexes) {
        expect(typeof idx.name).toBe("string");
        expect(typeof idx.table).toBe("string");
        expect(Array.isArray(idx.columns)).toBe(true);
        expect(typeof idx.unique).toBe("boolean");
      }
    });

    test("getConstraints returns array of ConstraintInfo", async () => {
      const constraints = await conn.getConstraints(setup.tableName);
      expect(Array.isArray(constraints)).toBe(true);
      for (const c of constraints) {
        expect(typeof c.name).toBe("string");
        expect(typeof c.table).toBe("string");
        expect(["primary_key", "foreign_key", "unique", "check"]).toContain(c.type);
        expect(Array.isArray(c.columns)).toBe(true);
      }
    });

    test("searchSchema returns tables and columns", async () => {
      const result = await conn.searchSchema(setup.columnName);
      expect(Array.isArray(result.tables)).toBe(true);
      expect(Array.isArray(result.columns)).toBe(true);
      const found = result.columns.find(
        (c) => c.column.toLowerCase() === setup.columnName.toLowerCase(),
      );
      expect(found).toBeDefined();
    });

    test("close does not throw", async () => {
      const tempConn = await setup.connect();
      await expect(tempConn.close()).resolves.toBeUndefined();
    });
  });
};
