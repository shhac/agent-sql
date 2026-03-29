import type { Command } from "commander";
import { printJson } from "../../lib/output.ts";
import { resolveConnectionAlias, withDriverAction } from "../action-helpers.ts";

type SearchOpts = {
  connection?: string;
};

export function registerSearch(schema: Command): void {
  schema
    .command("search")
    .description("Search table and column names by pattern")
    .argument("<pattern>", "Search pattern (e.g. 'user', 'email')")
    .action(async (pattern: string, opts: SearchOpts) => {
      const connectionAlias = resolveConnectionAlias(opts, schema);

      await withDriverAction({ connection: connectionAlias }, async (driver) => {
        const results = await driver.searchSchema(pattern);
        printJson(results);
      });
    });
}
