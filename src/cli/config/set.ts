import type { Command } from "commander";
import { updateSetting } from "../../lib/config.ts";
import { printError, printJson } from "../../lib/output.ts";
import { VALID_KEYS, parseConfigValue } from "./valid-keys.ts";

export function registerSet(config: Command): void {
  config
    .command("set")
    .description("Set a config value")
    .argument("<key>", "Config key (e.g. defaults.limit)")
    .argument("<value>", "Value to set")
    .action((key: string, rawValue: string) => {
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
        const value = parseConfigValue(key, rawValue);
        updateSetting(key, value);
        printJson({ ok: true, key, value });
      } catch (err) {
        printError({
          message: err instanceof Error ? err.message : "Set failed",
          fixableBy: "agent",
        });
      }
    });
}
