import type { Command } from "commander";
import { getSetting } from "../../lib/config.ts";
import { printError, printJson } from "../../lib/output.ts";
import { VALID_KEYS } from "./valid-keys.ts";

export function registerGet(config: Command): void {
  config
    .command("get")
    .description("Get a config value")
    .argument("<key>", "Config key (e.g. defaults.limit)")
    .action((key: string) => {
      const def = VALID_KEYS.find((k) => k.key === key);
      if (!def) {
        const validKeys = VALID_KEYS.map((k) => k.key).join(", ");
        printError({
          message: `Unknown key: "${key}". Valid keys: ${validKeys}`,
          fixableBy: "agent",
        });
        return;
      }

      try {
        const value = getSetting(key);
        printJson({ key, value: value ?? null });
      } catch (err) {
        printError({ message: err instanceof Error ? err.message : "Get failed" });
      }
    });
}
