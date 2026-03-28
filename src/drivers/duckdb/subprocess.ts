import { getTimeout } from "../../lib/timeout";
import { withCatchSync } from "../../lib/with-catch";

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

const spawnDuckDb = (bin: string, fullArgs: string[]) => {
  const [err, proc] = withCatchSync(() =>
    Bun.spawn([bin, ...fullArgs], {
      stdout: "pipe",
      stderr: "pipe",
      env: process.env,
    }),
  );
  if (err) {
    throw Object.assign(
      new Error(`DuckDB CLI not found (${bin}). Install with: brew install duckdb`),
      {
        hint: "DuckDB requires the duckdb CLI on PATH. Set AGENT_SQL_DUCKDB_PATH to use a custom location.",
        fixableBy: "human" as const,
      },
    );
  }
  return proc;
};

export const execDuckDb = async (args: string[], sql: string): Promise<ExecResult> => {
  const timeoutMs = getTimeout();
  const proc = spawnDuckDb(findDuckDb(), [...args, "-c", sql]);
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

export const execDuckDbJson = async (opts: JsonQueryOpts): Promise<Record<string, unknown>[]> => {
  const args = ["-cmd", ".mode jsonlines"];
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

  // Surface DuckDB warnings (deprecations, extension loading) to stderr
  if (result.stderr.trim()) {
    process.stderr.write(result.stderr);
  }

  return parseNdjson(result.stdout);
};

const parseNdjson = (stdout: string): Record<string, unknown>[] => {
  const results: Record<string, unknown>[] = [];
  for (const line of stdout.split("\n")) {
    const trimmed = line.trim();
    // Skip empty lines and DuckDB's "{" quirk for empty result sets
    if (!trimmed || trimmed === "{") {
      continue;
    }
    try {
      results.push(JSON.parse(trimmed) as Record<string, unknown>);
    } catch {
      throw Object.assign(new Error(`Failed to parse DuckDB output: ${trimmed.slice(0, 200)}`), {
        fixableBy: "agent" as const,
      });
    }
  }
  return results;
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

  if (firstLine.includes("read-only mode") || firstLine.includes("Permission Error")) {
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
