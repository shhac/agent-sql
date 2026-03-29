import { describe } from "bun:test";
import { driverContractTests } from "./contract";
import { connectPg } from "../../src/drivers/pg";

const PG_URL = process.env.AGENT_SQL_PG_TEST_URL;
const runTests = PG_URL
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
    port: Number(parsed.port) || 5432,
    database: parsed.pathname.slice(1),
    username: parsed.username,
    password: parsed.password,
  };
};

runTests({
  connect: () =>
    connectPg({
      ...parseUrl(PG_URL!),
      readonly: false,
    }),
  seedSql: [
    "CREATE TABLE IF NOT EXISTS contract_test (id SERIAL PRIMARY KEY, name TEXT NOT NULL, email TEXT UNIQUE)",
    "INSERT INTO contract_test (name, email) VALUES ('Alice', 'alice@contract.com') ON CONFLICT DO NOTHING",
    "INSERT INTO contract_test (name, email) VALUES ('Bob', 'bob@contract.com') ON CONFLICT DO NOTHING",
  ],
  tableName: "contract_test",
  columnName: "email",
});
