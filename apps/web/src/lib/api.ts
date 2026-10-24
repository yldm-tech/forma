// The SPA's client for the Go server. Every call is same-origin with the session cookie, and carries the header the server's CSRF check requires on mutations (apps/server/internal/platform/httpapi/csrf.go).
export const CSRF_HEADER = "X-Requested-With";

/** One failed constraint on one input field, as /api/app reports validation errors. */
export interface FieldError {
  field: string;
  rule: string;
  param?: string;
}

/** The code of an RFC 9457 problem from /api/v3 that names none itself. */
export const PROBLEM_CODE = "problem";

/** One offending input of an RFC 9457 problem from /api/v3; reason carries the service's error code where it has one. */
export interface InvalidParam {
  name: string;
  reason: string;
}

/** A non-2xx response, decoded from the `{code, message, details}` envelope (or RFC 9457 problem details on /api/v3). */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details?: Record<string, unknown>,
    /** Seconds until a rate-limited request may succeed. */
    readonly retryAfter?: number,
    /** The invalid_params of a v3 problem, empty for any other error. */
    readonly invalidParams: InvalidParam[] = []
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
    invalid_params?: unknown;
  } | null;
  if (!body) return fallback;
  // A v3 problem without its own code still carries a detail written for the user, unlike a body-less failure.
  const code =
    typeof body.code === "string"
      ? body.code
      : typeof body.detail === "string"
        ? PROBLEM_CODE
        : fallback.code;
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
  const invalidParams = Array.isArray(body.invalid_params) ? (body.invalid_params as InvalidParam[]) : [];
  return new ApiError(
    response.status,
    code,
    message,
    details,
    parseRetryAfter(response, details),
    invalidParams
  );
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
  let payload: string | FormData | undefined;
  // A form is sent as multipart, with the boundary the browser writes into its own Content-Type.
  if (body instanceof FormData) {
    payload = body;
  } else if (body !== undefined) {
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

/** The file name a Content-Disposition header gives, or null without one. Reads the RFC 5987 filename* form first, then the quoted or bare filename. */
export const contentDispositionFilename = (header: string | null): string | null => {
  if (!header) return null;
  for (const part of header.split(";")) {
    const [rawKey, ...rest] = part.split("=");
    const key = rawKey?.trim().toLowerCase();
    const value = rest.join("=").trim();
    if (key === "filename*") {
      const encoded = value.replace(/^UTF-8''/i, "");
      try {
        return decodeURIComponent(encoded);
      } catch {
        return encoded;
      }
    }
  }
  for (const part of header.split(";")) {
    const [rawKey, ...rest] = part.split("=");
    if (rawKey?.trim().toLowerCase() !== "filename") continue;
    const value = rest.join("=").trim();
    return value.startsWith('"') && value.endsWith('"') && value.length >= 2 ? value.slice(1, -1) : value;
  }
  return null;
};

/** A file the server answers a POST with, and the name its Content-Disposition gives it. */
export interface DownloadedFile {
  blob: Blob;
  filename: string | null;
}

/** POSTs a JSON body to an endpoint that answers a file rather than JSON; a failure still throws ApiError from the error envelope. */
export const downloadFile = async (
  path: string,
  body: unknown,
  options: RequestOptions = {}
): Promise<DownloadedFile> => {
  const response = await fetch(path, {
    method: "POST",
    headers: new Headers({ "Content-Type": "application/json", [CSRF_HEADER]: "XMLHttpRequest" }),
    body: JSON.stringify(body),
    credentials: "same-origin",
    signal: options.signal,
  });
  if (!response.ok) throw await readError(response);
  return {
    blob: await response.blob(),
    filename: contentDispositionFilename(response.headers.get("content-disposition")),
  };
};

/** POSTs a JSON body to an endpoint that answers a stream, and returns the open response for the caller to read; a refusal before the stream opened still throws ApiError from the error envelope. */
export const openStream = async (
  path: string,
  body: unknown,
  options: RequestOptions = {}
): Promise<Response> => {
  const response = await fetch(path, {
    method: "POST",
    headers: new Headers({ "Content-Type": "application/json", [CSRF_HEADER]: "XMLHttpRequest" }),
    body: JSON.stringify(body),
    credentials: "same-origin",
    cache: "no-store",
    signal: options.signal,
  });
  if (!response.ok) throw await readError(response);
  return response;
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
