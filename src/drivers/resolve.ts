import type { Driver } from "./types";
import type { DriverConnection } from "./types";
import type { Connection } from "../lib/config";
import { getConnection, getConnections, getDefaultConnectionAlias } from "../lib/config";
import { getCredential, type Credential } from "../lib/credentials";
import { connectPg } from "./pg";
import { connectSqlite } from "./sqlite";
import { connectMysql } from "./mysql";

type ResolveOpts = {
  connection?: string;
  write?: boolean;
};

const DRIVER_URL_PATTERNS: [RegExp, Driver][] = [
  [/^postgres(ql)?:\/\//, "pg"],
  [/^mysql:\/\//, "mysql"],
  [/^mariadb:\/\//, "mysql"],
  [/^sqlite:\/\//, "sqlite"],
];

const SQLITE_FILE_EXTENSIONS = [".sqlite", ".db", ".sqlite3", ".db3"];

const detectDriverFromUrl = (url: string): Driver | undefined => {
  for (const [pattern, driver] of DRIVER_URL_PATTERNS) {
    if (pattern.test(url)) {
      return driver;
    }
  }

  const lower = url.toLowerCase();
  if (SQLITE_FILE_EXTENSIONS.some((ext) => lower.endsWith(ext))) {
    return "sqlite";
  }

  return undefined;
};

const resolveAlias = (explicit?: string): string => {
  if (explicit) {
    return explicit;
  }

  const envAlias = process.env.AGENT_SQL_CONNECTION?.trim();
  if (envAlias) {
    return envAlias;
  }

  const defaultAlias = getDefaultConnectionAlias();
  if (defaultAlias) {
    return defaultAlias;
  }

  const available = Object.keys(getConnections());
  const listing = available.length > 0 ? available.join(", ") : "(none configured)";
  throw Object.assign(
    new Error(
      `No connection specified and no default configured. Available connections: ${listing}`,
    ),
    { fixableBy: "agent" as const },
  );
};

const resolveDriverType = (conn: Connection): Driver => {
  if (conn.driver) {
    return conn.driver;
  }

  if (conn.url) {
    const detected = detectDriverFromUrl(conn.url);
    if (detected) {
      return detected;
    }
  }

  throw new Error(
    "Cannot determine driver type. Set the 'driver' field on the connection or use a URL with a recognizable scheme (postgres://, sqlite://, mysql://).",
  );
};

const checkWritePermission = (opts: {
  driver: Driver;
  credential: Credential | null;
  write: boolean;
  alias: string;
}): boolean => {
  if (!opts.write) {
    return true;
  }

  if (opts.credential && !opts.credential.writePermission) {
    throw Object.assign(
      new Error(
        `Write mode requested but credential for connection '${opts.alias}' has writePermission disabled.`,
      ),
      {
        hint: "Update the credential with --write to enable write permission, or use a different credential.",
        fixableBy: "human" as const,
      },
    );
  }

  if ((opts.driver === "pg" || opts.driver === "mysql") && !opts.credential) {
    const driverName = opts.driver === "pg" ? "PostgreSQL" : "MySQL";
    throw Object.assign(
      new Error(
        `Write mode requested but ${driverName} connection '${opts.alias}' has no credential. ${driverName} requires a credential with writePermission to enable writes.`,
      ),
      {
        hint: "Add a credential with: agent-sql credential add <name> --username <user> --password <pass> --write",
        fixableBy: "human" as const,
      },
    );
  }

  // SQLite without credential is allowed to write
  if (opts.driver === "sqlite" && !opts.credential) {
    return false;
  }

  return false;
};

export const resolveDriver = async (opts?: ResolveOpts): Promise<DriverConnection> => {
  const alias = resolveAlias(opts?.connection);
  const conn = getConnection(alias);

  if (!conn) {
    const available = Object.keys(getConnections());
    const listing = available.length > 0 ? available.join(", ") : "(none configured)";
    throw Object.assign(
      new Error(`Unknown connection '${alias}'. Available connections: ${listing}`),
      { fixableBy: "agent" as const },
    );
  }

  const driver = resolveDriverType(conn);
  const credential = conn.credential ? getCredential(conn.credential) : null;
  const write = opts?.write ?? false;

  // checkWritePermission throws if write is requested but not allowed
  checkWritePermission({ driver, credential, write, alias });

  const readonly = !write;

  if (driver === "pg") {
    if (!credential?.username || !credential?.password) {
      throw Object.assign(
        new Error(
          `PostgreSQL connection '${alias}' requires a credential with username and password.`,
        ),
        {
          hint: "Add a credential with: agent-sql credential add <name> --username <user> --password <pass>",
          fixableBy: "human" as const,
        },
      );
    }

    return connectPg({
      host: conn.host ?? "localhost",
      port: conn.port ?? 5432,
      database: conn.database ?? "postgres",
      username: credential.username,
      password: credential.password,
      readonly,
    });
  }

  if (driver === "sqlite") {
    const path = conn.path ?? conn.url?.replace(/^sqlite:\/\//, "");
    if (!path) {
      throw new Error(
        `SQLite connection '${alias}' requires a path. Set 'path' on the connection or use a sqlite:// URL.`,
      );
    }

    return connectSqlite({ path, readonly });
  }

  if (driver === "mysql") {
    if (!credential?.username || !credential?.password) {
      throw Object.assign(
        new Error(`MySQL connection '${alias}' requires a credential with username and password.`),
        {
          hint: "Add a credential with: agent-sql credential add <name> --username <user> --password <pass>",
          fixableBy: "human" as const,
        },
      );
    }

    return connectMysql({
      host: conn.host ?? "localhost",
      port: conn.port ?? 3306,
      database: conn.database ?? "mysql",
      username: credential.username,
      password: credential.password,
      readonly,
    });
  }

  throw new Error(`Unknown driver '${driver}'. Supported drivers: pg, sqlite, mysql.`);
};
