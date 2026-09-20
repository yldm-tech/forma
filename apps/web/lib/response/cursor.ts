import "server-only";
import { z } from "zod";
import { Prisma } from "@forma/database/prisma";

/**
 * Keyset cursor for the response batch loops (export, summary).
 *
 * It carries both sort keys because the batches are ordered by `createdAt desc, id desc` and an id alone cannot express a position in that order: `createdAt` is caller-supplied on the management API, so a historical import has an old `createdAt` and a freshly minted (lexically large) cuid. A cursor predicate written as `id < cursor` drops exactly those rows — silently, since an empty batch reads as "no more pages".
 */
export const ZResponseCursor = z.object({
  createdAt: z.date(),
  id: z.cuid2(),
});

export type TResponseCursor = z.infer<typeof ZResponseCursor>;

/**
 * The keyset predicate for "strictly after the cursor" under `orderBy: [{ createdAt: desc }, { id: desc }]`: an older `createdAt`, or the same `createdAt` with a smaller id.
 */
export const buildResponseCursorWhereClause = (cursor: TResponseCursor): Prisma.ResponseWhereInput => ({
  OR: [
    { createdAt: { lt: cursor.createdAt } },
    { createdAt: { equals: cursor.createdAt }, id: { lt: cursor.id } },
  ],
});

/**
 * Appends the cursor predicate to an existing where clause. It goes into `AND` rather than onto a top-level key because `buildWhereClause` already owns `AND`, and its own filters must keep applying alongside the cursor.
 */
export const applyResponseCursor = (
  whereClause: Prisma.ResponseWhereInput,
  cursor?: TResponseCursor
): Prisma.ResponseWhereInput => {
  if (!cursor) return whereClause;

  const existing = whereClause.AND;
  const andClauses = Array.isArray(existing) ? existing : existing ? [existing] : [];

  return {
    ...whereClause,
    AND: [...andClauses, buildResponseCursorWhereClause(cursor)],
  };
};
