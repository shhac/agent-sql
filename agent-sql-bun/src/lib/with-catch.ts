export const withCatch = async <T>(promise: Promise<T>): Promise<[Error] | [undefined, T]> => {
  try {
    const result = await promise;
    return [undefined, result];
  } catch (err) {
    return [err instanceof Error ? err : new Error(String(err))];
  }
};

export const withCatchSync = <T>(fn: () => T): [Error] | [undefined, T] => {
  try {
    const result = fn();
    return [undefined, result];
  } catch (err) {
    return [err instanceof Error ? err : new Error(String(err))];
  }
};

type RetryOpts = {
  maxRetries: number;
  shouldRetry: (err: Error) => boolean;
  delay: (attempt: number) => number;
};

export const withRetry = async <T>(fn: () => Promise<T>, opts: RetryOpts): Promise<T> => {
  for (const attempt of Array.from({ length: opts.maxRetries + 1 }, (_, i) => i)) {
    const [err, result] = await withCatch(Promise.resolve().then(fn));
    if (!err) {
      return result;
    }
    if (attempt === opts.maxRetries || !opts.shouldRetry(err)) {
      throw err;
    }
    await new Promise((r) => setTimeout(r, opts.delay(attempt)));
  }
  throw new Error("Retry exhausted");
};
