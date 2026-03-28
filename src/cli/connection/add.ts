import { resolve } from "node:path";
import type { Command } from "commander";
import type { Driver, Connection } from "../../lib/config.ts";
import { storeConnection, setDefaultConnection } from "../../lib/config.ts";
import { getCredential, getCredentialNames } from "../../lib/credentials.ts";
import { printJson, printError } from "../../lib/output.ts";
import {
  detectDriverFromUrl,
  isFilePath,
  SQLITE_FILE_EXTENSIONS,
  DUCKDB_FILE_EXTENSIONS,
} from "../../drivers/resolve.ts";

type AddOpts = {
  driver?: Driver;
  host?: string;
  port?: string;
  database?: string;
  path?: string;
  url?: string;
  credential?: string;
  account?: string;
  warehouse?: string;
  role?: string;
  schema?: string;
  default?: boolean;
};

const resolveDriver = (opts: AddOpts): Driver => {
  if (opts.driver) {
    return opts.driver;
  }
  if (opts.url) {
    const detected = detectDriverFromUrl(opts.url);
    if (detected) {
      return detected;
    }
  }
  if (opts.path) {
    const pathLower = opts.path.toLowerCase();
    if (DUCKDB_FILE_EXTENSIONS.some((ext) => pathLower.endsWith(ext))) {
      return "duckdb";
    }
    return "sqlite";
  }
  throw new Error(
    "Cannot determine driver. Use --driver pg|cockroachdb|sqlite|duckdb|mysql|snowflake, a connection URL, or a file path for SQLite.",
  );
};

const parseConnectionString = (connStr: string, opts: AddOpts): void => {
  // File path → SQLite or DuckDB
  const lower = connStr.toLowerCase();
  if (DUCKDB_FILE_EXTENSIONS.some((ext) => lower.endsWith(ext))) {
    opts.path = connStr;
    return;
  }
  if (SQLITE_FILE_EXTENSIONS.some((ext) => lower.endsWith(ext)) || isFilePath(connStr)) {
    opts.path = connStr;
    return;
  }

  // URL → parse by driver
  const driver = detectDriverFromUrl(connStr);
  if (!driver) {
    throw new Error(
      `Cannot parse connection string: "${connStr}". Expected a URL (postgres://, cockroachdb://, duckdb://, mysql://, snowflake://) or a file path (.db, .sqlite, .duckdb).`,
    );
  }

  if (driver === "sqlite") {
    opts.path = connStr.replace(/^sqlite:\/\//, "");
    return;
  }

  if (driver === "duckdb") {
    opts.path = connStr.replace(/^duckdb:\/\//, "");
    return;
  }

  if (driver === "snowflake") {
    const parsed = new URL(connStr);
    opts.url = connStr;
    if (!opts.account) {
      opts.account = parsed.hostname;
    }
    const [database, schema] = parsed.pathname.replace(/^\//, "").split("/");
    if (!opts.database && database) {
      opts.database = database;
    }
    if (!opts.schema && schema) {
      opts.schema = schema;
    }
    const wh = parsed.searchParams.get("warehouse");
    if (!opts.warehouse && wh) {
      opts.warehouse = wh;
    }
    const role = parsed.searchParams.get("role");
    if (!opts.role && role) {
      opts.role = role;
    }
    return;
  }

  // PG, CockroachDB, or MySQL
  opts.url = connStr;
  const parsed = new URL(connStr);
  if (!opts.host && parsed.hostname) {
    opts.host = parsed.hostname;
  }
  if (!opts.port && parsed.port) {
    opts.port = parsed.port;
  }
  if (!opts.database) {
    const db = parsed.pathname.replace(/^\//, "");
    if (db) {
      opts.database = db;
    }
  }
};

const buildConnection = (opts: AddOpts): Connection => {
  const driver = resolveDriver(opts);
  const conn: Connection = { driver };

  if (opts.url) {
    conn.url = opts.url;
  }
  if (opts.host) {
    conn.host = opts.host;
  }
  if (opts.port) {
    conn.port = Number(opts.port);
  }
  if (opts.database) {
    conn.database = opts.database;
  }
  if (opts.credential) {
    conn.credential = opts.credential;
  }
  if (opts.path) {
    conn.path = resolve(opts.path);
  }
  if (opts.account) {
    conn.account = opts.account;
  }
  if (opts.warehouse) {
    conn.warehouse = opts.warehouse;
  }
  if (opts.role) {
    conn.role = opts.role;
  }
  if (opts.schema) {
    conn.schema = opts.schema;
  }

  return conn;
};

export function registerAdd(connection: Command): void {
  connection
    .command("add")
    .description("Add a SQL connection")
    .argument("<alias>", "Short name for this connection (e.g. local, staging, prod)")
    .argument("[connection-string]", "Connection URL or file path (auto-detects driver)")
    .option("--driver <driver>", "Database driver: pg, cockroachdb, sqlite, mysql, or snowflake")
    .option("--host <host>", "Database host")
    .option("--port <port>", "Database port")
    .option("--database <db>", "Database name")
    .option("--path <path>", "Path to SQLite database file (resolved to absolute)")
    .option("--url <url>", "Connection URL (auto-detects driver from scheme)")
    .option("--account <account>", "Snowflake account identifier (orgname-accountname)")
    .option("--warehouse <warehouse>", "Snowflake warehouse")
    .option("--role <role>", "Snowflake role")
    .option("--schema <schema>", "Default schema")
    .option("--credential <name>", "Credential alias for authentication")
    .option("--default", "Set as default connection")
    .action((...args: [string, string | undefined, AddOpts]) => {
      const [alias, connStr, opts] = args;
      try {
        if (connStr) {
          parseConnectionString(connStr, opts);
        }

        if (opts.credential) {
          const cred = getCredential(opts.credential);
          if (!cred) {
            const available = getCredentialNames();
            throw new Error(
              `Credential "${opts.credential}" not found. Available: ${available.join(", ") || "(none)"}. Run: agent-sql credential add <alias> --username <user> --password <pass>`,
            );
          }
        }

        const conn = buildConnection(opts);

        storeConnection(alias, conn);

        if (opts.default) {
          setDefaultConnection(alias);
        }

        printJson(
          {
            ok: true,
            alias,
            driver: conn.driver,
            host: conn.host,
            port: conn.port,
            database: conn.database,
            path: conn.path,
            url: conn.url,
            credential: conn.credential,
            account: conn.account,
            warehouse: conn.warehouse,
            role: conn.role,
            schema: conn.schema,
            isDefault: opts.default ?? false,
            hint: "Test with: agent-sql connection test",
          },
          { prune: true },
        );
      } catch (err) {
        printError({ message: err instanceof Error ? err.message : "Failed to add connection" });
      }
    });
}
