import { openStream } from "../../../lib/api";
import type { SurveyGenerateBody, SurveyGenerationStreamEvent } from "./events";
import { NdjsonParser } from "./stream-parser";

const STREAM_ENDPOINT = "/api/app/surveys/generate/stream";

/**
 * forma2's streamSurveyGeneration: reads the generation stream and hands each event to onEvent as it arrives. A refusal before the stream opened throws the ApiError its envelope carries; failures after it opened arrive as error events, because the status code is spent by then.
 */
export async function streamSurveyGeneration(
  body: SurveyGenerateBody,
  { signal, onEvent }: { signal: AbortSignal; onEvent: (event: SurveyGenerationStreamEvent) => void }
): Promise<void> {
  const response = await openStream(STREAM_ENDPOINT, body, { signal });
  if (!response.body) {
    throw new Error("The survey generation stream returned no body.");
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  const parser = new NdjsonParser<SurveyGenerationStreamEvent>();
  let sawTerminalEvent = false;
  const handle = (event: SurveyGenerationStreamEvent) => {
    if (event.type === "done" || event.type === "error") sawTerminalEvent = true;
    onEvent(event);
  };
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      parser.push(decoder.decode(value, { stream: true })).forEach(handle);
    }
    parser.flush().forEach(handle);
  } finally {
    reader.releaseLock();
  }
  // A body that ends after only partials (a dropped connection, a proxy cutting the response) would otherwise leave the caller waiting in its generating state for an event that is never coming.
  if (!sawTerminalEvent) {
    throw new Error("The survey generation stream ended without a result.");
  }
}
