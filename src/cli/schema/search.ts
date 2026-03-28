import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson } from "../../lib/output.ts";
import { handleActionError, resolveConnectionAlias } from "../action-helpers.ts";

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
        const driver = await resolveDriver({ connection: connectionAlias });
        try {
          const results = await driver.searchSchema(pattern);
          printJson(results);
        } finally {
          await driver.close();
        }
      } catch (err) {
        handleActionError(err, connectionAlias);
      }
    });
}
