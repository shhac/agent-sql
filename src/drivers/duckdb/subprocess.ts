import { getTimeout } from "../../lib/timeout";

type ExecResult = {
  stdout: string;
  stderr: string;
  exitCode: number;
};

const findDuckDb = (): string => {
  const custom = process.env.AGENT_SQL_DUCKDB_PATH;
  if (custom) {
    return custom;
  }
  return "duckdb";
};

export const execDuckDb = async (
  args: string[],
  sql: string,
): Promise<ExecResult> => {
  const timeoutMs = getTimeout();
  const bin = findDuckDb();

  const proc = Bun.spawn([bin, ...args, "-c", sql], {
    stdout: "pipe",
    stderr: "pipe",
    env: process.env,
  });

  const timer = setTimeout(() => proc.kill(), timeoutMs);

  try {
    const [stdout, stderr] = await Promise.all([
      new Response(proc.stdout).text(),
      new Response(proc.stderr).text(),
    ]);
    const exitCode = await proc.exited;
    return { stdout, stderr, exitCode };
  } finally {
    clearTimeout(timer);
  }
};

type JsonQueryOpts = {
  dbPath: string | undefined;
  sql: string;
  readonly: boolean;
};

export const execDuckDbJson = async (
  opts: JsonQueryOpts,
): Promise<Record<string, unknown>[]> => {
  const args = ["-json"];
  if (opts.dbPath && opts.readonly) {
    args.push("-readonly");
  }
  if (opts.dbPath) {
    args.push(opts.dbPath);
  }

  const result = await execDuckDb(args, opts.sql);

  if (result.exitCode !== 0) {
    const message = result.stderr.trim() || "DuckDB query failed";
    throw classifyError(message);
  }

  const trimmed = result.stdout.trim();
  // DuckDB outputs "[{]" for empty result sets — not valid JSON
  if (!trimmed || trimmed === "[]" || trimmed === "[{]") {
    return [];
  }
  return JSON.parse(trimmed) as Record<string, unknown>[];
};

const classifyError = (message: string): Error => {
  const firstLine = message.split("\n")[0] ?? message;

  if (firstLine.includes("Catalog Error")) {
    return Object.assign(new Error(firstLine), {
      hint: message.includes("Did you mean")
        ? message.split("\n").find((l) => l.includes("Did you mean"))
        : "Use 'schema tables' to see available tables.",
      fixableBy: "agent" as const,
    });
  }

  if (firstLine.includes("Parser Error")) {
    return Object.assign(new Error(firstLine), {
      fixableBy: "agent" as const,
    });
  }

  if (
    firstLine.includes("read-only mode") ||
    firstLine.includes("Permission Error")
  ) {
    return Object.assign(new Error(firstLine), {
      hint: "This connection is read-only. To enable writes, use a credential with writePermission and pass --write.",
      fixableBy: "human" as const,
    });
  }

  if (firstLine.includes("IO Error")) {
    return Object.assign(new Error(firstLine), {
      fixableBy: "agent" as const,
    });
  }

  return Object.assign(new Error(firstLine), {
    fixableBy: "agent" as const,
  });
};

export const checkDuckDbAvailable = (): void => {
  const bin = findDuckDb();
  try {
    const result = Bun.spawnSync([bin, "--version"], {
      stdout: "pipe",
      stderr: "pipe",
    });

    if (result.exitCode !== 0) {
      throw new Error("non-zero exit");
    }
  } catch {
    throw Object.assign(
      new Error(
        `DuckDB CLI not found (${bin}). Install with: brew install duckdb`,
      ),
      {
        hint: "DuckDB requires the duckdb CLI on PATH. Set AGENT_SQL_DUCKDB_PATH to use a custom location.",
        fixableBy: "human" as const,
      },
    );
  }
};
