import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson, printError } from "../../lib/output.ts";
import { enhanceError } from "../../lib/errors.ts";

type TablesOpts = {
  connection?: string;
  includeSystem?: boolean;
};

export function registerTables(schema: Command): void {
  schema
    .command("tables")
    .description("List all tables")
    .option("--include-system", "Include system tables (pg_catalog, information_schema)")
    .action(async (opts: TablesOpts) => {
      const connectionAlias = opts.connection ?? (schema.parent?.getOptionValue("connection") as string | undefined);

      try {
        const driver = await resolveDriver({ connection: connectionAlias });
        try {
          const tables = await driver.getTables({ includeSystem: opts.includeSystem });
          printJson({ tables });
        } finally {
          await driver.close();
        }
      } catch (err) {
        const enhanced = enhanceError(err instanceof Error ? err : new Error(String(err)), {
          connectionAlias,
        });
        printError({
          message: enhanced.message,
          hint: enhanced.hint,
          fixableBy: enhanced.fixableBy,
        });
      }
    });
}
