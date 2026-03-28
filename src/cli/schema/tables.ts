import type { Command } from "commander";
import { printJson } from "../../lib/output.ts";
import { handleActionError, resolveConnectionAlias, withDriver } from "../action-helpers.ts";

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
        await withDriver({ connection: connectionAlias }, async (driver) => {
          const tables = await driver.getTables({ includeSystem: opts.includeSystem });
          printJson({ tables });
        });
      } catch (err) {
        handleActionError(err, connectionAlias);
      }
    });
}
