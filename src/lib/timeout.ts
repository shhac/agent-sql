import { getSetting } from "./config.ts";

const DEFAULT_TIMEOUT = 30000;

const state: { override: number | undefined } = { override: undefined };

export const configureTimeout = (ms?: number): void => {
  state.override = ms;
};

export const getTimeout = (): number =>
  state.override ?? (getSetting("query.timeout") as number | undefined) ?? DEFAULT_TIMEOUT;

export const resetTimeout = (): void => {
  state.override = undefined;
};
