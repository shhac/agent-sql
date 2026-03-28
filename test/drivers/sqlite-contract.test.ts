import { driverContractTests } from "./contract";
import { connectSqlite } from "../../src/drivers/sqlite";
import { mkdtempSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

const tempDir = mkdtempSync(join(tmpdir(), "agent-sql-contract-sqlite-"));

driverContractTests({
  connect: () =>
    Promise.resolve(
      connectSqlite({
        path: join(tempDir, "test.db"),
        readonly: false,
        create: true,
      }),
    ),
  seedSql: [
    "CREATE TABLE IF NOT EXISTS contract_test (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT UNIQUE)",
    "INSERT OR IGNORE INTO contract_test (id, name, email) VALUES (1, 'Alice', 'alice@test.com')",
    "INSERT OR IGNORE INTO contract_test (id, name, email) VALUES (2, 'Bob', 'bob@test.com')",
  ],
  tableName: "contract_test",
  columnName: "email",
});
