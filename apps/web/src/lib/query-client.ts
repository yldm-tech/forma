import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { ApiError } from "./api";
import { appHref } from "./base-path";
import { loginHref } from "./navigation";
import { authKeys } from "./queries/auth";

/**
 * A 4xx answer, rate limits included, is the server's decision and repeating the request cannot change it; only network failures and 5xx are retried, twice.
 */
export const shouldRetry = (failureCount: number, error: unknown): boolean => {
  if (error instanceof ApiError && error.status < 500) return false;
  return failureCount < 2;
};

/** The server's answer to a request whose session cookie names no live session. A 401 with any other code is a refused credential (a wrong password or backup code at sign-in), not a lost session. */
export const isSessionEnded = (error: unknown): boolean =>
  error instanceof ApiError && error.status === 401 && error.code === "not_authenticated";

/** Leaves for the sign-in page with the current page as where to return, as forma2's proxy redirected any request without a session. A full navigation, so nothing of the ended session survives in memory. */
const goToSignIn = () => {
  const { pathname, search, hash } = globalThis.location;
  globalThis.location.assign(appHref(loginHref(pathname + search + hash)));
};

/**
 * The SPA's query client. When any request of a signed-in browser finds its session gone (a timeout, a sign-out or password reset in another tab, a forced logout), the cached data of that session is dropped and the browser goes to sign-in, instead of every page showing "Not authenticated" until a reload. A browser that never signed in (no cached user) is left to the signed-out screens, which expect that answer.
 */
export const createQueryClient = (onSessionEnded: () => void = goToSignIn): QueryClient => {
  const handle = (error: unknown) => {
    if (!isSessionEnded(error) || client.getQueryData(authKeys.me) === undefined) return;
    client.clear();
    onSessionEnded();
  };
  const client: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError: handle }),
    mutationCache: new MutationCache({ onError: handle }),
    defaultOptions: {
      queries: { staleTime: 30_000, retry: shouldRetry },
      // A mutation is not known to be idempotent, so it is never repeated behind the user's back.
      mutations: { retry: false },
    },
  });
  return client;
};
