import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { Database } from "bun:sqlite";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import yaml from "js-yaml";

const PROJECT_ROOT = join(import.meta.dir, "..", "..");
const CLI_PATH = join(PROJECT_ROOT, "bin", "agent-sql.bun.js");

let tempDir: string;
let dbPath: string;
let configDir: string;

beforeAll(() => {
  tempDir = mkdtempSync(join(tmpdir(), "agent-sql-fmt-"));
  dbPath = join(tempDir, "test.db");
  configDir = join(tempDir, "config");

  const db = new Database(dbPath);
  db.exec(`
    CREATE TABLE users (
      id INTEGER PRIMARY KEY,
      name TEXT NOT NULL,
      email TEXT UNIQUE
    );
    CREATE TABLE posts (
      id INTEGER PRIMARY KEY,
      user_id INTEGER REFERENCES users(id),
      title TEXT NOT NULL
    );
    INSERT INTO users (id, name, email) VALUES (1, 'Alice', 'alice@example.com');
    INSERT INTO users (id, name, email) VALUES (2, 'Bob', 'bob@example.com');
    INSERT INTO posts (id, user_id, title) VALUES (1, 1, 'Hello World');
  `);
  db.close();

  mkdirSync(join(configDir, "agent-sql"), { recursive: true });
  writeFileSync(
    join(configDir, "agent-sql", "config.json"),
    JSON.stringify({
      default_connection: "test",
      connections: {
        test: { driver: "sqlite", path: dbPath },
      },
      settings: {},
    }),
  );
});

afterAll(() => {
  rmSync(tempDir, { recursive: true, force: true });
});

type CliResult = { stdout: string; stderr: string; exitCode: number };

const run = async (args: string[]): Promise<CliResult> => {
  const proc = Bun.spawn(["bun", "run", CLI_PATH, ...args], {
    cwd: PROJECT_ROOT,
    env: { ...process.env, XDG_CONFIG_HOME: configDir },
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

// ---------------------------------------------------------------------------
// JSONL format (default)
// ---------------------------------------------------------------------------

describe("default format (jsonl)", () => {
  test("query run produces one JSON object per line", async () => {
    const result = await run(["query", "run", "SELECT id, name FROM users ORDER BY id"]);
    expect(result.exitCode).toBe(0);
    const lines = result.stdout.trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    const row1 = JSON.parse(lines[0]!) as Record<string, unknown>;
    const row2 = JSON.parse(lines[1]!) as Record<string, unknown>;
    expect(row1.id).toBe(1);
    expect(row1.name).toBe("Alice");
    expect(row2.id).toBe(2);
    expect(row2.name).toBe("Bob");
  });

  test("each JSONL line includes @truncated", async () => {
    const result = await run(["query", "run", "SELECT id, name FROM users ORDER BY id"]);
    const lines = result.stdout.trimEnd().split("\n");
    for (const line of lines) {
      const row = JSON.parse(line) as Record<string, unknown>;
      expect("@truncated" in row).toBe(true);
    }
  });

  test("--limit 1 appends @pagination line", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT id, name FROM users ORDER BY id",
      "--limit",
      "1",
    ]);
    expect(result.exitCode).toBe(0);
    const lines = result.stdout.trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    const pagination = JSON.parse(lines[1]!) as Record<string, unknown>;
    expect(pagination["@pagination"]).toEqual({ hasMore: true, rowCount: 1 });
  });

  test("compact mode still produces JSONL with named keys", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT id, name FROM users ORDER BY id",
      "--compact",
    ]);
    expect(result.exitCode).toBe(0);
    const lines = result.stdout.trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    const row1 = JSON.parse(lines[0]!) as Record<string, unknown>;
    expect(row1.id).toBe(1);
    expect(row1.name).toBe("Alice");
  });

  test("schema tables still uses JSON envelope (non-tabular)", async () => {
    const result = await run(["schema", "tables"]);
    expect(result.exitCode).toBe(0);
    const parsed = JSON.parse(result.stdout) as { tables: { name: string }[] };
    expect(parsed.tables).toBeDefined();
  });
});

// ---------------------------------------------------------------------------
// YAML format
// ---------------------------------------------------------------------------

describe("--format yaml", () => {
  test("query run produces parseable YAML with correct data", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT id, name FROM users ORDER BY id",
      "--format",
      "yaml",
    ]);
    expect(result.exitCode).toBe(0);
    const parsed = yaml.load(result.stdout) as {
      columns: string[];
      rows: Record<string, unknown>[];
    };
    expect(parsed.columns).toEqual(["id", "name"]);
    expect(parsed.rows).toHaveLength(2);
    expect(parsed.rows[0]!.id).toBe(1);
    expect(parsed.rows[0]!.name).toBe("Alice");
    expect(parsed.rows[1]!.id).toBe(2);
    expect(parsed.rows[1]!.name).toBe("Bob");
  });

  test("schema tables produces parseable YAML", async () => {
    const result = await run(["schema", "tables", "--format", "yaml"]);
    expect(result.exitCode).toBe(0);
    const parsed = yaml.load(result.stdout) as {
      tables: { name: string }[];
    };
    const names = parsed.tables.map((t) => t.name).sort();
    expect(names).toEqual(["posts", "users"]);
  });

  test("errors are always JSON even with --format yaml", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT * FROM nonexistent_table",
      "--format",
      "yaml",
    ]);
    expect(result.exitCode).toBe(1);
    const parsed = JSON.parse(result.stderr) as { error: string };
    expect(parsed.error).toBeDefined();
  });
});

// ---------------------------------------------------------------------------
// CSV format
// ---------------------------------------------------------------------------

describe("--format csv", () => {
  test("query run produces CSV with header and data rows", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT id, name FROM users ORDER BY id",
      "--format",
      "csv",
    ]);
    expect(result.exitCode).toBe(0);
    const lines = result.stdout.trimEnd().split("\n");
    expect(lines[0]).toBe("id,name");
    expect(lines[1]).toBe("1,Alice");
    expect(lines[2]).toBe("2,Bob");
  });

  test("schema tables with --format csv falls back to JSON", async () => {
    const result = await run(["schema", "tables", "--format", "csv"]);
    expect(result.exitCode).toBe(0);
    const parsed = JSON.parse(result.stdout) as { tables: { name: string }[] };
    expect(parsed.tables).toBeDefined();
  });

  test("compact mode with --format csv produces CSV", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT id, name FROM users ORDER BY id",
      "--compact",
      "--format",
      "csv",
    ]);
    expect(result.exitCode).toBe(0);
    const lines = result.stdout.trimEnd().split("\n");
    expect(lines[0]).toBe("id,name,@truncated");
    expect(lines[1]).toBe("1,Alice,");
    expect(lines[2]).toBe("2,Bob,");
  });
});
