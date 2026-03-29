import type { Command } from "commander";
import { resetSettings } from "../../lib/config.ts";
import { printError, printJson } from "../../lib/output.ts";

export function registerReset(config: Command): void {
  config
    .command("reset")
    .description("Reset all settings to defaults")
    .action(() => {
      try {
        resetSettings();
        printJson({ ok: true, message: "Settings reset to defaults" });
      } catch (err) {
        printError({ message: err instanceof Error ? err.message : "Reset failed" });
      }
    });
}
