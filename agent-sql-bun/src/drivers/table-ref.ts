export const parseTableRef = (
  table: string,
  defaultSchema: string,
): { schema: string; table: string } => {
  const parts = table.split(".");
  if (parts.length >= 2) {
    return { schema: parts[0]!, table: parts[1]! };
  }
  return { schema: defaultSchema, table: parts[0]! };
};
