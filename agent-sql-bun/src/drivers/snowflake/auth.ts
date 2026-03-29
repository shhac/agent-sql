// Snowflake authentication: PAT (Personal Access Token) support
// Key-pair JWT is a stretch goal — designed to be added here without changing the rest of the driver

export type AuthHeaders = Record<string, string>;

export const buildPatHeaders = (token: string): AuthHeaders => ({
  Authorization: `Bearer ${token}`,
  "X-Snowflake-Authorization-Token-Type": "PROGRAMMATIC_ACCESS_TOKEN",
});

export const buildBaseUrl = (account: string): string =>
  `https://${account}.snowflakecomputing.com`;
