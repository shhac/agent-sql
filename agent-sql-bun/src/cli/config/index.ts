import type { Command } from "commander";
import { registerGet } from "./get.ts";
import { registerSet } from "./set.ts";
import { registerReset } from "./reset.ts";
import { registerListKeys } from "./list-keys.ts";
import { registerUsage } from "./usage.ts";

export function registerConfigCommand({ program }: { program: Command }): void {
  const config = program.command("config").description("Manage CLI settings");
  registerGet(config);
  registerSet(config);
  registerReset(config);
  registerListKeys(config);
  registerUsage(config);
}
