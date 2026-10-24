/** Lexical update tag for the updates that load stored text into an editor. The text-saving listener ignores them, so opening an editor never reports a change. */
export const EDITOR_SEED_TAG = "forma-seed";

/** Whether an update (by its tags) only loaded stored text, rather than recording an edit. */
export const isSeedUpdate = (tags: ReadonlySet<string>): boolean => tags.has(EDITOR_SEED_TAG);
