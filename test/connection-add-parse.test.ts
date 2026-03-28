import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

const PROJECT_ROOT = join(import.meta.dir, "..");
const CLI_PATH = join(PROJECT_ROOT, "bin", "agent-sql.bun.js");

let tempDir: string;
let configDir: string;

beforeAll(() => {
  tempDir = mkdtempSync(join(tmpdir(), "agent-sql-add-"));
  configDir = join(tempDir, "config");
  mkdirSync(join(configDir, "agent-sql"), { recursive: true });
  writeFileSync(
    join(configDir, "agent-sql", "config.json"),
    JSON.stringify({ connections: {}, settings: {} }, null, 2),
  );
});

afterAll(() => {
  rmSync(tempDir, { recursive: true, force: true });
});

type CliResult = {
  stdout: string;
  stderr: string;
  exitCode: number;
};

const run = async (args: string[]): Promise<CliResult> => {
  const proc = Bun.spawn(["bun", "run", CLI_PATH, ...args], {
    cwd: PROJECT_ROOT,
    env: {
      ...process.env,
      XDG_CONFIG_HOME: configDir,
    },
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
  ]);
  const exitCode = await proc.exited;
  return { stdout, stderr, exitCode };
};

const parseStdout = (result: CliResult): Record<string, unknown> =>
  JSON.parse(result.stdout) as Record<string, unknown>;

describe("connection add - parse connection string", () => {
  test("parses postgres:// URL", async () => {
    const result = await run(["connection", "add", "test-pg", "postgres://localhost:5433/mydb"]);
    const out = parseStdout(result);
    expect(out.ok).toBe(true);
    expect(out.driver).toBe("pg");
    expect(out.url).toBe("postgres://localhost:5433/mydb");
  });

  test("parses mysql:// URL", async () => {
    const result = await run(["connection", "add", "test-mysql", "mysql://localhost:3307/appdb"]);
    const out = parseStdout(result);
    expect(out.ok).toBe(true);
    expect(out.driver).toBe("mysql");
    expect(out.url).toBe("mysql://localhost:3307/appdb");
  });

  test("parses SQLite file path", async () => {
    const dbPath = join(tempDir, "local.db");
    writeFileSync(dbPath, "");
    const result = await run(["connection", "add", "test-sqlite", dbPath]);
    const out = parseStdout(result);
    expect(out.ok).toBe(true);
    expect(out.driver).toBe("sqlite");
    expect(out.path).toBe(dbPath);
  });

  test("parses snowflake:// URL", async () => {
    const result = await run([
      "connection",
      "add",
      "test-snow",
      "snowflake://myorg-myaccount/mydb/public?warehouse=compute_wh&role=analyst",
    ]);
    const out = parseStdout(result);
    expect(out.ok).toBe(true);
    expect(out.driver).toBe("snowflake");
    expect(out.account).toBe("myorg-myaccount");
    expect(out.database).toBe("mydb");
    expect(out.schema).toBe("public");
    expect(out.warehouse).toBe("compute_wh");
    expect(out.role).toBe("analyst");
  });

  test("flags override URL-parsed values", async () => {
    const result = await run([
      "connection",
      "add",
      "test-override",
      "postgres://localhost:5433/mydb",
      "--database",
      "otherdb",
      "--host",
      "remotehost",
      "--port",
      "9999",
    ]);
    const out = parseStdout(result);
    expect(out.ok).toBe(true);
    expect(out.driver).toBe("pg");
    // Flags are set before parseConnectionString runs, and parseConnectionString
    // only fills in values that are not already set by opts
    expect(out.database).toBe("otherdb");
    expect(out.host).toBe("remotehost");
    expect(out.port).toBe(9999);
  });
});
