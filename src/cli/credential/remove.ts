import type { Command } from "commander";
import { removeCredential } from "../../lib/credentials.ts";
import { getConnections, updateConnection } from "../../lib/config.ts";
import { printJson, printError } from "../../lib/output.ts";

const getConnectionsUsingCredential = (credentialName: string): string[] =>
  Object.entries(getConnections())
    .filter(([, conn]) => conn.credential === credentialName)
    .map(([alias]) => alias);

export function registerRemove(credential: Command): void {
  credential
    .command("remove")
    .description("Remove a stored credential")
    .argument("<name>", "Credential name to remove")
    .option("--force", "Remove even if referenced by connections (clears their credential refs)")
    .action((name: string, opts: { force?: boolean }) => {
      try {
        const usedBy = getConnectionsUsingCredential(name);

        if (usedBy.length > 0 && !opts.force) {
          printError({
            message: `Credential "${name}" is referenced by connections: ${usedBy.join(", ")}`,
            hint: "Use --force to remove anyway and clear credential refs from those connections",
            fixableBy: "human",
          });
          return;
        }

        if (usedBy.length > 0 && opts.force) {
          for (const connAlias of usedBy) {
            updateConnection(connAlias, { credential: undefined });
          }
        }

        const removed = removeCredential(name);
        if (!removed) {
          printError({ message: `Credential "${name}" not found` });
          return;
        }

        printJson({
          ok: true,
          removed: name,
          clearedFrom: usedBy.length > 0 ? usedBy : undefined,
        });
      } catch (err) {
        printError({ message: err instanceof Error ? err.message : "Failed to remove credential" });
      }
    });
}
