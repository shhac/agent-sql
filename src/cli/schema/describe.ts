import type { Command } from "commander";
import { printJson } from "../../lib/output.ts";
import { handleActionError, resolveConnectionAlias, withDriver } from "../action-helpers.ts";

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
      const connectionAlias = resolveConnectionAlias(opts, schema);

      try {
        await withDriver({ connection: connectionAlias }, async (driver) => {
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
        });
      } catch (err) {
        handleActionError(err, connectionAlias);
      }
    });
}
