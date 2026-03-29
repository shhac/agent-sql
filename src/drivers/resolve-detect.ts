import { existsSync } from "node:fs";
import { resolve } from "node:path";
import type { Driver } from "./types";

export const DRIVER_URL_PATTERNS: [RegExp, Driver][] = [
  [/^postgres(ql)?:\/\//, "pg"],
  [/^mysql:\/\//, "mysql"],
  [/^mariadb:\/\//, "mariadb"],
  [/^sqlite:\/\//, "sqlite"],
  [/^snowflake:\/\//, "snowflake"],
  [/^cockroachdb:\/\//, "cockroachdb"],
  [/^duckdb:\/\//, "duckdb"],
];

export const SQLITE_FILE_EXTENSIONS = [".sqlite", ".db", ".sqlite3", ".db3"];

export const DUCKDB_FILE_EXTENSIONS = [".duckdb"];

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
  if (DUCKDB_FILE_EXTENSIONS.some((ext) => lower.endsWith(ext))) {
    return "duckdb";
  }

  return undefined;
};

export const isConnectionUrl = (value: string): boolean =>
  DRIVER_URL_PATTERNS.some(([pattern]) => pattern.test(value));

export const isFilePath = (value: string): boolean => {
  const lower = value.toLowerCase();
  if (SQLITE_FILE_EXTENSIONS.some((ext) => lower.endsWith(ext))) {
    return true;
  }
  if (DUCKDB_FILE_EXTENSIONS.some((ext) => lower.endsWith(ext))) {
    return true;
  }
  if (value.startsWith("/") || value.startsWith("./") || value.startsWith("../")) {
    return existsSync(resolve(value));
  }
  return false;
};
