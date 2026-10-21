import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "./api";

/**
 * A 4xx answer, rate limits included, is the server's decision and repeating the request cannot change it; only network failures and 5xx are retried, twice.
 */
export const shouldRetry = (failureCount: number, error: unknown): boolean => {
  if (error instanceof ApiError && error.status < 500) return false;
  return failureCount < 2;
};

export const createQueryClient = (): QueryClient =>
  new QueryClient({
    defaultOptions: {
      queries: { staleTime: 30_000, retry: shouldRetry },
      // A mutation is not known to be idempotent, so it is never repeated behind the user's back.
      mutations: { retry: false },
    },
  });
