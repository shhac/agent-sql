import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson } from "../../lib/output.ts";
import { handleActionError } from "../action-helpers.ts";

export function registerTest(connection: Command): void {
  connection
    .command("test")
    .description("Test connectivity for a connection")
    .argument("[alias]", "Connection alias to test (defaults to default connection)")
    .action(async (alias?: string) => {
      try {
        const driver = await resolveDriver({ connection: alias });
        const testAlias = alias ?? "default";

        try {
          const result = await driver.query("SELECT 1");
          printJson({
            ok: true,
            connection: testAlias,
            rows: result.rows,
          });
        } finally {
          await driver.close();
        }
      } catch (err) {
        handleActionError(err, alias);
      }
    });
}
