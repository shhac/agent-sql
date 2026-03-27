import type { Command } from "commander";
import type { ConstraintInfo } from "../../drivers/types.ts";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson, printError } from "../../lib/output.ts";
import { enhanceError } from "../../lib/errors.ts";

type ConstraintsOpts = {
  connection?: string;
  type?: string;
};

const VALID_TYPES = new Set(["pk", "fk", "unique", "check"]);

const TYPE_MAP: Record<string, ConstraintInfo["type"]> = {
  pk: "primary_key",
  fk: "foreign_key",
  unique: "unique",
  check: "check",
};

export function registerConstraints(schema: Command): void {
  schema
    .command("constraints")
    .description("Show constraints for a table or all tables")
    .argument("[table]", "Table name (supports dot notation: schema.table)")
    .option("--type <type>", "Filter by type: pk, fk, unique, check")
    .action(async (table: string | undefined, opts: ConstraintsOpts) => {
      if (opts.type && !VALID_TYPES.has(opts.type)) {
        printError({
          message: `Invalid constraint type: "${opts.type}". Valid types: pk, fk, unique, check`,
          fixableBy: "agent",
        });
        return;
      }

      const connectionAlias =
        opts.connection ?? (schema.parent?.getOptionValue("connection") as string | undefined);

      try {
        const driver = await resolveDriver({ connection: connectionAlias });
        try {
          const allConstraints = await driver.getConstraints(table);
          const constraints = opts.type
            ? allConstraints.filter((c) => c.type === TYPE_MAP[opts.type!])
            : allConstraints;
          printJson({ constraints });
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
