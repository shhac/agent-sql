import { resolve } from "node:path";
import type { Command } from "commander";
import type { Driver, Connection } from "../../lib/config.ts";
import { storeConnection, setDefaultConnection } from "../../lib/config.ts";
import { getCredential, getCredentialNames } from "../../lib/credentials.ts";
import { printJson, printError } from "../../lib/output.ts";
import { detectDriverFromUrl } from "../../drivers/resolve.ts";

type AddOpts = {
  driver?: Driver;
  host?: string;
  port?: string;
  database?: string;
  path?: string;
  url?: string;
  credential?: string;
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
    return "sqlite";
  }
  throw new Error(
    "Cannot determine driver. Use --driver pg|sqlite|mysql, --url with a recognizable scheme, or --path for SQLite.",
  );
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

  return conn;
};

export function registerAdd(connection: Command): void {
  connection
    .command("add")
    .description("Add a SQL connection")
    .argument("<alias>", "Short name for this connection (e.g. local, staging, prod)")
    .option("--driver <driver>", "Database driver: pg, sqlite, or mysql")
    .option("--host <host>", "Database host")
    .option("--port <port>", "Database port")
    .option("--database <db>", "Database name")
    .option("--path <path>", "Path to SQLite database file (resolved to absolute)")
    .option("--url <url>", "Connection URL (auto-detects driver from scheme)")
    .option("--credential <name>", "Credential alias for authentication")
    .option("--default", "Set as default connection")
    .action((alias: string, opts: AddOpts) => {
      try {
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
