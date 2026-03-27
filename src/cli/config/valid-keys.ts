export type KeyDefinition = {
  key: string;
  description: string;
} & (
  | { type: "number"; defaultValue: number; min?: number; max?: number }
  | { type: "string"; defaultValue: string; allowedValues: string[] }
);

export const VALID_KEYS: KeyDefinition[] = [
  {
    key: "defaults.limit",
    type: "number",
    defaultValue: 20,
    description: "Default row limit for queries",
    min: 1,
    max: 1000,
  },
  {
    key: "defaults.format",
    type: "string",
    defaultValue: "json",
    description: "Default output format",
    allowedValues: ["json", "yaml", "csv"],
  },
  {
    key: "query.timeout",
    type: "number",
    defaultValue: 30000,
    description: "Query timeout in milliseconds",
    min: 1000,
    max: 300000,
  },
  {
    key: "query.maxRows",
    type: "number",
    defaultValue: 100,
    description: "Maximum rows per query",
    min: 1,
    max: 10000,
  },
  {
    key: "truncation.maxLength",
    type: "number",
    defaultValue: 200,
    description: "String truncation threshold",
    min: 50,
    max: 100000,
  },
];

export const parseConfigValue = (key: string, rawValue: string): number | string => {
  const def = VALID_KEYS.find((k) => k.key === key);
  if (!def) {
    const validKeys = VALID_KEYS.map((k) => k.key).join(", ");
    throw new Error(`Unknown config key: "${key}". Valid keys: ${validKeys}`);
  }

  if (def.type === "string") {
    if (!def.allowedValues.includes(rawValue)) {
      throw new Error(
        `"${key}" must be one of: ${def.allowedValues.join(", ")}. Got: "${rawValue}"`,
      );
    }
    return rawValue;
  }

  const num = Number(rawValue);
  if (!Number.isFinite(num) || !Number.isInteger(num)) {
    throw new Error(`"${key}" must be an integer. Got: "${rawValue}"`);
  }
  if (def.min !== undefined && num < def.min) {
    throw new Error(`"${key}" minimum is ${def.min}. Got: ${num}`);
  }
  if (def.max !== undefined && num > def.max) {
    throw new Error(`"${key}" maximum is ${def.max}. Got: ${num}`);
  }

  return num;
};
