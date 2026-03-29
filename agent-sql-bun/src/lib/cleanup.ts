import type { DriverConnection } from "../drivers/types.ts";

const state: { driver: DriverConnection | null } = { driver: null };

export const setActiveDriver = (driver: DriverConnection): void => {
  state.driver = driver;
};

export const clearActiveDriver = (): void => {
  state.driver = null;
};

export const installSignalHandler = (): void => {
  process.on("SIGINT", async () => {
    if (state.driver) {
      await state.driver.close().catch(() => {});
    }
    process.exit(130);
  });
};
