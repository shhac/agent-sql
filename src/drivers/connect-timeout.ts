const CONNECT_TIMEOUT_MS = 10_000;

export const withConnectTimeout = <T>(promise: Promise<T>): Promise<T> => {
  const timeout = new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error("Connection timed out")), CONNECT_TIMEOUT_MS),
  );
  return Promise.race([promise, timeout]);
};
