export type OutputFormat = "jsonl" | "json" | "yaml" | "csv";

const state = { format: "jsonl" as OutputFormat };

export const configureFormat = (format: string): void => {
  state.format = format as OutputFormat;
};

export const getFormat = (): OutputFormat => state.format;

export const resetFormat = (): void => {
  state.format = "jsonl";
};
