import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { Database } from "bun:sqlite";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

const PROJECT_ROOT = join(import.meta.dir, "..", "..");
const CLI_PATH = join(PROJECT_ROOT, "bin", "agent-sql.bun.js");

let tempDir: string;
let dbPath: string;
let configDir: string;

beforeAll(() => {
  tempDir = mkdtempSync(join(tmpdir(), "agent-sql-int-"));
  dbPath = join(tempDir, "test.db");
  configDir = join(tempDir, "config");

  const db = new Database(dbPath);
  db.exec(`
    CREATE TABLE users (
      id INTEGER PRIMARY KEY,
      name TEXT NOT NULL,
      email TEXT UNIQUE,
      bio TEXT,
      created_at TEXT DEFAULT CURRENT_TIMESTAMP
    );
    CREATE TABLE posts (
      id INTEGER PRIMARY KEY,
      user_id INTEGER REFERENCES users(id),
      title TEXT NOT NULL,
      body TEXT,
      published INTEGER DEFAULT 0
    );
    CREATE INDEX idx_posts_user_id ON posts(user_id);
    CREATE INDEX idx_posts_published ON posts(published);
    INSERT INTO users (id, name, email, bio) VALUES (1, 'Alice', 'alice@example.com', 'A software developer who enjoys writing long blog posts about distributed systems and functional programming paradigms. She has been coding for over fifteen years and specializes in backend development with a focus on scalability and performance optimization.');
    INSERT INTO users (id, name, email) VALUES (2, 'Bob', 'bob@example.com');
    INSERT INTO posts (id, user_id, title, body, published) VALUES (1, 1, 'Hello World', 'My first post content here', 1);
    INSERT INTO posts (id, user_id, title, body, published) VALUES (2, 1, 'Second Post', 'More content', 0);
    INSERT INTO posts (id, user_id, title, body, published) VALUES (3, 2, 'Bob Post', 'Bob writes too', 1);
  `);
  db.close();

  mkdirSync(join(configDir, "agent-sql"), { recursive: true });
  writeFileSync(
    join(configDir, "agent-sql", "config.json"),
    JSON.stringify(
      {
        default_connection: "test",
        connections: {
          test: {
            driver: "sqlite",
            path: dbPath,
          },
        },
        settings: {},
      },
      null,
      2,
    ),
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

const parseStdout = (result: CliResult): unknown => JSON.parse(result.stdout);

// ---------------------------------------------------------------------------
// Query commands
// ---------------------------------------------------------------------------

describe("query run", () => {
  test("returns correct rows with columns", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT * FROM users ORDER BY id",
      "--format",
      "json",
    ]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as { columns: string[]; rows: Record<string, unknown>[] };
    expect(parsed.columns).toEqual(["id", "name", "email", "bio", "created_at"]);
    expect(parsed.rows).toHaveLength(2);
    expect(parsed.rows[0]!.id).toBe(1);
    expect(parsed.rows[0]!.name).toBe("Alice");
    expect(parsed.rows[1]!.id).toBe(2);
    expect(parsed.rows[1]!.name).toBe("Bob");
  });

  test("--limit 1 returns 1 row with hasMore", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT * FROM users ORDER BY id",
      "--limit",
      "1",
      "--format",
      "json",
    ]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as {
      rows: Record<string, unknown>[];
      pagination?: { hasMore: boolean };
    };
    expect(parsed.rows).toHaveLength(1);
    expect(parsed.pagination?.hasMore).toBe(true);
  });

  test("--compact returns array-of-arrays format", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT id, name FROM users ORDER BY id",
      "--compact",
      "--format",
      "json",
    ]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as { columns: string[]; rows: unknown[][] };
    expect(parsed.columns).toEqual(["id", "name", "@truncated"]);
    expect(parsed.rows[0]).toEqual([1, "Alice", null]);
    expect(parsed.rows[1]).toEqual([2, "Bob", null]);
  });
});

describe("run (top-level alias)", () => {
  test("works identically to query run", async () => {
    const result = await run(["run", "SELECT id, name FROM users ORDER BY id", "--format", "json"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as { columns: string[]; rows: Record<string, unknown>[] };
    expect(parsed.columns).toEqual(["id", "name"]);
    expect(parsed.rows).toHaveLength(2);
  });
});

describe("query sample", () => {
  test("returns sample rows", async () => {
    const result = await run(["query", "sample", "users", "--limit", "2", "--format", "json"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as { rows: Record<string, unknown>[] };
    expect(parsed.rows).toHaveLength(2);
  });
});

describe("query count", () => {
  test("returns total count", async () => {
    const result = await run(["query", "count", "users"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as { table: string; count: number };
    expect(parsed.table).toBe("users");
    expect(parsed.count).toBe(2);
  });

  test("--where returns filtered count", async () => {
    const result = await run(["query", "count", "posts", "--where", "published = 1"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as { table: string; count: number };
    expect(parsed.table).toBe("posts");
    expect(parsed.count).toBe(2);
  });
});

describe("query explain", () => {
  test("returns explain output", async () => {
    const result = await run(["query", "explain", "SELECT * FROM users"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as { plan: unknown[] };
    expect(parsed.plan).toBeDefined();
    expect(Array.isArray(parsed.plan)).toBe(true);
    expect(parsed.plan.length).toBeGreaterThan(0);
  });
});

describe("truncation", () => {
  test("bio field gets truncated and @truncated metadata present", async () => {
    const result = await run([
      "query",
      "run",
      "SELECT * FROM users WHERE id = 1",
      "--format",
      "json",
    ]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as { rows: Record<string, unknown>[] };
    const alice = parsed.rows[0]!;
    expect(typeof alice.bio).toBe("string");
    // bio is > 200 chars so should be truncated with ellipsis
    expect((alice.bio as string).endsWith("\u2026")).toBe(true);
    expect(alice["@truncated"]).toBeDefined();
    const truncated = alice["@truncated"] as Record<string, number>;
    expect(truncated.bio).toBeGreaterThan(200);
  });
});

describe("write protection", () => {
  test("write blocked without --write flag", async () => {
    const result = await run([
      "query",
      "run",
      "INSERT INTO users (id, name, email) VALUES (99, 'Test', 'test@example.com')",
    ]);
    // Should fail because DB opened read-only
    expect(result.exitCode).toBe(1);
    const parsed = JSON.parse(result.stderr) as { error: string };
    expect(parsed.error).toBeDefined();
  });

  test("write allowed with --write flag", async () => {
    const result = await run([
      "query",
      "run",
      "INSERT INTO users (id, name, email) VALUES (99, 'Test', 'test@example.com')",
      "--write",
      "--format",
      "json",
    ]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as {
      result: string;
      rowsAffected: number;
      command: string;
    };
    expect(parsed.result).toBe("ok");
    expect(parsed.rowsAffected).toBe(1);
    expect(parsed.command).toBe("INSERT");

    // Clean up inserted row
    await run(["query", "run", "DELETE FROM users WHERE id = 99", "--write"]);
  });
});

// ---------------------------------------------------------------------------
// Schema commands
// ---------------------------------------------------------------------------

describe("schema tables", () => {
  test("lists users and posts", async () => {
    const result = await run(["schema", "tables"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as { tables: { name: string }[] };
    const names = parsed.tables.map((t) => t.name).sort();
    expect(names).toEqual(["posts", "users"]);
  });
});

describe("schema describe", () => {
  test("shows columns with types", async () => {
    const result = await run(["schema", "describe", "users"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as {
      table: string;
      columns: { name: string; type: string; nullable: boolean; primaryKey?: boolean }[];
    };
    expect(parsed.table).toBe("users");
    const colNames = parsed.columns.map((c) => c.name);
    expect(colNames).toEqual(["id", "name", "email", "bio", "created_at"]);

    const idCol = parsed.columns.find((c) => c.name === "id")!;
    expect(idCol.primaryKey).toBe(true);
    expect(idCol.type).toBe("INTEGER");
  });

  test("--detailed includes constraints and indexes", async () => {
    const result = await run(["schema", "describe", "users", "--detailed"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as {
      table: string;
      columns: unknown[];
      constraints?: unknown[];
      indexes?: unknown[];
    };
    expect(parsed.table).toBe("users");
    expect(parsed.columns).toBeDefined();
    expect(parsed.constraints).toBeDefined();
    expect(parsed.indexes).toBeDefined();
  });
});

describe("schema indexes", () => {
  test("shows indexes for posts table", async () => {
    const result = await run(["schema", "indexes", "posts"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as {
      indexes: { name: string; table: string; columns: string[] }[];
    };
    const indexNames = parsed.indexes.map((i) => i.name).sort();
    expect(indexNames).toContain("idx_posts_user_id");
    expect(indexNames).toContain("idx_posts_published");
  });
});

describe("schema constraints", () => {
  test("shows FK to users on posts table", async () => {
    const result = await run(["schema", "constraints", "posts"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as {
      constraints: {
        name: string;
        type: string;
        columns: string[];
        referencedTable?: string;
      }[];
    };
    const fk = parsed.constraints.find((c) => c.type === "foreign_key");
    expect(fk).toBeDefined();
    expect(fk!.columns).toContain("user_id");
    expect(fk!.referencedTable).toBe("users");
  });
});

describe("schema search", () => {
  test("finds users table and user_id column", async () => {
    const result = await run(["schema", "search", "user"]);
    expect(result.exitCode).toBe(0);
    const parsed = parseStdout(result) as {
      tables: { name: string }[];
      columns: { table: string; column: string }[];
    };
    const tableNames = parsed.tables.map((t) => t.name);
    expect(tableNames).toContain("users");
    const colMatch = parsed.columns.find((c) => c.column === "user_id");
    expect(colMatch).toBeDefined();
    expect(colMatch!.table).toBe("posts");
  });
});

describe("schema dump", () => {
  test("returns complete schema for all tables", async () => {
    const result = await run(["schema", "dump"]);
    expect(result.exitCode).toBe(0);
    const data = JSON.parse(result.stdout);
    expect(data.tables).toBeArray();
    expect(data.tables.length).toBeGreaterThanOrEqual(2);
    const tableNames = data.tables.map((t: { name: string }) => t.name);
    expect(tableNames).toContain("users");
    expect(tableNames).toContain("posts");
    const users = data.tables.find((t: { name: string }) => t.name === "users");
    expect(users.columns).toBeArray();
    expect(users.columns.length).toBeGreaterThan(0);
  });
});
