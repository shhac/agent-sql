import type { Command } from "commander";
import { removeConnection } from "../../lib/config.ts";
import { printJson, printError } from "../../lib/output.ts";

export function registerRemove(connection: Command): void {
  connection
    .command("remove")
    .description("Remove a saved connection")
    .argument("<alias>", "Connection alias to remove")
    .action((alias: string) => {
      try {
        removeConnection(alias);
        printJson({ ok: true, removed: alias });
      } catch (err) {
        printError({ message: err instanceof Error ? err.message : "Failed to remove connection" });
      }
    });
}
