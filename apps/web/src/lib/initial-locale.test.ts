import { expect, test } from "vite-plus/test";
import { initialLocale } from "./initial-locale";

test("the address's lang outranks the browser's languages", () => {
  expect(initialLocale("?lang=ja-JP", ["en-US", "en"])).toBe("ja-JP");
  expect(initialLocale("?callbackUrl=%2F&lang=zh-Hans-CN", ["en-US"])).toBe("zh-Hans-CN");
});

test("the browser's languages decide without a usable hint", () => {
  expect(initialLocale("", ["ja", "en-US"])).toBe("ja-JP");
  expect(initialLocale("?lang=", ["zh-TW"])).toBe("zh-Hant-TW");
  expect(initialLocale("?lang=xx-YY", ["ja"])).toBe("ja-JP");
  expect(initialLocale("", [])).toBe("en-US");
});
