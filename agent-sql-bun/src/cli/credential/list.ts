import type { Command } from "commander";
import { listCredentials } from "../../lib/credentials.ts";
import { printJson } from "../../lib/output.ts";

export function registerList(credential: Command): void {
  credential
    .command("list")
    .description("List stored credentials (passwords masked, writePermission shown)")
    .action(() => {
      const entries = listCredentials();

      const items = entries.map((entry) => ({
        name: entry.name,
        username: entry.username ?? null,
        password: entry.password ?? null,
        writePermission: entry.writePermission,
      }));

      printJson({ credentials: items });
    });
}
