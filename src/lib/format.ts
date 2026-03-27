export type OutputFormat = "jsonl" | "json" | "yaml" | "csv";

let resolvedFormat: OutputFormat = "jsonl";

export const configureFormat = (format: string): void => {
  resolvedFormat = format as OutputFormat;
};

export const getFormat = (): OutputFormat => resolvedFormat;
