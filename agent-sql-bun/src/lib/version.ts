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
    const maxDepth = 6;
    const state = { dir: dirname(fileURLToPath(import.meta.url)), remaining: maxDepth };
    while (state.remaining > 0) {
      const candidate = join(state.dir, "package.json");
      if (existsSync(candidate)) {
        const raw = readFileSync(candidate, "utf8");
        const pkg = JSON.parse(raw) as { version?: unknown };
        return typeof pkg.version === "string" ? pkg.version.trim() || "unknown" : "unknown";
      }
      const parent = dirname(state.dir);
      if (parent === state.dir) {
        break;
      }
      state.dir = parent;
      state.remaining -= 1;
    }
  } catch {
    // fall through
  }

  return "unknown";
};

const version = resolveVersion();

export const getVersion = (): string => version;
