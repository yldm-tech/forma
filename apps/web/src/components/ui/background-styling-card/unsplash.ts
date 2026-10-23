import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../../../lib/api";

/** One Unsplash search result (apps/server/internal/unsplash). */
export interface UnsplashImage {
  id: string;
  altDescription: string;
  /** The photo with its attribution parameters, the value a survey stores as its background. */
  url: string;
  /** What /api/app/unsplash/download is sent when the photo is chosen. */
  downloadLocation: string;
  authorName: string;
}

export const unsplashKeys = {
  status: () => ["unsplash", "status"] as const,
  search: (query: string) => ["unsplash", "search", query] as const,
};

/** GET /api/app/unsplash/status: whether the instance has UNSPLASH_ACCESS_KEY, forma2's isUnsplashConfigured. It does not change while the server runs. */
export const unsplashStatusQuery = () =>
  queryOptions({
    queryKey: unsplashKeys.status(),
    queryFn: ({ signal }) => api.get<{ configured: boolean }>("/api/app/unsplash/status", { signal }),
    staleTime: Infinity,
  });

/** GET /api/app/unsplash/search, nine photos a page; an empty page ends the list. */
export const unsplashSearchQuery = (query: string) =>
  infiniteQueryOptions({
    queryKey: unsplashKeys.search(query),
    queryFn: ({ pageParam, signal }) =>
      api.get<{ images: UnsplashImage[] }>(
        `/api/app/unsplash/search?${new URLSearchParams({ query, page: String(pageParam) }).toString()}`,
        { signal }
      ),
    initialPageParam: 1,
    getNextPageParam: (lastPage, pages) => (lastPage.images.length > 0 ? pages.length + 1 : undefined),
    enabled: query.trim() !== "",
    // A search spends the instance's Unsplash quota; a page once fetched is kept rather than refetched.
    staleTime: Infinity,
  });

/** POST /api/app/unsplash/download: tells Unsplash a photo was chosen, as its API guidelines require. */
export const triggerUnsplashDownload = (downloadUrl: string) =>
  api.post<void>("/api/app/unsplash/download", { downloadUrl });
