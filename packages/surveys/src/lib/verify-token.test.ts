import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { hashVerifyToken } from "./verify-token";

describe("hashVerifyToken", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  test("is the SHA-256 of the token as lowercase hex", async () => {
    await expect(hashVerifyToken("abc")).resolves.toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    );
  });

  test("is the same for the same link and differs between links", async () => {
    const a = await hashVerifyToken("token-of-a");
    expect(await hashVerifyToken("token-of-a")).toBe(a);
    expect(await hashVerifyToken("token-of-b")).not.toBe(a);
  });

  test("without SubtleCrypto, is a value no other page gets, not even one opened from the same link", async () => {
    const { getRandomValues } = globalThis.crypto;
    vi.stubGlobal("crypto", { getRandomValues: getRandomValues.bind(globalThis.crypto) });

    const first = await hashVerifyToken("token-of-a");
    const second = await hashVerifyToken("token-of-a");

    expect(first).toMatch(/^unhashed-[0-9a-f]{32}$/);
    expect(second).not.toBe(first);
  });
});
