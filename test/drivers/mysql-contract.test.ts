import { describe } from "bun:test";
import { driverContractTests } from "./contract";
import { connectMysql } from "../../src/drivers/mysql";

const MYSQL_URL = process.env.AGENT_SQL_MYSQL_TEST_URL;
const runTests = MYSQL_URL
  ? driverContractTests
  : () => describe.skip("contract: DriverConnection", () => {});

const parseUrl = (
  url: string,
): {
  host: string;
  port: number;
  database: string;
  username: string;
  password: string;
} => {
  const parsed = new URL(url);
  return {
    host: parsed.hostname,
    port: Number(parsed.port) || 3306,
    database: parsed.pathname.slice(1),
    username: parsed.username,
    password: parsed.password,
  };
};

runTests({
  connect: () =>
    connectMysql({
      ...parseUrl(MYSQL_URL!),
      readonly: false,
    }),
  seedSql: [
    "CREATE TABLE IF NOT EXISTS contract_test (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255) NOT NULL, email VARCHAR(255) UNIQUE)",
    "INSERT IGNORE INTO contract_test (name, email) VALUES ('Alice', 'alice@contract.com')",
    "INSERT IGNORE INTO contract_test (name, email) VALUES ('Bob', 'bob@contract.com')",
  ],
  tableName: "contract_test",
  columnName: "email",
});
