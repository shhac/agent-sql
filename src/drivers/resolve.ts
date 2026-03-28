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
import { detectDriverFromUrl } from "./resolve-detect";
import { resolveAdHocConnection } from "./resolve-ad-hoc";

export {
  detectDriverFromUrl,
  isConnectionUrl,
  isFilePath,
  SQLITE_FILE_EXTENSIONS,
} from "./resolve-detect";

type ResolveOpts = {
  connection?: string;
  write?: boolean;
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
    "Cannot determine driver type. Set the 'driver' field on the connection or use a URL with a recognizable scheme (postgres://, cockroachdb://, sqlite://, mysql://).",
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
    (opts.driver === "pg" ||
      opts.driver === "cockroachdb" ||
      opts.driver === "mysql" ||
      opts.driver === "snowflake") &&
    !opts.credential
  ) {
    const driverNames: Record<string, string> = {
      pg: "PostgreSQL",
      cockroachdb: "CockroachDB",
      mysql: "MySQL",
      snowflake: "Snowflake",
    };
    const driverName = driverNames[opts.driver] ?? opts.driver;
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

type ConfigConnectOpts = {
  conn: Connection;
  credential: Credential | null;
  readonly: boolean;
  alias: string;
};

const connectPgLike = async (
  opts: ConfigConnectOpts,
  defaults: { label: string; port: number; database: string },
): Promise<DriverConnection> => {
  if (!opts.credential?.username || !opts.credential?.password) {
    throw Object.assign(
      new Error(
        `${defaults.label} connection '${opts.alias}' requires a credential with username and password.`,
      ),
      {
        hint: "Add a credential with: agent-sql credential add <name> --username <user> --password <pass>",
        fixableBy: "human" as const,
      },
    );
  }

  return connectPg({
    host: opts.conn.host ?? "localhost",
    port: opts.conn.port ?? defaults.port,
    database: opts.conn.database ?? defaults.database,
    username: opts.credential.username,
    password: opts.credential.password,
    readonly: opts.readonly,
  });
};

const connectPgFromConfig = async (opts: ConfigConnectOpts): Promise<DriverConnection> =>
  connectPgLike(opts, { label: "PostgreSQL", port: 5432, database: "postgres" });

const connectCockroachDbFromConfig = async (opts: ConfigConnectOpts): Promise<DriverConnection> =>
  connectPgLike(opts, { label: "CockroachDB", port: 26257, database: "defaultdb" });

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
  cockroachdb: connectCockroachDbFromConfig,
  sqlite: connectSqliteFromConfig,
  mysql: connectMysqlFromConfig,
  snowflake: connectSnowflakeFromConfig,
};

export const resolveDriver = async (opts?: ResolveOpts): Promise<DriverConnection> => {
  const alias = resolveAlias(opts?.connection);
  const write = opts?.write ?? false;

  // Try ad-hoc connection (URL or file path) before config lookup
  const adHoc = await resolveAdHocConnection({ connectionStr: alias, write, trackDriver });
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
    throw new Error(
      `Unknown driver '${driver}'. Supported drivers: pg, cockroachdb, sqlite, mysql, snowflake.`,
    );
  }
  return trackDriver(await builder({ conn, credential, readonly, alias }));
};
