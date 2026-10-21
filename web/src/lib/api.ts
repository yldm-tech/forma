// Problem details returned by the Go API (RFC 9457 on /api/v3, `{code, message}` elsewhere).
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string
  ) {
    super(message);
    this.name = "ApiError";
  }
}

const readError = async (response: Response): Promise<ApiError> => {
  const fallback = new ApiError(
    response.status,
    "http_error",
    `Request failed with status ${response.status}`
  );
  if (!response.headers.get("content-type")?.includes("json")) return fallback;
  const body = (await response.json().catch(() => null)) as {
    code?: unknown;
    message?: unknown;
    detail?: unknown;
  } | null;
  if (!body) return fallback;
  const code = typeof body.code === "string" ? body.code : fallback.code;
  const message =
    typeof body.detail === "string"
      ? body.detail
      : typeof body.message === "string"
        ? body.message
        : fallback.message;
  return new ApiError(response.status, code, message);
};

// Same-origin JSON GET. Session cookies ride along automatically; the API never sees a cross-origin request from the SPA.
export const getJSON = async <T>(path: string, signal?: AbortSignal): Promise<T> => {
  const response = await fetch(path, { headers: { Accept: "application/json" }, signal });
  if (!response.ok) throw await readError(response);
  return (await response.json()) as T;
};
