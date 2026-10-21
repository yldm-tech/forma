/** The outcome of a single keyboard/pointer reorder in a ranking element. */
export interface RankingReorder {
  /** The ranked ids after the move. Reference-equal contents when the move was a no-op. */
  rankedIds: string[];
  /** Zero-based index the moved id ended up at, or -1 when it was not ranked at all. */
  index: number;
  /** Whether the order actually changed — false at the ends of the list. */
  changed: boolean;
}

/**
 * Move one ranked id up or down by a single position.
 *
 * The ends of the list are a no-op rather than a wrap or a clamp that reports success: the button at
 * the boundary is inert, and the caller uses `changed` to decide whether there is anything to
 * announce. Kept out of the component so the boundary cases are testable without a DOM.
 */
export const reorderRankedIds = (
  rankedIds: readonly string[],
  itemId: string,
  direction: "up" | "down"
): RankingReorder => {
  const index = rankedIds.indexOf(itemId);
  if (index === -1) return { rankedIds: [...rankedIds], index: -1, changed: false };

  const newIndex = direction === "up" ? index - 1 : index + 1;
  if (newIndex < 0 || newIndex > rankedIds.length - 1) {
    return { rankedIds: [...rankedIds], index, changed: false };
  }

  const next = [...rankedIds];
  const [movedItem] = next.splice(index, 1);
  next.splice(newIndex, 0, movedItem);
  return { rankedIds: next, index: newIndex, changed: true };
};
