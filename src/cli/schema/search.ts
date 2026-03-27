import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson, printError } from "../../lib/output.ts";
import { enhanceError } from "../../lib/errors.ts";

type SearchOpts = {
  connection?: string;
};

export function registerSearch(schema: Command): void {
  schema
    .command("search")
    .description("Search table and column names by pattern")
    .argument("<pattern>", "Search pattern (e.g. 'user', 'email')")
    .action(async (pattern: string, opts: SearchOpts) => {
      const connectionAlias =
        opts.connection ?? (schema.parent?.getOptionValue("connection") as string | undefined);

      try {
        const driver = await resolveDriver({ connection: connectionAlias });
        try {
          const results = await driver.searchSchema(pattern);
          printJson(results);
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
