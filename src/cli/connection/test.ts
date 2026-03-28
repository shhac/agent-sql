import type { Command } from "commander";
import { printJson } from "../../lib/output.ts";
import { handleActionError, withDriver } from "../action-helpers.ts";

export function registerTest(connection: Command): void {
  connection
    .command("test")
    .description("Test connectivity for a connection")
    .argument("[alias]", "Connection alias to test (defaults to default connection)")
    .action(async (alias?: string) => {
      try {
        const testAlias = alias ?? "default";
        await withDriver({ connection: alias }, async (driver) => {
          const result = await driver.query("SELECT 1");
          printJson({
            ok: true,
            connection: testAlias,
            rows: result.rows,
          });
        });
      } catch (err) {
        handleActionError(err, alias);
      }
    });
}
