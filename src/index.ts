import { Command } from "commander";
import { getVersion } from "./lib/version.ts";
import { configureTruncation } from "./lib/truncation.ts";
import { configureTimeout } from "./lib/timeout.ts";
import { configureFormat } from "./lib/format.ts";
import { getSettings, getSetting } from "./lib/config.ts";
import { registerConfigCommand } from "./cli/config/index.ts";
import { registerConnectionCommand } from "./cli/connection/index.ts";
import { registerCredentialCommand } from "./cli/credential/index.ts";
import { registerQueryCommand } from "./cli/query/index.ts";
import { registerRunAlias } from "./cli/query/run-alias.ts";
import { registerSchemaCommand } from "./cli/schema/index.ts";
import { registerUsageCommand } from "./cli/usage/index.ts";

const program = new Command();
program
  .name("agent-sql")
  .description("Read-only-by-default SQL CLI for AI agents")
  .version(getVersion());

program.option("-c, --connection <alias>", "Connection alias to use");
program.option("--expand <fields>", "Expand truncated fields (comma-separated field names)");
program.option("--full", "Show full content for all truncated fields");
program.option("--timeout <ms>", "Query timeout in milliseconds (overrides config)");
program.option("--format <format>", "Output format: json, yaml, or csv (overrides config)");

program.hook("preAction", (thisCommand) => {
  const opts = thisCommand.opts();
  const settings = getSettings();
  configureTruncation({
    expand: opts.expand,
    full: opts.full,
    maxLength: settings.truncation?.maxLength,
  });
  if (opts.timeout) {
    const ms = parseInt(opts.timeout, 10);
    if (!Number.isFinite(ms) || ms < 1) {
      throw new Error(
        `Invalid --timeout: "${opts.timeout}". Must be a positive integer (milliseconds).`,
      );
    }
    configureTimeout(ms);
  }
  const allowedFormats = ["json", "yaml", "csv"];
  const format = opts.format ?? (getSetting("defaults.format") as string | undefined) ?? "json";
  if (!allowedFormats.includes(format)) {
    throw new Error(
      `Invalid --format: "${format}". Must be one of: ${allowedFormats.join(", ")}`,
    );
  }
  configureFormat(format);
});

registerConfigCommand({ program });
registerConnectionCommand({ program });
registerCredentialCommand({ program });
registerQueryCommand({ program });
registerRunAlias({ program });
registerSchemaCommand({ program });
registerUsageCommand({ program });

program.parse(process.argv);
if (!process.argv.slice(2).length) {
  program.outputHelp();
}
