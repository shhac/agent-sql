import type { Command } from "commander";
import { registerAdd } from "./add.ts";
import { registerRemove } from "./remove.ts";
import { registerList } from "./list.ts";
import { registerUsage } from "./usage.ts";

export function registerCredentialCommand({ program }: { program: Command }): void {
  const credential = program.command("credential").description("Manage stored credentials");
  registerAdd(credential);
  registerRemove(credential);
  registerList(credential);
  registerUsage(credential);
}
