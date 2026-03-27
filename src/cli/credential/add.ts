import { platform } from "node:os";
import type { Command } from "commander";
import { storeCredential } from "../../lib/credentials.ts";
import { printJson, printError } from "../../lib/output.ts";

export function registerAdd(credential: Command): void {
  credential
    .command("add")
    .description("Add or update a stored credential")
    .argument("<name>", "Short name for this credential (e.g. acme, local-dev)")
    .option("--username <user>", "Database username")
    .option("--password <pass>", "Database password")
    .option("--write", "Allow write operations when using this credential")
    .action((name: string, opts: { username?: string; password?: string; write?: boolean }) => {
      try {
        const writePermission = opts.write === true;

        const { storage } = storeCredential(name, {
          username: opts.username,
          password: opts.password,
          writePermission,
        });

        if (storage === "file" && platform() !== "darwin") {
          process.stderr.write(
            "Warning: credentials stored in plaintext (macOS Keychain not available)\n",
          );
        }

        printJson({
          ok: true,
          credential: name,
          username: opts.username ?? null,
          writePermission,
          storage,
          hint: `Use with: agent-sql connection add <alias> --driver <pg|sqlite> --credential ${name}`,
        });
      } catch (err) {
        printError({ message: err instanceof Error ? err.message : "Failed to add credential" });
      }
    });
}
