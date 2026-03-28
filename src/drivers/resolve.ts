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
import { connectSnowflake } from "./snowflake";

type ResolveOpts = {
  connection?: string;
  write?: boolean;
};

const DRIVER_URL_PATTERNS: [RegExp, Driver][] = [
  [/^postgres(ql)?:\/\//, "pg"],
  [/^mysql:\/\//, "mysql"],
  [/^mariadb:\/\//, "mysql"],
  [/^sqlite:\/\//, "sqlite"],
  [/^snowflake:\/\//, "snowflake"],
];

export const SQLITE_FILE_EXTENSIONS = [".sqlite", ".db", ".sqlite3", ".db3"];

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
}): void => {
  if (!opts.write) {
    return;
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

  if (
    (opts.driver === "pg" || opts.driver === "mysql" || opts.driver === "snowflake") &&
    !opts.credential
  ) {
    const driverName =
      opts.driver === "pg" ? "PostgreSQL" : opts.driver === "mysql" ? "MySQL" : "Snowflake";
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
      return trackDriver(await connectSqlite({ path: resolve(filePath), readonly: true }));
    }

    if (driver === "snowflake") {
      rejectAdHocUrlWrite(write);
      const token = process.env.AGENT_SQL_SNOWFLAKE_TOKEN;
      if (!token) {
        throw Object.assign(
          new Error(
            "Ad-hoc Snowflake connections require AGENT_SQL_SNOWFLAKE_TOKEN environment variable.",
          ),
          { fixableBy: "human" as const },
        );
      }
      const parsed = new URL(connectionStr);
      const pathParts = parsed.pathname.replace(/^\//, "").split("/");
      return trackDriver(
        await connectSnowflake({
          account: parsed.hostname,
          database: pathParts[0] || undefined,
          schema: pathParts[1] || undefined,
          warehouse: parsed.searchParams.get("warehouse") ?? undefined,
          role: parsed.searchParams.get("role") ?? undefined,
          token,
          readonly: true,
        }),
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
    const absPath = resolve(connectionStr);
    const fileExists = await Bun.file(absPath).exists();
    if (!fileExists && !write) {
      throw Object.assign(new Error(`SQLite database not found: ${connectionStr}`), {
        hint: "Check the file path, or use --write to create a new database.",
        fixableBy: "agent" as const,
      });
    }
    return trackDriver(
      await connectSqlite({ path: absPath, readonly: !write, create: !fileExists }),
    );
  }

  return undefined;
};

type ConfigConnectOpts = {
  conn: Connection;
  credential: Credential | null;
  readonly: boolean;
  alias: string;
};

const connectPgFromConfig = async (opts: ConfigConnectOpts): Promise<DriverConnection> => {
  if (!opts.credential?.username || !opts.credential?.password) {
    throw Object.assign(
      new Error(
        `PostgreSQL connection '${opts.alias}' requires a credential with username and password.`,
      ),
      {
        hint: "Add a credential with: agent-sql credential add <name> --username <user> --password <pass>",
        fixableBy: "human" as const,
      },
    );
  }

  return connectPg({
    host: opts.conn.host ?? "localhost",
    port: opts.conn.port ?? 5432,
    database: opts.conn.database ?? "postgres",
    username: opts.credential.username,
    password: opts.credential.password,
    readonly: opts.readonly,
  });
};

const connectSqliteFromConfig = async (opts: ConfigConnectOpts): Promise<DriverConnection> => {
  const path = opts.conn.path ?? opts.conn.url?.replace(/^sqlite:\/\//, "");
  if (!path) {
    throw new Error(
      `SQLite connection '${opts.alias}' requires a path. Set 'path' on the connection or use a sqlite:// URL.`,
    );
  }

  return connectSqlite({ path, readonly: opts.readonly });
};

const connectMysqlFromConfig = async (opts: ConfigConnectOpts): Promise<DriverConnection> => {
  if (!opts.credential?.username || !opts.credential?.password) {
    throw Object.assign(
      new Error(
        `MySQL connection '${opts.alias}' requires a credential with username and password.`,
      ),
      {
        hint: "Add a credential with: agent-sql credential add <name> --username <user> --password <pass>",
        fixableBy: "human" as const,
      },
    );
  }

  return connectMysql({
    host: opts.conn.host ?? "localhost",
    port: opts.conn.port ?? 3306,
    database: opts.conn.database ?? "mysql",
    username: opts.credential.username,
    password: opts.credential.password,
    readonly: opts.readonly,
  });
};

const connectSnowflakeFromConfig = async (opts: ConfigConnectOpts): Promise<DriverConnection> => {
  if (!opts.credential?.password) {
    throw Object.assign(
      new Error(
        `Snowflake connection '${opts.alias}' requires a credential with a PAT (personal access token) as the password.`,
      ),
      {
        hint: "Add a credential with: agent-sql credential add <name> --password <pat_secret>",
        fixableBy: "human" as const,
      },
    );
  }

  return connectSnowflake({
    account: opts.conn.account ?? "",
    database: opts.conn.database,
    schema: opts.conn.schema,
    warehouse: opts.conn.warehouse,
    role: opts.conn.role,
    token: opts.credential.password,
    readonly: opts.readonly,
  });
};

const configConnectBuilders: Record<
  Driver,
  (opts: ConfigConnectOpts) => Promise<DriverConnection>
> = {
  pg: connectPgFromConfig,
  sqlite: connectSqliteFromConfig,
  mysql: connectMysqlFromConfig,
  snowflake: connectSnowflakeFromConfig,
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

  const builder = configConnectBuilders[driver];
  if (!builder) {
    throw new Error(`Unknown driver '${driver}'. Supported drivers: pg, sqlite, mysql, snowflake.`);
  }
  return trackDriver(await builder({ conn, credential, readonly, alias }));
};
