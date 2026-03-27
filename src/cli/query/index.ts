import type { Command } from "commander";
import { registerRun } from "./run.ts";
import { registerSample } from "./sample.ts";
import { registerExplain } from "./explain.ts";
import { registerCount } from "./count.ts";
import { registerUsage } from "./usage.ts";

export function registerQueryCommand({ program }: { program: Command }): void {
  const query = program.command("query").description("Execute SQL queries");
  registerRun(query);
  registerSample(query);
  registerExplain(query);
  registerCount(query);
  registerUsage(query);
}
