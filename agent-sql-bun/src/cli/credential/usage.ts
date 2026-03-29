import type { Command } from "commander";

const USAGE_TEXT = `credential — Manage stored credentials for SQL database authentication

COMMANDS:
  credential add <name> [--username <user>] [--password <pass>] [--write]
    Store a named credential. Overwrites if name already exists.
    SQLite credentials may omit username/password (only writePermission matters).
    Snowflake uses a PAT (Personal Access Token) as the password.
    --write grants permission for INSERT/UPDATE/DELETE/DDL operations.

  credential remove <name>
    Remove a stored credential. Fails if any connection references it.
    --force removes anyway and clears credential refs from those connections.

  credential list
    List all stored credentials (passwords always masked).
    Shows writePermission for each credential.

WORKFLOW:
  1. Store credential:   agent-sql credential add acme --username deploy --password secret --write
  2. Add connections:    agent-sql connection add prod --driver pg --credential acme
                         agent-sql connection add staging --driver pg --credential acme
  3. Rotate password:    agent-sql credential add acme --username deploy --password new-secret --write
     All connections referencing "acme" pick up the new password automatically.

SQLITE NOTE:
  SQLite credentials typically only need --write to enable write mode.
  Username/password are optional since SQLite uses file-system permissions.
    agent-sql credential add local-write --write
    agent-sql connection add mydb --driver sqlite --path ./data.db --credential local-write

SNOWFLAKE NOTE:
  Snowflake authenticates via PAT (Personal Access Token) stored as the password.
  No --username needed — Snowflake identifies the user from the token.
    agent-sql credential add sf-cred --password <PAT>
    agent-sql connection add sf-prod --driver snowflake --account myorg-myaccount --database MYDB --credential sf-cred

KEYCHAIN (macOS):
  On macOS, credentials are stored in the system keychain automatically.
  Non-macOS falls back to plaintext config. \`credential list\` output is identical
  regardless of storage backend.

CONFIG: ~/.config/agent-sql/credentials.json (respects XDG_CONFIG_HOME)
`;

export function registerUsage(credential: Command): void {
  credential
    .command("usage")
    .description("Print credential command documentation (LLM-optimized)")
    .action(() => {
      process.stdout.write(USAGE_TEXT);
    });
}
