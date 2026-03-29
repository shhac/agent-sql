import type { Command } from "commander";
import { getConnections, getDefaultConnectionAlias } from "../../lib/config.ts";
import { printJson } from "../../lib/output.ts";

export function registerList(connection: Command): void {
  connection
    .command("list")
    .description("List saved connections")
    .action(() => {
      const connections = getConnections();
      const defaultAlias = getDefaultConnectionAlias();

      const items = Object.entries(connections).map(([alias, conn]) => ({
        alias,
        driver: conn.driver,
        host: conn.host,
        port: conn.port,
        database: conn.database,
        path: conn.path,
        url: conn.url,
        credential: conn.credential,
        default: alias === defaultAlias,
      }));

      printJson({ connections: items }, { prune: true });
    });
}
