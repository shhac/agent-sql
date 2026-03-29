import type { Command } from "commander";
import { registerTables } from "./tables.ts";
import { registerDescribe } from "./describe.ts";
import { registerIndexes } from "./indexes.ts";
import { registerConstraints } from "./constraints.ts";
import { registerSearch } from "./search.ts";
import { registerDump } from "./dump.ts";
import { registerUsage } from "./usage.ts";

export function registerSchemaCommand({ program }: { program: Command }): void {
  const schema = program.command("schema").description("Explore database structure");
  registerTables(schema);
  registerDescribe(schema);
  registerIndexes(schema);
  registerConstraints(schema);
  registerSearch(schema);
  registerDump(schema);
  registerUsage(schema);
}
