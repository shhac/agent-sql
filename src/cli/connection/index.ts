import type { Command } from "commander";
import { registerAdd } from "./add.ts";
import { registerRemove } from "./remove.ts";
import { registerList } from "./list.ts";
import { registerTest } from "./test.ts";
import { registerUpdate } from "./update.ts";
import { registerSetDefault } from "./set-default.ts";
import { registerUsage } from "./usage.ts";

export function registerConnectionCommand({ program }: { program: Command }): void {
  const connection = program.command("connection").description("Manage SQL connections");
  registerAdd(connection);
  registerRemove(connection);
  registerUpdate(connection);
  registerList(connection);
  registerTest(connection);
  registerSetDefault(connection);
  registerUsage(connection);
}
