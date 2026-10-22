import { describe, expect, test } from "vite-plus/test";
import { checkForYoutubePrivacyMode } from "./utils";

describe("checkForYoutubePrivacyMode", () => {
  test("returns true for youtube-nocookie.com URLs", () => {
    expect(checkForYoutubePrivacyMode("https://www.youtube-nocookie.com/watch?v=test")).toBe(true);
    expect(checkForYoutubePrivacyMode("https://www.youtube-nocookie.com/embed/123")).toBe(true);
  });

  test("returns false for regular youtube.com and other URLs", () => {
    expect(checkForYoutubePrivacyMode("https://www.youtube.com/watch?v=test")).toBe(false);
    expect(checkForYoutubePrivacyMode("https://www.example.com")).toBe(false);
  });

  test("returns false for invalid, empty or non-string values", () => {
    expect(checkForYoutubePrivacyMode("not-a-url")).toBe(false);
    expect(checkForYoutubePrivacyMode("")).toBe(false);
    expect(checkForYoutubePrivacyMode("   ")).toBe(false);
    expect(checkForYoutubePrivacyMode(null as unknown as string)).toBe(false);
    expect(checkForYoutubePrivacyMode(123 as unknown as string)).toBe(false);
  });
});
