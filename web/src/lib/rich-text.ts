/** One run of a translated message: plain text, or text the message marks bold. */
export interface TextRun {
  text: string;
  bold: boolean;
}

/**
 * Splits a translated message on the `<b>…</b>` markup forma2's catalogues carry (forma2 injected it as HTML). Only that markup is recognised, so a translation can never inject anything else into the page; unbalanced markup is shown as text.
 */
export const boldRuns = (message: string): TextRun[] => {
  const runs: TextRun[] = [];
  const pattern = /<b>([^<]*)<\/b>/g;
  let last = 0;
  for (const match of message.matchAll(pattern)) {
    if (match.index > last) runs.push({ text: message.slice(last, match.index), bold: false });
    runs.push({ text: match[1] ?? "", bold: true });
    last = match.index + match[0].length;
  }
  if (last < message.length) runs.push({ text: message.slice(last), bold: false });
  return runs;
};
