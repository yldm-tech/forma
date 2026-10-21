import { ApiError } from "./api";

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

/**
 * Loads something the caller has just created, waiting for its permissions to arrive. The server's authorization is a projection that `forma worker` delivers every few seconds, so for a moment after a create the creator's own request is refused (403, or 404 where existence is hidden). forma2 had no such window; navigating straight to the new organization, workspace or team would show a not-found. Any other failure, or a refusal that outlasts the window, is the caller's error.
 */
export const untilAuthorized = async <T>(
  load: () => Promise<T>,
  { timeoutMs = 15_000, intervalMs = 500, wait = sleep, now = Date.now } = {}
): Promise<T> => {
  const deadline = now() + timeoutMs;
  for (;;) {
    try {
      return await load();
    } catch (error) {
      const refused = error instanceof ApiError && (error.status === 403 || error.status === 404);
      if (!refused || now() >= deadline) throw error;
      await wait(intervalMs);
    }
  }
};
