export type OutputFormat = "json" | "yaml" | "csv";

let resolvedFormat: OutputFormat = "json";

export const configureFormat = (format: string): void => {
  resolvedFormat = format as OutputFormat;
};

export const getFormat = (): OutputFormat => resolvedFormat;
