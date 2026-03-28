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
