// Snowflake SQL API v2 response types

export type SnowflakeColumnType = {
  name: string;
  type: string; // "fixed", "real", "text", "boolean", "date", "timestamp_ltz", "timestamp_ntz", "timestamp_tz", "time", "variant", "object", "array", "binary"
  nullable: boolean;
  scale?: number;
  precision?: number;
  length?: number;
  byteLength?: number;
};

export type SnowflakePartitionInfo = {
  rowCount: number;
  uncompressedSize: number;
};

export type SnowflakeResultMetadata = {
  numRows: number;
  format: string;
  partitionInfo: SnowflakePartitionInfo[];
  rowType: SnowflakeColumnType[];
};

// Successful query response
export type SnowflakeQueryResponse = {
  code: string;
  statementHandle: string;
  sqlState: string;
  message: string;
  resultSetMetaData: SnowflakeResultMetadata;
  data: (string | null)[][];
  statementStatusUrl: string;
  createdOn: number;
};

// Async in-progress response (HTTP 202)
export type SnowflakeAsyncResponse = {
  code: string; // "333334"
  message: string;
  statementHandle: string;
  statementStatusUrl: string;
};

// Error response
export type SnowflakeErrorResponse = {
  code: string;
  message: string;
  sqlState: string;
  statementHandle?: string;
};

// Union of possible responses
export type SnowflakeResponse =
  | SnowflakeQueryResponse
  | SnowflakeAsyncResponse
  | SnowflakeErrorResponse;

// Request body for POST /api/v2/statements
export type SnowflakeStatementRequest = {
  statement: string;
  timeout?: number;
  database?: string;
  schema?: string;
  warehouse?: string;
  role?: string;
  parameters?: Record<string, string>;
  bindings?: Record<string, { type: string; value: string }>;
};

// Connection options for the Snowflake driver
export type SnowflakeOpts = {
  account: string;
  database?: string;
  schema?: string;
  warehouse?: string;
  role?: string;
  token: string; // PAT secret
  readonly?: boolean;
};

// Type guard helpers
export const isQueryResponse = (r: SnowflakeResponse): r is SnowflakeQueryResponse =>
  "resultSetMetaData" in r && "data" in r;

export const isAsyncResponse = (r: SnowflakeResponse): r is SnowflakeAsyncResponse =>
  r.code === "333334";

export const isErrorResponse = (r: SnowflakeResponse): r is SnowflakeErrorResponse =>
  !isQueryResponse(r) && !isAsyncResponse(r) && "message" in r && r.code !== "090001";
