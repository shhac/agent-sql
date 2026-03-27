import type { Command } from "commander";

const USAGE_TEXT = `config — Manage CLI settings

COMMANDS:
  config get <key>              Get a config value
  config set <key> <value>      Set a config value
  config reset                  Reset all settings to defaults
  config list-keys              List all valid keys with defaults and ranges

KEYS:
  defaults.limit        (20)     Default row limit for queries [1-1000]
  query.timeout         (30000)  Query timeout in ms [1000-300000]
  query.maxRows         (100)    Maximum rows per query [1-10000]
  truncation.maxLength  (200)    String truncation threshold [50-100000]

EXAMPLES:
  agent-sql config set defaults.limit 50
  agent-sql config get query.timeout
  agent-sql config reset
`;

export function registerUsage(config: Command): void {
  config
    .command("usage")
    .description("Print config command documentation (LLM-optimized)")
    .action(() => {
      process.stdout.write(USAGE_TEXT);
    });
}
