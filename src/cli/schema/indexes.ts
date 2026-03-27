import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson, printError } from "../../lib/output.ts";
import { enhanceError } from "../../lib/errors.ts";

type IndexesOpts = {
  connection?: string;
};

export function registerIndexes(schema: Command): void {
  schema
    .command("indexes")
    .description("Show indexes for a table or all tables")
    .argument("[table]", "Table name (supports dot notation: schema.table)")
    .action(async (table: string | undefined, opts: IndexesOpts) => {
      const connection = schema.parent?.getOptionValue("connection") as string | undefined;

      try {
        const driver = await resolveDriver({ connection: opts.connection ?? connection });
        try {
          const indexes = await driver.getIndexes(table);
          printJson({ indexes });
        } finally {
          await driver.close();
        }
      } catch (err) {
        const enhanced = enhanceError(err instanceof Error ? err : new Error(String(err)));
        printError({
          message: enhanced.message,
          hint: enhanced.hint,
          fixableBy: enhanced.fixableBy,
        });
      }
    });
}
