import type { Command } from "commander";
import type { TableInfo } from "../../drivers/types.ts";
import { resolveDriver } from "../../drivers/resolve.ts";
import { printJson } from "../../lib/output.ts";
import { handleActionError, resolveConnectionAlias } from "../action-helpers.ts";

type DumpOpts = {
  connection?: string;
  tables?: string;
  includeSystem?: boolean;
};

function parseTableFilter(raw: string): Set<string> {
  return new Set(
    raw
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean),
  );
}

function matchesFilter(table: TableInfo, filter: Set<string>): boolean {
  const qualified = table.schema ? `${table.schema}.${table.name}` : table.name;
  return filter.has(qualified) || filter.has(table.name);
}

function qualifiedName(table: TableInfo): string {
  return table.schema ? `${table.schema}.${table.name}` : table.name;
}

export function registerDump(parent: Command): void {
  parent
    .command("dump")
    .description("Dump full schema (tables, columns, indexes, constraints)")
    .option(
      "--tables <tables>",
      "Comma-separated table filter (supports dot notation: schema.table)",
    )
    .option("--include-system", "Include system tables")
    .action(async (opts: DumpOpts) => {
      const connectionAlias = resolveConnectionAlias(opts, parent);

      try {
        const driver = await resolveDriver({ connection: connectionAlias });
        try {
          const allTables = await driver.getTables({ includeSystem: opts.includeSystem });
          const filter = opts.tables ? parseTableFilter(opts.tables) : undefined;
          const filtered = filter ? allTables.filter((t) => matchesFilter(t, filter)) : allTables;

          const tables = await Promise.all(
            filtered.map(async (t) => {
              const name = qualifiedName(t);
              const [columns, indexes, constraints] = await Promise.all([
                driver.describeTable(name),
                driver.getIndexes(name),
                driver.getConstraints(name),
              ]);
              return {
                name: t.name,
                schema: t.schema,
                columns,
                indexes,
                constraints,
              };
            }),
          );

          printJson({ tables });
        } finally {
          await driver.close();
        }
      } catch (err) {
        handleActionError(err, connectionAlias);
      }
    });
}
