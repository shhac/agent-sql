import type { Command } from "commander";
import { executeRun, type RunOptions } from "./run-action.ts";

export function registerRun(parent: Command): void {
  parent
    .command("run")
    .description("Execute a SQL query")
    .argument("<sql>", "SQL query to execute")
    .option("-c, --connection <alias>", "Connection to use")
    .option("--limit <n>", "Max rows to return")
    .option("--write", "Enable write mode")
    .option("--compact", "Use compact array-of-arrays output format")
    .option("--expand <fields>", "Comma-separated fields to show untruncated")
    .option("--full", "Show all fields untruncated")
    .option("--timeout <ms>", "Query timeout override")
    .action((sql: string, opts: RunOptions) => executeRun(sql, opts));
}
