import type { Command } from "commander";
import { removeCredential, removeAllCredentials } from "../../lib/credentials.ts";
import { getConnections, updateConnection } from "../../lib/config.ts";
import { printJson, printError } from "../../lib/output.ts";

const getConnectionsUsingCredential = (credentialName: string): string[] =>
  Object.entries(getConnections())
    .filter(([, conn]) => conn.credential === credentialName)
    .map(([alias]) => alias);

const clearAllCredentialRefs = (): string[] => {
  const cleared: string[] = [];
  for (const [alias, conn] of Object.entries(getConnections())) {
    if (conn.credential) {
      updateConnection(alias, { credential: undefined });
      cleared.push(alias);
    }
  }
  return cleared;
};

export function registerRemove(credential: Command): void {
  credential
    .command("remove")
    .description("Remove a stored credential")
    .argument("[name]", "Credential name to remove")
    .option("--all", "Remove all stored credentials")
    .option("--force", "Remove even if referenced by connections (clears their credential refs)")
    .action((name: string | undefined, opts: { all?: boolean; force?: boolean }) => {
      try {
        if (opts.all) {
          const clearedFrom = opts.force ? clearAllCredentialRefs() : [];
          const removed = removeAllCredentials();

          if (removed.length === 0) {
            printJson({ ok: true, removed: [], message: "No credentials to remove" });
            return;
          }

          printJson({
            ok: true,
            removed,
            clearedFrom: clearedFrom.length > 0 ? clearedFrom : undefined,
          });
          return;
        }

        if (!name) {
          printError({
            message: "Credential name is required (or use --all to remove all credentials)",
            fixableBy: "agent",
          });
          return;
        }

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
