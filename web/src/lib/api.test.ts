import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { ApiError, getJSON } from "./api";

const respond = (status: number, body: unknown, contentType = "application/json") =>
  vi.fn().mockResolvedValue(
    new Response(typeof body === "string" ? body : JSON.stringify(body), {
      status,
      headers: { "content-type": contentType },
    })
  );

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getJSON", () => {
  test("returns the decoded body on success", async () => {
    vi.stubGlobal("fetch", respond(200, { status: "ok" }));
    await expect(getJSON("/health")).resolves.toEqual({ status: "ok" });
  });

  test("maps a {code, message} error body", async () => {
    vi.stubGlobal("fetch", respond(404, { code: "not_found", message: "Route not found" }));
    await expect(getJSON("/api/v3/nope")).rejects.toMatchObject({
      status: 404,
      code: "not_found",
      message: "Route not found",
    });
  });

  test("prefers an RFC 9457 detail over message", async () => {
    vi.stubGlobal("fetch", respond(422, { code: "invalid", message: "Invalid", detail: "name is required" }));
    await expect(getJSON("/api/v3/surveys")).rejects.toMatchObject({
      code: "invalid",
      message: "name is required",
    });
  });

  test("falls back when the error body is not JSON", async () => {
    vi.stubGlobal("fetch", respond(502, "<html>Bad Gateway</html>", "text/html"));
    const error = await getJSON("/health/ready").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status: 502, code: "http_error" });
  });
});
