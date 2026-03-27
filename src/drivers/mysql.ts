// MySQL safety strategy: per-query START TRANSACTION READ ONLY wrapping
// + protocol-level single-statement enforcement (COM_QUERY). No parser needed.

import type { DriverConnection } from "./types.ts";

export function connectMysql(): DriverConnection {
  throw new Error("MySQL support is not yet implemented. Currently supported drivers: pg, sqlite.");
}
