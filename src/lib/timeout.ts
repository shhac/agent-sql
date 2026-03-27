import { getSetting } from "./config.ts";

const DEFAULT_TIMEOUT = 30000;

let cliOverride: number | undefined;

export const configureTimeout = (ms?: number): void => {
  cliOverride = ms;
};

export const getTimeout = (): number =>
  cliOverride ?? (getSetting("query.timeout") as number | undefined) ?? DEFAULT_TIMEOUT;
