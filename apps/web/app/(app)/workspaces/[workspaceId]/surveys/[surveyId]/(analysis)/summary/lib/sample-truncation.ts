/**
 * Summary payloads cap every per-response sample list at a fixed size (`VALUES_LIMIT` in
 * `surveySummary.ts`) while still reporting the untruncated `responseCount`. The UI therefore has to
 * derive the truncation itself; nothing in the payload flags it.
 */
export interface TSampleTruncation {
  shown: number;
  total: number;
}

/**
 * Returns the shown/total pair when a sample list was cut short, or null when it is complete.
 *
 * `responseCount` is the untruncated number of answers for the element; `sampleCount` is the length
 * of the capped list that actually reached the client.
 */
export const getSampleTruncation = (sampleCount: number, responseCount: number): TSampleTruncation | null => {
  if (sampleCount <= 0) return null;
  if (sampleCount >= responseCount) return null;
  return { shown: sampleCount, total: responseCount };
};
