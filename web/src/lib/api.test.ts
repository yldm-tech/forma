import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { ApiError, CSRF_HEADER, api } from "./api";

const respond = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : typeof body === "string" ? body : JSON.stringify(body), {
      status,
      headers: { "content-type": "application/json", ...headers },
    })
  );

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("api", () => {
  test("returns the decoded body on success", async () => {
    vi.stubGlobal("fetch", respond(200, { status: "ok" }));
    await expect(api.get("/health")).resolves.toEqual({ status: "ok" });
  });

  test("sends same-origin credentials, the CSRF header and a JSON body", async () => {
    const fetchMock = respond(200, { id: "1" });
    vi.stubGlobal("fetch", fetchMock);
    await api.post("/api/app/things", { name: "x" });

    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = new Headers(init.headers);
    expect(path).toBe("/api/app/things");
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("same-origin");
    expect(headers.get(CSRF_HEADER)).toBe("XMLHttpRequest");
    expect(headers.get("content-type")).toBe("application/json");
    expect(init.body).toBe(JSON.stringify({ name: "x" }));
  });

  test("sends no body or content type without a payload", async () => {
    const fetchMock = respond(204, undefined);
    vi.stubGlobal("fetch", fetchMock);
    await expect(api.delete("/api/app/things/1")).resolves.toBeUndefined();
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(init.body).toBeUndefined();
    expect(new Headers(init.headers).has("content-type")).toBe(false);
  });

  test("maps the error envelope", async () => {
    vi.stubGlobal(
      "fetch",
      respond(400, {
        code: "validation_error",
        message: "Invalid input",
        details: { fields: [{ field: "email", rule: "email" }] },
      })
    );
    const error = (await api.post("/api/app/signup", {}).catch((e: unknown) => e)) as ApiError;
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status: 400, code: "validation_error", message: "Invalid input" });
    expect(error.fieldErrors).toEqual([{ field: "email", rule: "email" }]);
  });

  test("reads retryAfter from a rate-limit response", async () => {
    vi.stubGlobal(
      "fetch",
      respond(
        429,
        { code: "too_many_requests", message: "Maximum number of requests reached. Please try again later." },
        { "retry-after": "42" }
      )
    );
    const error = (await api.post("/api/app/x").catch((e: unknown) => e)) as ApiError;
    expect(error.isRateLimited).toBe(true);
    expect(error.retryAfter).toBe(42);
    expect(error.fieldErrors).toEqual([]);
  });

  test("prefers an RFC 9457 detail over message", async () => {
    vi.stubGlobal("fetch", respond(422, { code: "invalid", message: "Invalid", detail: "name is required" }));
    await expect(api.get("/api/v3/surveys")).rejects.toMatchObject({
      code: "invalid",
      message: "name is required",
    });
  });

  test("keeps the invalid_params of a v3 problem", async () => {
    vi.stubGlobal(
      "fetch",
      respond(
        422,
        {
          title: "Unprocessable Content",
          status: 422,
          detail: "Unable to update tag",
          invalid_params: [{ name: "name", reason: "tag_name_already_exists" }],
        },
        { "content-type": "application/problem+json" }
      )
    );
    const error = (await api.patch("/api/v3/tags/1", { name: "x" }).catch((e: unknown) => e)) as ApiError;
    expect(error.message).toBe("Unable to update tag");
    expect(error.code).toBe("problem");
    expect(error.invalidParams).toEqual([{ name: "name", reason: "tag_name_already_exists" }]);
  });

  test("sends a form as multipart without setting the content type itself", async () => {
    const fetchMock = respond(200, { contacts: [] });
    vi.stubGlobal("fetch", fetchMock);
    const form = new FormData();
    form.append("duplicateContactsAction", "skip");
    await api.post("/api/app/workspaces/w/contacts/import", form);
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(init.body).toBe(form);
    expect(new Headers(init.headers).has("content-type")).toBe(false);
  });

  test("falls back when the error body is not JSON", async () => {
    vi.stubGlobal("fetch", respond(502, "<html>Bad Gateway</html>", { "content-type": "text/html" }));
    const error = await api.get("/health/ready").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status: 502, code: "http_error" });
  });
});
