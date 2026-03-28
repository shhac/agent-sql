import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson } from "../../lib/output.ts";
import { handleActionError, resolveConnectionAlias } from "../action-helpers.ts";

type IndexesOpts = {
  connection?: string;
};

export function registerIndexes(schema: Command): void {
  schema
    .command("indexes")
    .description("Show indexes for a table or all tables")
    .argument("[table]", "Table name (supports dot notation: schema.table)")
    .action(async (table: string | undefined, opts: IndexesOpts) => {
      const connectionAlias = resolveConnectionAlias(opts, schema);

      try {
        const driver = await resolveDriver({ connection: connectionAlias });
        try {
          const indexes = await driver.getIndexes(table);
          printJson({ indexes });
        } finally {
          await driver.close();
        }
      } catch (err) {
        handleActionError(err, connectionAlias);
      }
    });
}
