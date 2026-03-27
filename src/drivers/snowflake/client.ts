// HTTP client for Snowflake SQL API v2
// Thin wrapper around fetch() with retry, async polling, and partition fetching

import type { SnowflakeStatementRequest, SnowflakeQueryResponse, SnowflakeResponse } from "./types";
import { isQueryResponse, isAsyncResponse } from "./types";
import type { AuthHeaders } from "./auth";

export type SnowflakeClientOpts = {
  baseUrl: string;
  authHeaders: AuthHeaders;
  timeoutMs?: number;
};

const MAX_RETRIES = 3;
const BASE_DELAY_MS = 1000;
const MAX_DELAY_MS = 8000;

// Async poll intervals (matching Go driver pattern)
const POLL_INTERVALS_MS = [500, 500, 1000, 1500, 2000, 4000, 5000];

const jitteredDelay = (attempt: number): number => {
  const delay = Math.min(MAX_DELAY_MS, BASE_DELAY_MS * 2 ** attempt);
  return delay * (0.5 + Math.random() * 0.5);
};

const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));

const isRetryable = (status: number): boolean => status === 429 || status === 408 || status >= 500;

export class SnowflakeClient {
  private baseUrl: string;
  private authHeaders: AuthHeaders;
  private timeoutMs: number;

  constructor(opts: SnowflakeClientOpts) {
    this.baseUrl = opts.baseUrl;
    this.authHeaders = opts.authHeaders;
    this.timeoutMs = opts.timeoutMs ?? 30_000;
  }

  private async fetchWithRetry(url: string, init: RequestInit): Promise<Response> {
    let lastError: Error | undefined;

    for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
      if (attempt > 0) {
        await sleep(jitteredDelay(attempt - 1));
      }

      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), this.timeoutMs);

      try {
        const resp = await fetch(url, {
          ...init,
          signal: controller.signal,
        });
        if (!isRetryable(resp.status) || attempt === MAX_RETRIES) {
          return resp;
        }
        lastError = new Error(`HTTP ${resp.status}`);
      } catch (err) {
        lastError = err instanceof Error ? err : new Error(String(err));
        if (attempt === MAX_RETRIES) {
          break;
        }
      } finally {
        clearTimeout(timer);
      }
    }

    throw lastError ?? new Error("Request failed after retries");
  }

  async executeStatement(req: SnowflakeStatementRequest): Promise<SnowflakeQueryResponse> {
    const url = `${this.baseUrl}/api/v2/statements`;
    const body: SnowflakeStatementRequest = {
      ...req,
      parameters: {
        ...req.parameters,
        MULTI_STATEMENT_COUNT: "1",
      },
    };

    const resp = await this.fetchWithRetry(url, {
      method: "POST",
      headers: {
        ...this.authHeaders,
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify(body),
    });

    const json = (await resp.json()) as SnowflakeResponse;

    if (isAsyncResponse(json)) {
      return this.pollForResult(json.statementHandle);
    }

    if (!resp.ok && !isQueryResponse(json)) {
      const errMsg = "message" in json ? json.message : `HTTP ${resp.status}`;
      const sqlState = "sqlState" in json ? (json as { sqlState?: string }).sqlState : undefined;
      throw Object.assign(new Error(errMsg), {
        code: "code" in json ? json.code : undefined,
        sqlState,
      });
    }

    if (!isQueryResponse(json)) {
      throw new Error(
        `Unexpected response format from Snowflake (code: ${"code" in json ? json.code : "unknown"})`,
      );
    }

    return json;
  }

  private async pollForResult(handle: string): Promise<SnowflakeQueryResponse> {
    const url = `${this.baseUrl}/api/v2/statements/${handle}`;

    for (let attempt = 0; ; attempt++) {
      const delayMs = POLL_INTERVALS_MS[Math.min(attempt, POLL_INTERVALS_MS.length - 1)] ?? 5000;
      await sleep(delayMs);

      const resp = await this.fetchWithRetry(url, {
        method: "GET",
        headers: { ...this.authHeaders, Accept: "application/json" },
      });

      const json = (await resp.json()) as SnowflakeResponse;

      if (isQueryResponse(json)) {
        return json;
      }

      if (!isAsyncResponse(json)) {
        const errMsg = "message" in json ? json.message : `Polling failed (HTTP ${resp.status})`;
        throw Object.assign(new Error(errMsg), {
          code: "code" in json ? json.code : undefined,
        });
      }
    }
  }

  async fetchPartition(handle: string, partition: number): Promise<(string | null)[][]> {
    const url = `${this.baseUrl}/api/v2/statements/${handle}?partition=${partition}`;

    const resp = await this.fetchWithRetry(url, {
      method: "GET",
      headers: { ...this.authHeaders, Accept: "application/json" },
    });

    if (!resp.ok) {
      throw new Error(`Failed to fetch partition ${partition}: HTTP ${resp.status}`);
    }

    const json = (await resp.json()) as { data?: (string | null)[][] };
    return json.data ?? [];
  }

  async cancelStatement(handle: string): Promise<void> {
    const url = `${this.baseUrl}/api/v2/statements/${handle}/cancel`;
    await this.fetchWithRetry(url, {
      method: "POST",
      headers: {
        ...this.authHeaders,
        "Content-Type": "application/json",
      },
    });
  }
}
