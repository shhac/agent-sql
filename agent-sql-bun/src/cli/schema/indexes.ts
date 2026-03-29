import type { Command } from "commander";
import { printJson } from "../../lib/output.ts";
import { resolveConnectionAlias, withDriverAction } from "../action-helpers.ts";

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

      await withDriverAction({ connection: connectionAlias }, async (driver) => {
        const indexes = await driver.getIndexes(table);
        printJson({ indexes });
      });
    });
}
