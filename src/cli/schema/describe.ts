import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson, printError } from "../../lib/output.ts";
import { enhanceError } from "../../lib/errors.ts";

type DescribeOpts = {
  connection?: string;
  detailed?: boolean;
};

export function registerDescribe(schema: Command): void {
  schema
    .command("describe")
    .description("Describe a table's columns, types, and constraints")
    .argument("<table>", "Table name (supports dot notation: schema.table)")
    .option("--detailed", "Include constraints, indexes, and comments")
    .action(async (table: string, opts: DescribeOpts) => {
      const connectionAlias = opts.connection ?? (schema.parent?.getOptionValue("connection") as string | undefined);

      try {
        const driver = await resolveDriver({ connection: connectionAlias });
        try {
          const columns = await driver.describeTable(table);
          const result: Record<string, unknown> = { table, columns };

          if (opts.detailed) {
            const [constraints, indexes] = await Promise.all([
              driver.getConstraints(table),
              driver.getIndexes(table),
            ]);
            result.constraints = constraints;
            result.indexes = indexes;
          }

          printJson(result);
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
