import { describe, expect, test } from "vitest";
import { extractAuthToken } from "./utils";

describe("extractAuthToken", () => {
  test("extracts token from Authorization header with Bearer prefix", () => {
    const mockRequest = new Request("https://example.com", {
      headers: {
        authorization: "Bearer token123",
      },
    });

    const token = extractAuthToken(mockRequest);
    expect(token).toBe("token123");
  });

  test("extracts token from Authorization header with other prefix", () => {
    const mockRequest = new Request("https://example.com", {
      headers: {
        authorization: "Custom token123",
      },
    });

    const token = extractAuthToken(mockRequest);
    expect(token).toBe("token123");
  });

  test("extracts token from query parameter", () => {
    const mockRequest = new Request("https://example.com?access_token=token123");

    const token = extractAuthToken(mockRequest);
    expect(token).toBe("token123");
  });

  test("prioritizes Authorization header over query parameter", () => {
    const mockRequest = new Request("https://example.com?access_token=queryToken", {
      headers: {
        authorization: "Bearer headerToken",
      },
    });

    const token = extractAuthToken(mockRequest);
    expect(token).toBe("headerToken");
  });

  test("returns null when no token is found", () => {
    const mockRequest = new Request("https://example.com");

    expect(extractAuthToken(mockRequest)).toBeNull();
  });

  test("returns null when Authorization header is empty", () => {
    const mockRequest = new Request("https://example.com", {
      headers: {
        authorization: "",
      },
    });

    expect(extractAuthToken(mockRequest)).toBeNull();
  });

  test("returns null when query parameter is empty", () => {
    const mockRequest = new Request("https://example.com?access_token=");

    expect(extractAuthToken(mockRequest)).toBeNull();
  });

  test("returns null when Authorization header carries only a prefix", () => {
    const mockRequest = new Request("https://example.com", {
      headers: {
        authorization: "Bearer ",
      },
    });

    expect(extractAuthToken(mockRequest)).toBeNull();
  });

  test("never throws a Response, which Next.js would surface as a 500 instead of a 401", () => {
    const mockRequest = new Request("https://example.com");

    expect(() => extractAuthToken(mockRequest)).not.toThrow();
  });
});
