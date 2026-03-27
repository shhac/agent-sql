import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { mkdtempSync, rmSync, readFileSync, existsSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { clearConfigCache } from "../src/lib/config.ts";

const createTempDir = (): string => mkdtempSync(join(tmpdir(), "agent-sql-config-test-"));

let tempDir: string;
let originalXdg: string | undefined;

beforeEach(() => {
  clearConfigCache();
  tempDir = createTempDir();
  originalXdg = process.env.XDG_CONFIG_HOME;
  process.env.XDG_CONFIG_HOME = tempDir;
});

afterEach(() => {
  if (originalXdg === undefined) {
    delete process.env.XDG_CONFIG_HOME;
  } else {
    process.env.XDG_CONFIG_HOME = originalXdg;
  }
  rmSync(tempDir, { recursive: true, force: true });
});

const freshImport = async () => {
  const mod = await import("../src/lib/config.ts");
  return mod;
};

describe("config creation", () => {
  test("readConfig returns empty config when no file exists", async () => {
    const { readConfig } = await freshImport();
    const config = readConfig();
    expect(config).toEqual({ connections: {}, settings: {} });
  });

  test("writeConfig creates config dir and file", async () => {
    const { writeConfig } = await freshImport();
    writeConfig({ connections: {}, settings: {} });
    const configPath = join(tempDir, "agent-sql", "config.json");
    expect(existsSync(configPath)).toBe(true);
    const raw = JSON.parse(readFileSync(configPath, "utf8"));
    expect(raw.connections).toEqual({});
  });
});

describe("connection storage", () => {
  test("storeConnection adds a pg connection", async () => {
    const { storeConnection, getConnection } = await freshImport();
    storeConnection("prod", {
      driver: "pg",
      host: "db.example.com",
      port: 5432,
      database: "myapp",
      credential: "prod-readonly",
    });
    const conn = getConnection("prod");
    expect(conn).toBeDefined();
    expect(conn!.driver).toBe("pg");
    expect(conn!.host).toBe("db.example.com");
    expect(conn!.port).toBe(5432);
    expect(conn!.database).toBe("myapp");
    expect(conn!.credential).toBe("prod-readonly");
  });

  test("storeConnection adds a sqlite connection", async () => {
    const { storeConnection, getConnection } = await freshImport();
    storeConnection("local", {
      driver: "sqlite",
      path: "/Users/paul/data/app.sqlite",
    });
    const conn = getConnection("local");
    expect(conn).toBeDefined();
    expect(conn!.driver).toBe("sqlite");
    expect(conn!.path).toBe("/Users/paul/data/app.sqlite");
    expect(conn!.credential).toBeUndefined();
  });

  test("getConnections returns all connections", async () => {
    const { storeConnection, getConnections } = await freshImport();
    storeConnection("a", { driver: "sqlite", path: "/a.db" });
    storeConnection("b", { driver: "pg", host: "localhost", port: 5432, database: "b" });
    const conns = getConnections();
    expect(Object.keys(conns)).toEqual(["a", "b"]);
  });

  test("removeConnection deletes a connection", async () => {
    const { storeConnection, removeConnection, getConnection } = await freshImport();
    storeConnection("x", { driver: "sqlite", path: "/x.db" });
    removeConnection("x");
    expect(getConnection("x")).toBeUndefined();
  });

  test("removeConnection throws for unknown alias", async () => {
    const { removeConnection } = await freshImport();
    expect(() => removeConnection("nope")).toThrow(/Unknown connection.*"nope"/);
  });

  test("updateConnection modifies fields", async () => {
    const { storeConnection, updateConnection, getConnection } = await freshImport();
    storeConnection("pg1", { driver: "pg", host: "old.host", port: 5432, database: "db" });
    updateConnection("pg1", { host: "new.host", port: 5433 });
    const conn = getConnection("pg1");
    expect(conn!.host).toBe("new.host");
    expect(conn!.port).toBe(5433);
    expect(conn!.database).toBe("db");
  });

  test("updateConnection throws for unknown alias", async () => {
    const { updateConnection } = await freshImport();
    expect(() => updateConnection("ghost", { host: "x" })).toThrow(/Unknown connection.*"ghost"/);
  });

  test("storeConnection with url field", async () => {
    const { storeConnection, getConnection } = await freshImport();
    storeConnection("from-url", {
      driver: "pg",
      url: "postgres://user@localhost:5432/mydb",
    });
    const conn = getConnection("from-url");
    expect(conn!.url).toBe("postgres://user@localhost:5432/mydb");
  });
});

describe("default connection management", () => {
  test("first connection becomes default", async () => {
    const { storeConnection, getDefaultConnectionAlias } = await freshImport();
    storeConnection("first", { driver: "sqlite", path: "/first.db" });
    expect(getDefaultConnectionAlias()).toBe("first");
  });

  test("second connection does not override default", async () => {
    const { storeConnection, getDefaultConnectionAlias } = await freshImport();
    storeConnection("first", { driver: "sqlite", path: "/first.db" });
    storeConnection("second", { driver: "sqlite", path: "/second.db" });
    expect(getDefaultConnectionAlias()).toBe("first");
  });

  test("setDefaultConnection changes default", async () => {
    const { storeConnection, setDefaultConnection, getDefaultConnectionAlias } =
      await freshImport();
    storeConnection("a", { driver: "sqlite", path: "/a.db" });
    storeConnection("b", { driver: "sqlite", path: "/b.db" });
    setDefaultConnection("b");
    expect(getDefaultConnectionAlias()).toBe("b");
  });

  test("setDefaultConnection throws for unknown alias", async () => {
    const { setDefaultConnection } = await freshImport();
    expect(() => setDefaultConnection("missing")).toThrow(/Unknown connection.*"missing"/);
  });

  test("removing default connection reassigns to first remaining", async () => {
    const { storeConnection, removeConnection, getDefaultConnectionAlias } = await freshImport();
    storeConnection("a", { driver: "sqlite", path: "/a.db" });
    storeConnection("b", { driver: "sqlite", path: "/b.db" });
    removeConnection("a");
    expect(getDefaultConnectionAlias()).toBe("b");
  });

  test("removing last connection clears default", async () => {
    const { storeConnection, removeConnection, getDefaultConnectionAlias } = await freshImport();
    storeConnection("only", { driver: "sqlite", path: "/only.db" });
    removeConnection("only");
    expect(getDefaultConnectionAlias()).toBeUndefined();
  });
});

describe("settings", () => {
  test("getSettings returns defaults when no settings exist", async () => {
    const { getSettings } = await freshImport();
    const settings = getSettings();
    expect(settings).toEqual({});
  });

  test("getSetting retrieves a nested value", async () => {
    const { updateSetting, getSetting } = await freshImport();
    updateSetting("defaults.limit", 50);
    expect(getSetting("defaults.limit")).toBe(50);
  });

  test("getSetting returns undefined for missing key", async () => {
    const { getSetting } = await freshImport();
    expect(getSetting("nonexistent.key")).toBeUndefined();
  });

  test("updateSetting creates nested structure", async () => {
    const { updateSetting, getSetting } = await freshImport();
    updateSetting("query.timeout", 60000);
    updateSetting("query.maxRows", 500);
    expect(getSetting("query.timeout")).toBe(60000);
    expect(getSetting("query.maxRows")).toBe(500);
  });

  test("updateSetting overwrites existing value", async () => {
    const { updateSetting, getSetting } = await freshImport();
    updateSetting("truncation.maxLength", 100);
    updateSetting("truncation.maxLength", 300);
    expect(getSetting("truncation.maxLength")).toBe(300);
  });

  test("resetSettings clears all settings", async () => {
    const { updateSetting, resetSettings, getSettings } = await freshImport();
    updateSetting("defaults.limit", 50);
    updateSetting("query.timeout", 10000);
    resetSettings();
    expect(getSettings()).toEqual({});
  });
});

describe("XDG_CONFIG_HOME support", () => {
  test("uses XDG_CONFIG_HOME when set", async () => {
    const customDir = createTempDir();
    process.env.XDG_CONFIG_HOME = customDir;
    try {
      const { writeConfig } = await freshImport();
      writeConfig({ connections: {}, settings: {} });
      const configPath = join(customDir, "agent-sql", "config.json");
      expect(existsSync(configPath)).toBe(true);
    } finally {
      process.env.XDG_CONFIG_HOME = tempDir;
      rmSync(customDir, { recursive: true, force: true });
    }
  });
});

describe("config persistence", () => {
  test("data survives read/write cycle", async () => {
    const { storeConnection, updateSetting, readConfig } = await freshImport();
    storeConnection("mydb", {
      driver: "pg",
      host: "localhost",
      port: 5432,
      database: "test",
      credential: "cred1",
    });
    updateSetting("defaults.limit", 42);
    const config = readConfig();
    expect(config.connections!["mydb"]!.driver).toBe("pg");
    expect(config.settings!.defaults!.limit).toBe(42);
  });

  test("config file is valid JSON", async () => {
    const { storeConnection } = await freshImport();
    storeConnection("test", { driver: "sqlite", path: "/test.db" });
    const configPath = join(tempDir, "agent-sql", "config.json");
    const raw = readFileSync(configPath, "utf8");
    expect(() => JSON.parse(raw)).not.toThrow();
  });
});
