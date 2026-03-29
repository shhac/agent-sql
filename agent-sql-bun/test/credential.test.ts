import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { mkdtempSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const originalXdg = process.env.XDG_CONFIG_HOME;

const makeTempDir = (): string => mkdtempSync(join(tmpdir(), "agent-sql-cred-test-"));

const forceFileStorage = (dir: string): void => {
  process.env.XDG_CONFIG_HOME = dir;
};

const restoreEnv = (): void => {
  if (originalXdg === undefined) {
    delete process.env.XDG_CONFIG_HOME;
  } else {
    process.env.XDG_CONFIG_HOME = originalXdg;
  }
};

// Fresh import per test to pick up env changes
const loadModule = async () => {
  const mod = await import("../src/lib/credentials.ts");
  return mod;
};

describe("credential storage (file-based)", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = makeTempDir();
    forceFileStorage(tempDir);
  });

  afterEach(() => {
    restoreEnv();
    rmSync(tempDir, { recursive: true, force: true });
  });

  test("store and retrieve a PG credential (username + password + writePermission)", async () => {
    const { storeCredential, getCredential } = await loadModule();
    storeCredential("pg-prod", { username: "reader", password: "s3cret", writePermission: false });
    const cred = getCredential("pg-prod");
    expect(cred).toEqual({ username: "reader", password: "s3cret", writePermission: false });
  });

  test("store and retrieve a SQLite credential (writePermission only)", async () => {
    const { storeCredential, getCredential } = await loadModule();
    storeCredential("sqlite-rw", { writePermission: true });
    const cred = getCredential("sqlite-rw");
    expect(cred).toEqual({ writePermission: true });
  });

  test("store credential with writePermission: false", async () => {
    const { storeCredential, getCredential } = await loadModule();
    storeCredential("ro-cred", { writePermission: false });
    const cred = getCredential("ro-cred");
    expect(cred).toEqual({ writePermission: false });
  });

  test("getCredential returns null for nonexistent credential", async () => {
    const { getCredential } = await loadModule();
    expect(getCredential("does-not-exist")).toBeNull();
  });

  test("remove a credential", async () => {
    const { storeCredential, removeCredential, getCredential } = await loadModule();
    storeCredential("temp-cred", { username: "u", password: "p", writePermission: false });
    expect(getCredential("temp-cred")).not.toBeNull();
    const removed = removeCredential("temp-cred");
    expect(removed).toBe(true);
    expect(getCredential("temp-cred")).toBeNull();
  });

  test("remove nonexistent credential returns false", async () => {
    const { removeCredential } = await loadModule();
    expect(removeCredential("ghost")).toBe(false);
  });

  test("listCredentials returns all stored credentials with passwords masked", async () => {
    const { storeCredential, listCredentials } = await loadModule();
    storeCredential("pg-ro", { username: "reader", password: "secret123", writePermission: false });
    storeCredential("pg-rw", { username: "admin", password: "admin-pass", writePermission: true });
    storeCredential("sqlite-rw", { writePermission: true });

    const list = listCredentials();
    expect(list).toHaveLength(3);

    const pgRo = list.find((c) => c.name === "pg-ro");
    expect(pgRo).toBeDefined();
    expect(pgRo!.username).toBe("reader");
    expect(pgRo!.password).toBe("********");
    expect(pgRo!.writePermission).toBe(false);

    const pgRw = list.find((c) => c.name === "pg-rw");
    expect(pgRw).toBeDefined();
    expect(pgRw!.username).toBe("admin");
    expect(pgRw!.password).toBe("********");
    expect(pgRw!.writePermission).toBe(true);

    const sqliteRw = list.find((c) => c.name === "sqlite-rw");
    expect(sqliteRw).toBeDefined();
    expect(sqliteRw!.username).toBeUndefined();
    expect(sqliteRw!.password).toBeUndefined();
    expect(sqliteRw!.writePermission).toBe(true);
  });

  test("getCredentialNames returns just the names", async () => {
    const { storeCredential, getCredentialNames } = await loadModule();
    storeCredential("alpha", { writePermission: false });
    storeCredential("beta", { username: "u", password: "p", writePermission: true });

    const names = getCredentialNames();
    expect(names.sort()).toEqual(["alpha", "beta"]);
  });

  test("overwriting a credential replaces it", async () => {
    const { storeCredential, getCredential } = await loadModule();
    storeCredential("evolving", { username: "old", password: "old-pw", writePermission: false });
    storeCredential("evolving", { username: "new", password: "new-pw", writePermission: true });
    const cred = getCredential("evolving");
    expect(cred).toEqual({ username: "new", password: "new-pw", writePermission: true });
  });

  test("credentials are persisted to a JSON file", async () => {
    const { storeCredential } = await loadModule();
    storeCredential("persisted", { username: "u", password: "p", writePermission: false });

    const filePath = join(tempDir, "agent-sql", "credentials.json");
    const raw = readFileSync(filePath, "utf8");
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    expect(parsed).toHaveProperty("persisted");
  });

  test("credentials file is separate from config file", async () => {
    const { storeCredential } = await loadModule();
    storeCredential("isolated", { writePermission: true });

    const credPath = join(tempDir, "agent-sql", "credentials.json");
    const configPath = join(tempDir, "agent-sql", "config.json");

    const credRaw = readFileSync(credPath, "utf8");
    expect(credRaw).toContain("isolated");

    // config.json should not exist or should not contain credential data
    try {
      const configRaw = readFileSync(configPath, "utf8");
      expect(configRaw).not.toContain("isolated");
    } catch {
      // config.json doesn't exist — that's fine
    }
  });
});
