import { existsSync } from "node:fs";
import { resolve } from "node:path";
import type { Driver } from "./types";
import type { DriverConnection } from "./types";
import type { Connection } from "../lib/config";
import { getConnection, getConnections, getDefaultConnectionAlias } from "../lib/config";
import { getCredential, type Credential } from "../lib/credentials";
import { setActiveDriver, clearActiveDriver } from "../lib/cleanup";
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

export const detectDriverFromUrl = (url: string): Driver | undefined => {
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
      `No connection specified and no default configured. Available connections: ${listing}. ` +
        "Tip: -c also accepts file paths (e.g. ./data.db) and connection URLs (e.g. postgres://user:pass@host/db).",
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

const trackDriver = (connection: DriverConnection): DriverConnection => {
  setActiveDriver(connection);
  const originalClose = connection.close.bind(connection);
  return {
    ...connection,
    close: async () => {
      clearActiveDriver();
      return originalClose();
    },
  };
};

export const isConnectionUrl = (value: string): boolean =>
  DRIVER_URL_PATTERNS.some(([pattern]) => pattern.test(value));

export const isFilePath = (value: string): boolean => {
  const lower = value.toLowerCase();
  if (SQLITE_FILE_EXTENSIONS.some((ext) => lower.endsWith(ext))) {
    return true;
  }
  if (value.startsWith("/") || value.startsWith("./") || value.startsWith("../")) {
    return existsSync(resolve(value));
  }
  return false;
};

const parseConnectionUrl = (
  url: string,
): { host: string; port: number; database: string; username: string; password: string } => {
  const parsed = new URL(url);
  return {
    host: parsed.hostname || "localhost",
    port: parsed.port ? Number(parsed.port) : 0,
    database: parsed.pathname.replace(/^\//, "") || "",
    username: decodeURIComponent(parsed.username),
    password: decodeURIComponent(parsed.password),
  };
};

const rejectAdHocUrlWrite = (write: boolean): void => {
  if (!write) {
    return;
  }
  throw Object.assign(
    new Error(
      "Write mode is not available for ad-hoc connections. Set up a named connection with a write-enabled credential to use --write.",
    ),
    { fixableBy: "human" as const },
  );
};

const resolveAdHocConnection = async (
  connectionStr: string,
  write: boolean,
): Promise<DriverConnection | undefined> => {
  // URL check first — more recognizable shape than file paths
  if (isConnectionUrl(connectionStr)) {
    const driver = detectDriverFromUrl(connectionStr);
    if (!driver) {
      return undefined;
    }

    if (driver === "sqlite") {
      rejectAdHocUrlWrite(write);
      const filePath = connectionStr.replace(/^sqlite:\/\//, "");
      return trackDriver(
        await connectSqlite({ path: resolve(filePath), readonly: true }),
      );
    }

    rejectAdHocUrlWrite(write);
    const urlParts = parseConnectionUrl(connectionStr);
    const defaultPort = driver === "pg" ? 5432 : 3306;
    const defaultDb = driver === "pg" ? "postgres" : "mysql";
    const connectOpts = {
      host: urlParts.host,
      port: urlParts.port || defaultPort,
      database: urlParts.database || defaultDb,
      username: urlParts.username,
      password: urlParts.password,
      readonly: true,
    };

    if (driver === "pg") {
      return trackDriver(await connectPg(connectOpts));
    }
    return trackDriver(await connectMysql(connectOpts));
  }

  // File path check — SQLite file, write allowed via --write
  if (isFilePath(connectionStr)) {
    return trackDriver(
      await connectSqlite({ path: resolve(connectionStr), readonly: !write }),
    );
  }

  return undefined;
};

export const resolveDriver = async (opts?: ResolveOpts): Promise<DriverConnection> => {
  const alias = resolveAlias(opts?.connection);
  const write = opts?.write ?? false;

  // Try ad-hoc connection (URL or file path) before config lookup
  const adHoc = await resolveAdHocConnection(alias, write);
  if (adHoc) {
    return adHoc;
  }

  const conn = getConnection(alias);

  if (!conn) {
    const available = Object.keys(getConnections());
    const listing = available.length > 0 ? available.join(", ") : "(none configured)";
    throw Object.assign(
      new Error(
        `Unknown connection '${alias}'. Available connections: ${listing}. ` +
          "Tip: -c also accepts file paths (e.g. ./data.db) and connection URLs (e.g. postgres://user:pass@host/db).",
      ),
      { fixableBy: "agent" as const },
    );
  }

  const driver = resolveDriverType(conn);
  const credential = conn.credential ? getCredential(conn.credential) : null;

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

    return trackDriver(
      await connectPg({
        host: conn.host ?? "localhost",
        port: conn.port ?? 5432,
        database: conn.database ?? "postgres",
        username: credential.username,
        password: credential.password,
        readonly,
      }),
    );
  }

  if (driver === "sqlite") {
    const path = conn.path ?? conn.url?.replace(/^sqlite:\/\//, "");
    if (!path) {
      throw new Error(
        `SQLite connection '${alias}' requires a path. Set 'path' on the connection or use a sqlite:// URL.`,
      );
    }

    return trackDriver(await connectSqlite({ path, readonly }));
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

    return trackDriver(
      await connectMysql({
        host: conn.host ?? "localhost",
        port: conn.port ?? 3306,
        database: conn.database ?? "mysql",
        username: credential.username,
        password: credential.password,
        readonly,
      }),
    );
  }

  throw new Error(`Unknown driver '${driver}'. Supported drivers: pg, sqlite, mysql.`);
};
