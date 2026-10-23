/** What a screen built from several queries shows: a failure as soon as any of them failed, else a placeholder until all have answered. */
export type CombinedQueryState =
  | { status: "pending" }
  | { status: "error"; error: unknown }
  | { status: "success" };

export const combinedQueryState = (
  queries: readonly { isPending: boolean; isError: boolean; error: unknown }[]
): CombinedQueryState => {
  const failed = queries.find((query) => query.isError);
  if (failed) return { status: "error", error: failed.error };
  if (queries.some((query) => query.isPending)) return { status: "pending" };
  return { status: "success" };
};
