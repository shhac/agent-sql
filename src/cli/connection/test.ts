import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson, printError } from "../../lib/output.ts";

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
        const message = err instanceof Error ? err.message : "Connection test failed";
        const hint =
          err instanceof Error && "hint" in err
            ? (err as Error & { hint: string }).hint
            : undefined;
        const fixableBy =
          err instanceof Error && "fixableBy" in err
            ? ((err as Error & { fixableBy: string }).fixableBy as "agent" | "human" | "retry")
            : undefined;
        printError({ message, hint, fixableBy });
      }
    });
}
