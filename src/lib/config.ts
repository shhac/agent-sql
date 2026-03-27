import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { getConfigDir, ensureConfigDir } from "./paths";

export type Driver = "pg" | "sqlite" | "mysql";

export type Connection = {
  driver: Driver;
  host?: string;
  port?: number;
  database?: string;
  path?: string;
  url?: string;
  credential?: string;
};

export type DefaultsSettings = {
  limit?: number;
  format?: string;
};

export type QuerySettings = {
  timeout?: number;
  maxRows?: number;
};

export type TruncationSettings = {
  maxLength?: number;
};

export type Settings = {
  defaults?: DefaultsSettings;
  query?: QuerySettings;
  truncation?: TruncationSettings;
};

export type Config = {
  default_connection?: string;
  connections: Record<string, Connection>;
  settings: Settings;
};

const configPath = (): string => join(getConfigDir(), "config.json");

let cachedConfig: Config | null = null;

export const clearConfigCache = (): void => {
  cachedConfig = null;
};

export const readConfig = (): Config => {
  if (cachedConfig) {
    return cachedConfig;
  }

  const path = configPath();
  if (!existsSync(path)) {
    return { connections: {}, settings: {} };
  }
  try {
    const raw = JSON.parse(readFileSync(path, "utf8")) as Partial<Config>;
    cachedConfig = {
      default_connection: raw.default_connection,
      connections: raw.connections ?? {},
      settings: raw.settings ?? {},
    };
    return cachedConfig;
  } catch {
    return { connections: {}, settings: {} };
  }
};

export const writeConfig = (config: Config): void => {
  cachedConfig = null;
  const dir = ensureConfigDir();
  writeFileSync(join(dir, "config.json"), `${JSON.stringify(config, null, 2)}\n`, "utf8");
};

export const getConnection = (alias: string): Connection | undefined =>
  readConfig().connections[alias];

export const getConnections = (): Record<string, Connection> => readConfig().connections;

export const getDefaultConnectionAlias = (): string | undefined => readConfig().default_connection;

const validConnectionAliases = (config: Config): string =>
  Object.keys(config.connections).join(", ") || "(none)";

export const storeConnection = (alias: string, connection: Connection): void => {
  const config = readConfig();
  config.connections[alias] = connection;
  if (!config.default_connection) {
    config.default_connection = alias;
  }
  writeConfig(config);
};

export const removeConnection = (alias: string): void => {
  const config = readConfig();
  if (!config.connections[alias]) {
    throw new Error(`Unknown connection: "${alias}". Valid: ${validConnectionAliases(config)}`);
  }
  delete config.connections[alias];
  if (config.default_connection === alias) {
    const remaining = Object.keys(config.connections);
    config.default_connection = remaining.length > 0 ? remaining[0] : undefined;
  }
  writeConfig(config);
};

export const setDefaultConnection = (alias: string): void => {
  const config = readConfig();
  if (!config.connections[alias]) {
    throw new Error(`Unknown connection: "${alias}". Valid: ${validConnectionAliases(config)}`);
  }
  config.default_connection = alias;
  writeConfig(config);
};

export const updateConnection = (alias: string, updates: Partial<Connection>): void => {
  const config = readConfig();
  if (!config.connections[alias]) {
    throw new Error(`Unknown connection: "${alias}". Valid: ${validConnectionAliases(config)}`);
  }
  config.connections[alias] = { ...config.connections[alias]!, ...updates };
  writeConfig(config);
};

export const getSettings = (): Settings => readConfig().settings;

export const getSetting = (key: string): unknown =>
  key.split(".").reduce<unknown>((acc, part) => {
    if (acc === null || acc === undefined || typeof acc !== "object") {
      return undefined;
    }
    return (acc as Record<string, unknown>)[part];
  }, getSettings());

const ensureNestedPath = (obj: Record<string, unknown>, parts: string[]): Record<string, unknown> =>
  parts.reduce<Record<string, unknown>>((acc, part) => {
    if (acc[part] === undefined || typeof acc[part] !== "object") {
      acc[part] = {};
    }
    return acc[part] as Record<string, unknown>;
  }, obj);

export const updateSetting = (key: string, value: unknown): void => {
  const config = readConfig();
  const parts = key.split(".");
  const parent = ensureNestedPath(config.settings as Record<string, unknown>, parts.slice(0, -1));
  parent[parts.at(-1)!] = value;
  writeConfig(config);
};

export const resetSettings = (): void => {
  const config = readConfig();
  config.settings = {};
  writeConfig(config);
};
