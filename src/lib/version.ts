import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

declare const AGENT_SQL_BUILD_VERSION: string | undefined;

const resolveVersion = (): string => {
  if (typeof AGENT_SQL_BUILD_VERSION === "string" && AGENT_SQL_BUILD_VERSION) {
    return AGENT_SQL_BUILD_VERSION;
  }

  const envVersion =
    process.env.AGENT_SQL_VERSION?.trim() || process.env.npm_package_version?.trim();
  if (envVersion) {
    return envVersion;
  }

  try {
    const walkUp = (dir: string, remaining: number): string | undefined => {
      if (remaining <= 0) {
        return undefined;
      }
      const candidate = join(dir, "package.json");
      if (existsSync(candidate)) {
        const raw = readFileSync(candidate, "utf8");
        const pkg = JSON.parse(raw) as { version?: unknown };
        return typeof pkg.version === "string" ? pkg.version.trim() || "unknown" : "unknown";
      }
      const parent = dirname(dir);
      if (parent === dir) {
        return undefined;
      }
      return walkUp(parent, remaining - 1);
    };

    const found = walkUp(dirname(fileURLToPath(import.meta.url)), 6);
    if (found) {
      return found;
    }
  } catch {
    // fall through
  }

  return "unknown";
};

const version = resolveVersion();

export const getVersion = (): string => version;
