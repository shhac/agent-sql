import type { DriverConnection } from "../drivers/types.ts";

let activeDriver: DriverConnection | null = null;

export const setActiveDriver = (driver: DriverConnection): void => {
  activeDriver = driver;
};

export const clearActiveDriver = (): void => {
  activeDriver = null;
};

export const installSignalHandler = (): void => {
  process.on("SIGINT", async () => {
    if (activeDriver) {
      await activeDriver.close().catch(() => {});
    }
    process.exit(130);
  });
};
