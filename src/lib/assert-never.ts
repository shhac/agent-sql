export const assertNever = (value: never, msg?: string): never => {
  throw new Error(msg ?? `Unhandled value: ${String(value)}`);
};
