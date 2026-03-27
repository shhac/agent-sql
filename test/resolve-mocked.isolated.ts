import { describe, test, expect, beforeEach, afterEach, mock } from "bun:test";
import type { DriverConnection } from "../src/drivers/types";

const fakeDriver: DriverConnection = {
  query: async () => ({ columns: [], rows: [] }),
  getTables: async () => [],
  describeTable: async () => [],
  getIndexes: async () => [],
  getConstraints: async () => [],
  searchSchema: async () => ({ tables: [], columns: [] }),
  close: async () => {},
};

mock.module("../src/drivers/pg", () => ({
  connectPg: async () => fakeDriver,
}));

mock.module("../src/drivers/sqlite", () => ({
  connectSqlite: async () => fakeDriver,
}));

mock.module("../src/drivers/mysql", () => ({
  connectMysql: async () => fakeDriver,
}));

// Shared state for config/credential mocks
let mockConnections: Record<string, unknown> = {};
let mockDefaultAlias: string | undefined;
let mockCredentials: Record<string, unknown> = {};

mock.module("../src/lib/config", () => ({
  getConnection: (alias: string) => mockConnections[alias],
  getConnections: () => mockConnections,
  getDefaultConnectionAlias: () => mockDefaultAlias,
}));

mock.module("../src/lib/credentials", () => ({
  getCredential: (name: string) => mockCredentials[name] ?? null,
}));

mock.module("../src/lib/cleanup", () => ({
  setActiveDriver: () => {},
  clearActiveDriver: () => {},
}));

const { resolveDriver, detectDriverFromUrl } = await import("../src/drivers/resolve");

let savedEnv: string | undefined;

beforeEach(() => {
  mockConnections = {};
  mockDefaultAlias = undefined;
  mockCredentials = {};
  savedEnv = process.env.AGENT_SQL_CONNECTION;
  delete process.env.AGENT_SQL_CONNECTION;
});

afterEach(() => {
  if (savedEnv === undefined) {
    delete process.env.AGENT_SQL_CONNECTION;
  } else {
    process.env.AGENT_SQL_CONNECTION = savedEnv;
  }
});

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

  test("returns undefined for unrecognized URLs", () => {
    expect(detectDriverFromUrl("http://example.com")).toBeUndefined();
  });

  test("file extension detection is case-insensitive", () => {
    expect(detectDriverFromUrl("/data/APP.DB")).toBe("sqlite");
  });
});

describe("connection alias resolution", () => {
  test("uses explicit alias", async () => {
    mockConnections = {
      myconn: { driver: "sqlite", path: "/test.db" },
    };
    const driver = await resolveDriver({ connection: "myconn" });
    expect(driver).toBeDefined();
  });

  test("uses AGENT_SQL_CONNECTION env var when no explicit alias", async () => {
    process.env.AGENT_SQL_CONNECTION = "envconn";
    mockConnections = {
      envconn: { driver: "sqlite", path: "/test.db" },
    };
    const driver = await resolveDriver();
    expect(driver).toBeDefined();
  });

  test("uses default connection when no explicit or env alias", async () => {
    mockDefaultAlias = "defconn";
    mockConnections = {
      defconn: { driver: "sqlite", path: "/test.db" },
    };
    const driver = await resolveDriver();
    expect(driver).toBeDefined();
  });

  test("throws with listing when no alias resolvable", async () => {
    mockConnections = { a: {}, b: {} };
    await expect(resolveDriver()).rejects.toThrow(
      /No connection specified.*Available connections: a, b/,
    );
  });

  test("throws with (none configured) when no connections exist", async () => {
    await expect(resolveDriver()).rejects.toThrow(/\(none configured\)/);
  });
});

describe("unknown connection error", () => {
  test("lists available connections on unknown alias", async () => {
    mockConnections = { prod: { driver: "pg" }, staging: { driver: "pg" } };
    await expect(resolveDriver({ connection: "nope" })).rejects.toThrow(
      /Unknown connection 'nope'.*Available connections: prod, staging/,
    );
  });

  test("shows (none configured) when no connections on unknown alias", async () => {
    mockConnections = {};
    await expect(resolveDriver({ connection: "nope" })).rejects.toThrow(
      /Unknown connection 'nope'.*\(none configured\)/,
    );
  });
});

describe("write permission checks", () => {
  test("credential with writePermission=false rejects write", async () => {
    mockConnections = {
      myconn: { driver: "sqlite", path: "/test.db", credential: "cred1" },
    };
    mockCredentials = {
      cred1: { writePermission: false },
    };
    await expect(resolveDriver({ connection: "myconn", write: true })).rejects.toThrow(
      /writePermission disabled/,
    );
  });

  test("PG without credential rejects write", async () => {
    mockConnections = {
      myconn: { driver: "pg", host: "localhost", database: "test" },
    };
    await expect(resolveDriver({ connection: "myconn", write: true })).rejects.toThrow(
      /PostgreSQL.*requires a credential with writePermission/,
    );
  });

  test("MySQL without credential rejects write", async () => {
    mockConnections = {
      myconn: { driver: "mysql", host: "localhost", database: "test" },
    };
    await expect(resolveDriver({ connection: "myconn", write: true })).rejects.toThrow(
      /MySQL.*requires a credential with writePermission/,
    );
  });

  test("SQLite without credential allows write", async () => {
    mockConnections = {
      myconn: { driver: "sqlite", path: "/test.db" },
    };
    const driver = await resolveDriver({ connection: "myconn", write: true });
    expect(driver).toBeDefined();
  });
});

describe("unknown driver error", () => {
  test("throws for unsupported driver in connection config", async () => {
    mockConnections = {
      myconn: { url: "http://example.com" },
    };
    await expect(resolveDriver({ connection: "myconn" })).rejects.toThrow(
      /Cannot determine driver type/,
    );
  });
});

describe("driver resolution from connection config", () => {
  test("uses explicit driver field", async () => {
    mockConnections = {
      myconn: { driver: "sqlite", path: "/test.db" },
    };
    const driver = await resolveDriver({ connection: "myconn" });
    expect(driver).toBeDefined();
  });

  test("auto-detects driver from postgres URL", async () => {
    mockConnections = {
      myconn: {
        url: "postgres://localhost/test",
        host: "localhost",
        database: "test",
        credential: "cred1",
      },
    };
    mockCredentials = {
      cred1: { username: "user", password: "pass", writePermission: false },
    };
    const driver = await resolveDriver({ connection: "myconn" });
    expect(driver).toBeDefined();
  });

  test("auto-detects driver from mysql URL", async () => {
    mockConnections = {
      myconn: {
        url: "mysql://localhost/test",
        host: "localhost",
        database: "test",
        credential: "cred1",
      },
    };
    mockCredentials = {
      cred1: { username: "user", password: "pass", writePermission: false },
    };
    const driver = await resolveDriver({ connection: "myconn" });
    expect(driver).toBeDefined();
  });
});
