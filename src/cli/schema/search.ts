import type { Command } from "commander";
import { printJson } from "../../lib/output.ts";
import { handleActionError, resolveConnectionAlias, withDriver } from "../action-helpers.ts";

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

      try {
        await withDriver({ connection: connectionAlias }, async (driver) => {
          const results = await driver.searchSchema(pattern);
          printJson(results);
        });
      } catch (err) {
        handleActionError(err, connectionAlias);
      }
    });
}
