import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson } from "../../lib/output.ts";
import { handleActionError, resolveConnectionAlias } from "../action-helpers.ts";

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
      const connectionAlias = resolveConnectionAlias(opts, schema);

      try {
        const driver = await resolveDriver({ connection: connectionAlias });
        try {
          const tables = await driver.getTables({ includeSystem: opts.includeSystem });
          printJson({ tables });
        } finally {
          await driver.close();
        }
      } catch (err) {
        handleActionError(err, connectionAlias);
      }
    });
}
