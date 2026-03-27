import type { Command } from "commander";
import { resolveDriver } from "../../drivers/resolve.ts";
import { enhanceError } from "../../lib/errors.ts";
import { printError, printJson } from "../../lib/output.ts";

type ExplainOptions = {
  connection?: string;
  analyze?: boolean;
};

const WRITE_PATTERN = /^\s*(INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|TRUNCATE)\b/i;

const validateAnalyzeSafety = (sql: string): string | undefined => {
  if (WRITE_PATTERN.test(sql)) {
    return `EXPLAIN ANALYZE is not allowed for write queries (detected ${sql.match(WRITE_PATTERN)?.[1]?.toUpperCase()}). EXPLAIN ANALYZE actually executes the query, which would modify data. Use EXPLAIN without --analyze for write queries.`;
  }
  return undefined;
};

export function registerExplain(parent: Command): void {
  parent
    .command("explain")
    .description("Show the execution plan for a SQL query")
    .argument("<sql>", "SQL query to explain")
    .option("--analyze", "Run EXPLAIN ANALYZE (executes the query; read-only queries only)")
    .action(async (sql: string, opts: ExplainOptions) => {
      const connectionAlias =
        opts.connection ?? (parent.parent?.getOptionValue("connection") as string | undefined);
      try {
        if (opts.analyze) {
          const safety = validateAnalyzeSafety(sql);
          if (safety) {
            printError({ message: safety, fixableBy: "agent" });
            return;
          }
        }

        const prefix = opts.analyze ? "EXPLAIN ANALYZE" : "EXPLAIN";
        const explainSql = `${prefix} ${sql}`;

        const driver = await resolveDriver({ connection: connectionAlias });

        try {
          const result = await driver.query(explainSql);
          printJson({
            plan: result.rows,
          });
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
