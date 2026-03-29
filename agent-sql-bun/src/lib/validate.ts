const fail = (field: string, message: string): never => {
  throw new Error(`Invalid ${field}: ${message}`);
};

export const validateConfig = (
  raw: unknown,
): {
  connections: Record<string, unknown>;
  settings: Record<string, unknown>;
  default_connection?: string;
} => {
  if (!raw || typeof raw !== "object") {
    fail("config", "must be an object");
  }
  const obj = raw as Record<string, unknown>;
  return {
    default_connection:
      typeof obj.default_connection === "string" ? obj.default_connection : undefined,
    connections: (typeof obj.connections === "object" && obj.connections !== null
      ? obj.connections
      : {}) as Record<string, unknown>,
    settings: (typeof obj.settings === "object" && obj.settings !== null
      ? obj.settings
      : {}) as Record<string, unknown>,
  };
};

export const validateCredential = (
  raw: unknown,
): { username?: string; password?: string; writePermission?: boolean } => {
  if (!raw || typeof raw !== "object") {
    fail("credential", "must be an object");
  }
  const obj = raw as Record<string, unknown>;
  return {
    username: typeof obj.username === "string" ? obj.username : undefined,
    password: typeof obj.password === "string" ? obj.password : undefined,
    writePermission: typeof obj.writePermission === "boolean" ? obj.writePermission : undefined,
  };
};
