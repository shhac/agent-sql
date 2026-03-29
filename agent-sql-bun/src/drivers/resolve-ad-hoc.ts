import { resolve } from "node:path";
import type { DriverConnection } from "./types";
import { connectPg } from "./pg";
import { connectSqlite } from "./sqlite";
import { connectMysql } from "./mysql";
import { connectSnowflake } from "./snowflake";
import { connectDuckDb } from "./duckdb";
import {
  isConnectionUrl,
  isFilePath,
  detectDriverFromUrl,
  DUCKDB_FILE_EXTENSIONS,
} from "./resolve-detect";

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

type AdHocOpts = {
  connectionStr: string;
  write: boolean;
  trackDriver: (connection: DriverConnection) => DriverConnection;
};

export const resolveAdHocConnection = async (
  opts: AdHocOpts,
): Promise<DriverConnection | undefined> => {
  const { connectionStr, write, trackDriver } = opts;
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

    if (driver === "duckdb") {
      rejectAdHocUrlWrite(write);
      const filePath = connectionStr.replace(/^duckdb:\/\//, "");
      const path = filePath ? resolve(filePath) : undefined;
      return trackDriver(await connectDuckDb({ path, readonly: true }));
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
    const pgLike = driver === "pg" || driver === "cockroachdb";
    const defaultPort = pgLike ? (driver === "cockroachdb" ? 26257 : 5432) : 3306;
    const defaultDb = pgLike ? (driver === "cockroachdb" ? "defaultdb" : "postgres") : "mysql";
    const connectOpts = {
      host: urlParts.host,
      port: urlParts.port || defaultPort,
      database: urlParts.database || defaultDb,
      username: urlParts.username,
      password: urlParts.password,
      readonly: true,
    };

    if (pgLike) {
      return trackDriver(await connectPg(connectOpts));
    }
    const mysqlVariant = driver === "mariadb" ? "mariadb" : "mysql";
    return trackDriver(await connectMysql({ ...connectOpts, variant: mysqlVariant }));
  }

  // File path check — SQLite or DuckDB file
  if (isFilePath(connectionStr)) {
    const absPath = resolve(connectionStr);
    const lower = connectionStr.toLowerCase();
    const isDuckDb = DUCKDB_FILE_EXTENSIONS.some((ext) => lower.endsWith(ext));

    if (isDuckDb) {
      rejectAdHocUrlWrite(write);
      return trackDriver(await connectDuckDb({ path: absPath, readonly: true }));
    }

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
