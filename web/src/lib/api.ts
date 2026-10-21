// The SPA's client for the Go server. Every call is same-origin with the session cookie, and carries the header the server's CSRF check requires on mutations (server/internal/platform/httpapi/csrf.go).
export const CSRF_HEADER = "X-Requested-With";

/** One failed constraint on one input field, as /api/app reports validation errors. */
export interface FieldError {
  field: string;
  rule: string;
  param?: string;
}

/** A non-2xx response, decoded from the `{code, message, details}` envelope (or RFC 9457 problem details on /api/v3). */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details?: Record<string, unknown>,
    /** Seconds until a rate-limited request may succeed. */
    readonly retryAfter?: number
  ) {
    super(message);
    this.name = "ApiError";
  }

  get isRateLimited(): boolean {
    return this.status === 429;
  }

  /** The per-field failures of a validation error, empty for any other error. */
  get fieldErrors(): FieldError[] {
    const fields = this.details?.fields;
    return Array.isArray(fields) ? (fields as FieldError[]) : [];
  }
}

const parseRetryAfter = (response: Response, details?: Record<string, unknown>): number | undefined => {
  if (typeof details?.retryAfter === "number") return details.retryAfter;
  const header = Number.parseInt(response.headers.get("retry-after") ?? "", 10);
  return Number.isFinite(header) && header > 0 ? header : undefined;
};

const readError = async (response: Response): Promise<ApiError> => {
  const fallback = new ApiError(
    response.status,
    "http_error",
    `Request failed with status ${response.status}`,
    undefined,
    parseRetryAfter(response)
  );
  if (!response.headers.get("content-type")?.includes("json")) return fallback;
  const body = (await response.json().catch(() => null)) as {
    code?: unknown;
    message?: unknown;
    detail?: unknown;
    details?: unknown;
  } | null;
  if (!body) return fallback;
  const code = typeof body.code === "string" ? body.code : fallback.code;
  const message =
    typeof body.detail === "string"
      ? body.detail
      : typeof body.message === "string"
        ? body.message
        : fallback.message;
  const details =
    body.details && typeof body.details === "object" && !Array.isArray(body.details)
      ? (body.details as Record<string, unknown>)
      : undefined;
  return new ApiError(response.status, code, message, details, parseRetryAfter(response, details));
};

export interface RequestOptions {
  signal?: AbortSignal;
}

const request = async <T>(
  method: string,
  path: string,
  body: unknown,
  options: RequestOptions = {}
): Promise<T> => {
  const headers = new Headers({ Accept: "application/json", [CSRF_HEADER]: "XMLHttpRequest" });
  let payload: string | undefined;
  if (body !== undefined) {
    headers.set("Content-Type", "application/json");
    payload = JSON.stringify(body);
  }
  const response = await fetch(path, {
    method,
    headers,
    body: payload,
    credentials: "same-origin",
    signal: options.signal,
  });
  if (!response.ok) throw await readError(response);
  if (response.status === 204 || response.headers.get("content-length") === "0") return undefined as T;
  return (await response.json()) as T;
};

export const api = {
  get: <T>(path: string, options?: RequestOptions) => request<T>("GET", path, undefined, options),
  post: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>("POST", path, body, options),
  put: <T>(path: string, body?: unknown, options?: RequestOptions) => request<T>("PUT", path, body, options),
  patch: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>("PATCH", path, body, options),
  delete: <T>(path: string, options?: RequestOptions) => request<T>("DELETE", path, undefined, options),
};
