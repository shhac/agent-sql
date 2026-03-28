import { resolve } from "node:path";
import type { Command } from "commander";
import type { Driver, Connection } from "../../lib/config.ts";
import { updateConnection } from "../../lib/config.ts";
import { getCredential, getCredentialNames } from "../../lib/credentials.ts";
import { printJson, printError } from "../../lib/output.ts";

type UpdateOpts = {
  driver?: Driver;
  host?: string;
  port?: string;
  database?: string;
  path?: string;
  url?: string;
  credential?: string | false;
};

const collectUpdates = (opts: UpdateOpts): Partial<Connection> => {
  const updates: Partial<Connection> = {};

  if (opts.driver) {
    updates.driver = opts.driver;
  }
  if (opts.host) {
    updates.host = opts.host;
  }
  if (opts.port) {
    updates.port = Number(opts.port);
  }
  if (opts.database) {
    updates.database = opts.database;
  }
  if (opts.url) {
    updates.url = opts.url;
  }
  if (opts.path) {
    updates.path = resolve(opts.path);
  }

  if (opts.credential === false) {
    updates.credential = undefined;
  } else if (typeof opts.credential === "string") {
    updates.credential = opts.credential;
  }

  return updates;
};

export function registerUpdate(connection: Command): void {
  connection
    .command("update")
    .description("Update a saved connection")
    .argument("<alias>", "Connection alias to update")
    .option("--driver <driver>", "Database driver: pg, cockroachdb, sqlite, duckdb, mysql, or snowflake")
    .option("--host <host>", "Database host")
    .option("--port <port>", "Database port")
    .option("--database <db>", "Database name")
    .option("--path <path>", "Path to SQLite database file (resolved to absolute)")
    .option("--url <url>", "Connection URL")
    .option("--credential <name>", "Credential alias for authentication")
    .option("--no-credential", "Remove credential from connection")
    .action((alias: string, opts: UpdateOpts) => {
      try {
        if (typeof opts.credential === "string") {
          const cred = getCredential(opts.credential);
          if (!cred) {
            const available = getCredentialNames();
            throw new Error(
              `Credential "${opts.credential}" not found. Available: ${available.join(", ") || "(none)"}. Run: agent-sql credential add <alias> --username <user> --password <pass>`,
            );
          }
        }

        const updates = collectUpdates(opts);
        updateConnection(alias, updates);
        printJson({ ok: true, alias, updated: Object.keys(updates) });
      } catch (err) {
        printError({ message: err instanceof Error ? err.message : "Failed to update connection" });
      }
    });
}
